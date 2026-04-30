package system

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"router-manager/internal/shell"
)

type OSRelease struct {
	ID        string
	VersionID string
	Pretty    string
}

func RequireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("эта команда должна быть запущена от root: используйте sudo router-manager")
	}
	return nil
}

func SupportedOS(path string) (OSRelease, error) {
	release, err := ReadOSRelease(path)
	if err != nil {
		return release, err
	}
	switch release.ID {
	case "ubuntu":
		if release.VersionID == "22.04" || release.VersionID == "24.04" {
			return release, nil
		}
	case "debian":
		if release.VersionID == "12" {
			return release, nil
		}
	}
	return release, fmt.Errorf("неподдерживаемая ОС: %s %s. Поддерживаются Ubuntu 22.04/24.04 и Debian 12", release.ID, release.VersionID)
}

func ReadOSRelease(path string) (OSRelease, error) {
	file, err := os.Open(path)
	if err != nil {
		return OSRelease{}, fmt.Errorf("не удалось прочитать %s: %w", path, err)
	}
	defer file.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[key] = strings.Trim(value, `"`)
	}
	if err := scanner.Err(); err != nil {
		return OSRelease{}, err
	}
	return OSRelease{ID: values["ID"], VersionID: values["VERSION_ID"], Pretty: values["PRETTY_NAME"]}, nil
}

func RequireCommand(ctx context.Context, runner shell.Runner, name string) error {
	if _, err := exec.LookPath(name); err != nil {
		if runner != nil {
			if _, outputErr := runner.Output(ctx, "which", name); outputErr == nil {
				return nil
			}
		}
		return fmt.Errorf("команда %s не найдена", name)
	}
	return nil
}

func GoArchToLinuxAssetArch() (string, error) {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64", nil
	case "arm64":
		return "arm64", nil
	default:
		return "", fmt.Errorf("архитектура %s не поддерживается", runtime.GOARCH)
	}
}

func ParseIntDefault(value string, def int) int {
	if strings.TrimSpace(value) == "" {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return def
	}
	return n
}
