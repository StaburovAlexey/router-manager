package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"router-manager/internal/config"
	"router-manager/internal/shell"
	"router-manager/internal/system"
)

const (
	DefaultRepo       = "StaburovAlexey/router-manager"
	DefaultAPIBaseURL = "https://api.github.com"
	DefaultTargetPath = "/usr/local/sbin/router-manager"
)

type Service struct {
	Repo           string
	APIBaseURL     string
	TargetPath     string
	CurrentVersion string
	Force          bool
	Paths          config.Paths
	Client         *http.Client
	Out            io.Writer
	CheckBinary    func(context.Context, string) error
	Runner         shell.Runner
}

type release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func (s Service) Run(ctx context.Context) error {
	if err := system.RequireRoot(); err != nil {
		return err
	}
	out := s.Out
	if out == nil {
		out = io.Discard
	}
	if s.Paths.BaseDir == "" {
		s.Paths = config.DefaultPaths()
	}
	client := s.client()
	repo := s.repo()

	fmt.Fprintf(out, "Проверяю последний релиз GitHub: %s\n", repo)
	rel, err := s.latestRelease(ctx, client, repo)
	if err != nil {
		return err
	}
	if sameVersion(s.CurrentVersion, rel.TagName) && !s.Force {
		fmt.Fprintf(out, "Уже установлена последняя версия: %s\n", rel.TagName)
		return nil
	}

	assetName, err := binaryAssetName(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	binaryAsset, ok := findAsset(rel.Assets, assetName)
	if !ok {
		return fmt.Errorf("в релизе %s нет файла %s", rel.TagName, assetName)
	}
	checksumAsset, ok := findAsset(rel.Assets, "checksums.txt")
	if !ok {
		return fmt.Errorf("в релизе %s нет checksums.txt, обновление отменено", rel.TagName)
	}

	fmt.Fprintf(out, "Скачиваю %s (%s)\n", assetName, rel.TagName)
	binaryData, err := download(ctx, client, binaryAsset.BrowserDownloadURL)
	if err != nil {
		return err
	}
	checksums, err := download(ctx, client, checksumAsset.BrowserDownloadURL)
	if err != nil {
		return err
	}
	expected, ok := checksumFor(string(checksums), assetName)
	if !ok {
		return fmt.Errorf("checksums.txt не содержит SHA256 для %s", assetName)
	}
	if err := verifySHA256(binaryData, expected); err != nil {
		return err
	}
	fmt.Fprintln(out, "Checksum проверен.")

	tmp, err := writeTempBinary(binaryData)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := s.checkBinary(ctx, tmp); err != nil {
		return fmt.Errorf("скачанный бинарник не прошёл проверку: %w", err)
	}

	target := s.targetPath()
	backup, err := backupBinary(target, s.Paths.BackupsDir)
	if err != nil {
		return err
	}
	if backup != "" {
		fmt.Fprintf(out, "Backup текущего бинарника: %s\n", backup)
	}
	if err := installBinary(tmp, target); err != nil {
		return err
	}
	if err := s.checkBinary(ctx, target); err != nil {
		if backup != "" {
			_ = copyFile(backup, target, 0o755)
		}
		return fmt.Errorf("обновлённый бинарник не запускается, backup восстановлен: %w", err)
	}
	if err := s.runPostUpdate(ctx, target); err != nil {
		return err
	}

	fmt.Fprintf(out, "Обновление установлено: %s\n", target)
	fmt.Fprintln(out, "Служебные настройки обновлены. Закройте меню и снова запустите: sudo router-manager")
	return nil
}

func (s Service) repo() string {
	if s.Repo != "" {
		return s.Repo
	}
	if value := strings.TrimSpace(os.Getenv("ROUTER_MANAGER_UPDATE_REPO")); value != "" {
		return value
	}
	return DefaultRepo
}

func (s Service) apiBaseURL() string {
	if s.APIBaseURL != "" {
		return strings.TrimRight(s.APIBaseURL, "/")
	}
	if value := strings.TrimSpace(os.Getenv("ROUTER_MANAGER_UPDATE_API_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	return DefaultAPIBaseURL
}

func (s Service) targetPath() string {
	if s.TargetPath != "" {
		return s.TargetPath
	}
	if value := strings.TrimSpace(os.Getenv("ROUTER_MANAGER_BIN")); value != "" {
		return value
	}
	if value, err := exec.LookPath("router-manager"); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(value); resolveErr == nil {
			return resolved
		}
		return value
	}
	return DefaultTargetPath
}

func (s Service) client() *http.Client {
	if s.Client != nil {
		return s.Client
	}
	return &http.Client{Timeout: 90 * time.Second}
}

func (s Service) latestRelease(ctx context.Context, client *http.Client, repo string) (release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", s.apiBaseURL(), repo)
	data, err := get(ctx, client, url)
	if err != nil {
		return release{}, err
	}
	var rel release
	if err := json.Unmarshal(data, &rel); err != nil {
		return release{}, fmt.Errorf("GitHub release вернул некорректный JSON: %w", err)
	}
	if rel.TagName == "" {
		return release{}, fmt.Errorf("в GitHub release нет tag_name")
	}
	return rel, nil
}

func (s Service) checkBinary(ctx context.Context, path string) error {
	if s.CheckBinary != nil {
		return s.CheckBinary(ctx, path)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s --version: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (s Service) runPostUpdate(ctx context.Context, target string) error {
	runner := s.Runner
	if runner == nil {
		runner = shell.RealRunner{}
	}
	return runner.Run(ctx, target, "post-update")
}

func binaryAssetName(goos string, goarch string) (string, error) {
	if goos != "linux" {
		return "", fmt.Errorf("автообновление поддерживает только Linux, текущая ОС: %s", goos)
	}
	switch goarch {
	case "amd64", "arm64":
		return "router-manager-linux-" + goarch, nil
	default:
		return "", fmt.Errorf("архитектура %s не поддерживается для автообновления", goarch)
	}
}

func findAsset(assets []asset, name string) (asset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return asset{}, false
}

func sameVersion(current string, latest string) bool {
	current = strings.TrimSpace(current)
	latest = strings.TrimSpace(latest)
	if current == "" || current == "dev" || latest == "" {
		return false
	}
	return current == latest || strings.TrimPrefix(current, "v") == strings.TrimPrefix(latest, "v")
}

func checksumFor(text string, assetName string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[len(fields)-1], "*")
		if filepath.Base(name) == assetName {
			return fields[0], true
		}
	}
	return "", false
}

func verifySHA256(data []byte, expected string) error {
	expected = strings.ToLower(strings.TrimSpace(expected))
	if _, err := hex.DecodeString(expected); err != nil || len(expected) != 64 {
		return fmt.Errorf("checksum имеет некорректный формат: %s", expected)
	}
	sum := sha256.Sum256(data)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return fmt.Errorf("checksum не совпал: ожидалось %s, получено %s", expected, actual)
	}
	return nil
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	return get(ctx, client, url)
}

func get(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "router-manager-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := githubToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("GET %s: %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func githubToken() string {
	if token := strings.TrimSpace(os.Getenv("ROUTER_MANAGER_GITHUB_TOKEN")); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
}

func writeTempBinary(data []byte) (string, error) {
	tmp, err := os.CreateTemp("", "router-manager-update-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := os.Chmod(name, 0o755); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func backupBinary(target string, backupsDir string) (string, error) {
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if backupsDir == "" {
		backupsDir = filepath.Join(config.DefaultBaseDir, "backups")
	}
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("router-manager.%s.bak", time.Now().UTC().Format("20060102T150405Z"))
	backup := filepath.Join(backupsDir, name)
	if err := copyFile(target, backup, info.Mode().Perm()); err != nil {
		return "", err
	}
	return backup, nil
}

func installBinary(src string, target string) error {
	if target == "" {
		return fmt.Errorf("путь установки router-manager не определён")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".router-manager-update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpName)
	if err := copyFile(src, tmpName, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}

func copyFile(src string, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, perm)
}
