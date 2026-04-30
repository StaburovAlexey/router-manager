package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"vpn-router/internal/config"
	"vpn-router/internal/rules"
	"vpn-router/internal/shell"
	"vpn-router/internal/singbox"
	"vpn-router/internal/system"
)

var AptDependencies = []string{
	"iproute2",
	"iw",
	"nftables",
	"hostapd",
	"dnsmasq",
	"iw",
	"rfkill",
	"curl",
	"openssh-client",
	"procps",
	"qrencode",
	"nano",
	"ca-certificates",
	"iputils-ping",
	"dnsutils",
	"openssl",
}

type Service struct {
	Paths   config.Paths
	Runner  shell.Runner
	Stdout  io.Writer
	Install singbox.Installer
}

func (s Service) Run(ctx context.Context) error {
	if s.Stdout == nil {
		s.Stdout = os.Stdout
	}
	if err := system.RequireRoot(); err != nil {
		return err
	}
	release, err := system.SupportedOS("/etc/os-release")
	if err != nil {
		return err
	}
	fmt.Fprintf(s.Stdout, "ОС поддерживается: %s\n", release.Pretty)
	if _, err := exec.LookPath("apt"); err != nil {
		return fmt.Errorf("apt не найден. Поддерживаются Ubuntu/Debian с apt")
	}
	fmt.Fprintln(s.Stdout, "Установка системных зависимостей...")
	args := append([]string{"install", "-y"}, AptDependencies...)
	if err := s.Runner.Run(ctx, "apt", "update"); err != nil {
		return err
	}
	if err := s.Runner.Run(ctx, "apt", args...); err != nil {
		return err
	}
	fmt.Fprintln(s.Stdout, "Проверка sing-box...")
	if err := s.Install.EnsureInstalled(ctx, s.Runner); err != nil {
		return err
	}
	if err := config.EnsureDirs(s.Paths); err != nil {
		return err
	}
	if err := SeedFiles(s.Paths); err != nil {
		return err
	}
	if err := installSelf("/usr/local/sbin/vpn-router"); err != nil {
		return err
	}
	if _, err := exec.LookPath("vpn-router"); err != nil {
		if _, statErr := os.Stat("/usr/local/sbin/vpn-router"); statErr != nil {
			return fmt.Errorf("vpn-router не найден после установки: %w", err)
		}
	}
	fmt.Fprintln(s.Stdout, "Готово. Следующая команда:")
	fmt.Fprintln(s.Stdout, "sudo vpn-router setup")
	return nil
}

func SeedFiles(paths config.Paths) error {
	if err := config.EnsureDirs(paths); err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if _, err := os.Stat(paths.Config); os.IsNotExist(err) {
		if err := config.Save(paths, cfg); err != nil {
			return err
		}
	}
	if _, err := os.Stat(paths.ForeignServers); os.IsNotExist(err) {
		if err := config.SaveForeign(paths, config.ForeignServers{}); err != nil {
			return err
		}
	}
	if err := rules.EnsureDefault(paths); err != nil {
		return err
	}
	if _, err := os.Stat(paths.CustomProxy); os.IsNotExist(err) {
		if err := config.WriteSensitiveText(paths.CustomProxy, "{\n  \"version\": 3,\n  \"rules\": []\n}\n"); err != nil {
			return err
		}
	}
	if _, err := os.Stat(paths.VPNInfraRules); os.IsNotExist(err) {
		if err := config.WriteSensitiveText(paths.VPNInfraRules, "{\n  \"version\": 3,\n  \"rules\": []\n}\n"); err != nil {
			return err
		}
	}
	return nil
}

func installSelf(target string) error {
	source, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(target), ".vpn-router-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	return os.Rename(tmpName, target)
}
