package singbox

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"vpn-router/internal/config"
	"vpn-router/internal/shell"
	"vpn-router/internal/sshclient"
	"vpn-router/internal/system"
)

const (
	BinaryPath = "/usr/local/bin/sing-box"
	UnitPath   = "/etc/systemd/system/sing-box.service"
	Service    = "sing-box"
)

type Installer struct {
	HTTPClient *http.Client
}

func (i Installer) EnsureInstalled(ctx context.Context, runner shell.Runner) error {
	if _, err := runner.Output(ctx, BinaryPath, "version"); err == nil {
		return ensureUnit()
	}
	version := os.Getenv("VPN_ROUTER_SING_BOX_VERSION")
	if version == "" {
		var err error
		version, err = i.latestVersion(ctx)
		if err != nil {
			return err
		}
	}
	arch, err := system.GoArchToLinuxAssetArch()
	if err != nil {
		return err
	}
	if err := i.downloadAndInstall(ctx, version, arch); err != nil {
		return err
	}
	if err := ensureUnit(); err != nil {
		return err
	}
	return runner.Run(ctx, BinaryPath, "version")
}

func CheckConfig(ctx context.Context, runner shell.Runner, path string) error {
	return runner.Run(ctx, BinaryPath, "check", "-c", path)
}

func Restart(ctx context.Context, runner shell.Runner) error {
	return system.RestartAndCheck(ctx, runner, Service)
}

func Status(ctx context.Context, runner shell.Runner) string {
	return system.ServiceStatus(ctx, runner, Service)
}

func ApplyRemoteConfig(ctx context.Context, ssh sshclient.Client, target sshclient.Target, contents []byte) error {
	encoded := base64.StdEncoding.EncodeToString(contents)
	command := fmt.Sprintf(`set -eu
if ! command -v sing-box >/dev/null 2>&1; then
  echo "sing-box не установлен на удалённом сервере. Установите sing-box и повторите команду." >&2
  exit 127
fi
install -d -m 700 /etc/sing-box /etc/sing-box/backups
tmp="/etc/sing-box/config.json.tmp.$$"
trap 'rm -f "$tmp"' EXIT
printf %%s %q | base64 -d > "$tmp"
chmod 600 "$tmp"
sing-box check -c "$tmp"
if [ -f /etc/sing-box/config.json ]; then
  cp -p /etc/sing-box/config.json "/etc/sing-box/backups/config.$(date -u +%%Y%%m%%dT%%H%%M%%SZ).json"
fi
mv "$tmp" /etc/sing-box/config.json
systemctl enable sing-box >/dev/null 2>&1 || true
systemctl restart sing-box`, encoded)
	_, err := ssh.Run(ctx, target, command)
	return err
}

func (i Installer) EnsureRemoteInstalled(ctx context.Context, ssh sshclient.Client, target sshclient.Target) error {
	version := os.Getenv("VPN_ROUTER_SING_BOX_VERSION")
	if version == "" {
		var err error
		version, err = i.latestVersion(ctx)
		if err != nil {
			return err
		}
	}
	unit := `[Unit]
Description=sing-box service for VPN Router Manager
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/sing-box run -c /etc/sing-box/config.json
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`
	unitEncoded := base64.StdEncoding.EncodeToString([]byte(unit))
	command := fmt.Sprintf(`set -eu
if command -v /usr/local/bin/sing-box >/dev/null 2>&1; then
  /usr/local/bin/sing-box version >/dev/null
else
  if ! command -v curl >/dev/null 2>&1 || ! command -v tar >/dev/null 2>&1 || ! command -v gzip >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
      apt-get update
      DEBIAN_FRONTEND=noninteractive apt-get install -y curl ca-certificates tar gzip coreutils
    else
      echo "curl/tar/gzip не найдены, apt-get недоступен. Установите зависимости и повторите команду." >&2
      exit 127
    fi
  fi
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) arch="amd64" ;;
    aarch64|arm64) arch="arm64" ;;
    *) echo "архитектура $arch не поддерживается для sing-box" >&2; exit 1 ;;
  esac
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT
  url="https://github.com/SagerNet/sing-box/releases/download/v%[1]s/sing-box-%[1]s-linux-${arch}.tar.gz"
  curl -fsSL "$url" -o "$tmpdir/sing-box.tar.gz"
  tar -xzf "$tmpdir/sing-box.tar.gz" -C "$tmpdir"
  bin="$(find "$tmpdir" -type f -name sing-box | head -1)"
  test -n "$bin"
  install -m 755 "$bin" /usr/local/bin/sing-box
fi
install -d -m 755 /etc/systemd/system
printf %%s %[2]q | base64 -d > /etc/systemd/system/sing-box.service
chmod 644 /etc/systemd/system/sing-box.service
systemctl daemon-reload
/usr/local/bin/sing-box version >/dev/null`, version, unitEncoded)
	_, err := ssh.Run(ctx, target, command)
	return err
}

func ApplyRemoteRU(ctx context.Context, ssh sshclient.Client, target sshclient.Target, cfg config.Config, privateKey string, servers config.ForeignServers, selected string) error {
	data, err := RenderRU(cfg, privateKey, servers, selected)
	if err != nil {
		return err
	}
	if err := ApplyRemoteConfig(ctx, ssh, target, data); err != nil {
		return err
	}
	mode := "auto"
	if selected != "" {
		mode = "manual"
	}
	return WriteRemoteRUState(ctx, ssh, target, mode, selected)
}

func WriteRemoteRUState(ctx context.Context, ssh sshclient.Client, target sshclient.Target, mode string, selected string) error {
	state := fmt.Sprintf("{\"mode\":%q,\"selected_foreign\":%q}\n", mode, selected)
	encoded := base64.StdEncoding.EncodeToString([]byte(state))
	command := fmt.Sprintf("install -d -m 700 /etc/ru-vpn && printf %%s %q | base64 -d > /etc/ru-vpn/state.json && chmod 600 /etc/ru-vpn/state.json", encoded)
	_, err := ssh.Run(ctx, target, command)
	return err
}

func RemoteRealityPrivateKey(ctx context.Context, ssh sshclient.Client, target sshclient.Target) (string, error) {
	out, err := ssh.Run(ctx, target, "cat /etc/sing-box/config.json")
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать /etc/sing-box/config.json на входном VPN-сервере: %w", err)
	}
	key, err := ExtractRealityPrivateKey([]byte(out))
	if err != nil {
		return "", err
	}
	return key, nil
}

func ExtractRealityPrivateKey(data []byte) (string, error) {
	var payload struct {
		Inbounds []struct {
			TLS struct {
				Reality struct {
					PrivateKey string `json:"private_key"`
				} `json:"reality"`
			} `json:"tls"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", fmt.Errorf("remote sing-box config содержит некорректный JSON: %w", err)
	}
	for _, inbound := range payload.Inbounds {
		if inbound.TLS.Reality.PrivateKey != "" {
			return inbound.TLS.Reality.PrivateKey, nil
		}
	}
	return "", fmt.Errorf("remote sing-box config не содержит REALITY private_key")
}

func ApplyConfig(ctx context.Context, runner shell.Runner, paths config.Paths, contents []byte) error {
	if err := os.MkdirAll(filepath.Dir(paths.SingBoxLocalConf), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(paths.SingBoxLocalConf), "config-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(contents); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := CheckConfig(ctx, runner, tmpName); err != nil {
		return fmt.Errorf("sing-box config не прошёл проверку: %w", err)
	}
	backup, err := system.BackupFile(paths.SingBoxLocalConf, paths.BackupsDir)
	if err != nil {
		return err
	}
	if err := os.Rename(tmpName, paths.SingBoxLocalConf); err != nil {
		return err
	}
	if err := Restart(ctx, runner); err != nil {
		_ = system.RestoreFile(backup, paths.SingBoxLocalConf, 0o600)
		_ = Restart(ctx, runner)
		return fmt.Errorf("sing-box не перезапустился, выполнен rollback: %w", err)
	}
	return nil
}

func (i Installer) latestVersion(ctx context.Context) (string, error) {
	client := i.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/SagerNet/sing-box/releases/latest", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("не удалось получить latest release sing-box: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("GitHub вернул статус %s при запросе latest release sing-box", resp.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	version := strings.TrimPrefix(payload.TagName, "v")
	if version == "" {
		return "", fmt.Errorf("GitHub latest release sing-box не содержит tag_name")
	}
	return version, nil
}

func (i Installer) downloadAndInstall(ctx context.Context, version string, arch string) error {
	client := i.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	url := fmt.Sprintf("https://github.com/SagerNet/sing-box/releases/download/v%s/sing-box-%s-linux-%s.tar.gz", version, version, arch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("не удалось скачать sing-box: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("не удалось скачать sing-box: %s", resp.Status)
	}
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || filepath.Base(header.Name) != "sing-box" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(BinaryPath), 0o755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(BinaryPath), "sing-box-*")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		if _, err := io.Copy(tmp, tr); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return err
		}
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpName)
			return err
		}
		if err := os.Chmod(tmpName, 0o755); err != nil {
			_ = os.Remove(tmpName)
			return err
		}
		return os.Rename(tmpName, BinaryPath)
	}
	return fmt.Errorf("архив sing-box не содержит бинарник")
}

func ensureUnit() error {
	if _, err := os.Stat(UnitPath); err == nil {
		return nil
	}
	unit := `[Unit]
Description=sing-box service for VPN Router Manager
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/sing-box run -c /etc/sing-box/config.json
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`
	if err := os.WriteFile(UnitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	return nil
}
