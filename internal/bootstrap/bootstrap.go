package bootstrap

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"router-manager/internal/config"
	"router-manager/internal/rules"
	"router-manager/internal/shell"
	"router-manager/internal/singbox"
	"router-manager/internal/system"
)

var AptDependencies = []string{
	"iproute2",
	"iw",
	"nftables",
	"hostapd",
	"dnsmasq",
	"rfkill",
	"curl",
	"openssh-client",
	"sshpass",
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
	if err := installSelf("/usr/local/sbin/router-manager"); err != nil {
		return err
	}
	if err := InstallGeoIPTimer(ctx, s.Runner); err != nil {
		return err
	}
	if _, err := exec.LookPath("router-manager"); err != nil {
		if _, statErr := os.Stat("/usr/local/sbin/router-manager"); statErr != nil {
			return fmt.Errorf("router-manager не найден после установки: %w", err)
		}
	}
	fmt.Fprintln(s.Stdout, "Готово. Если настройка ещё не выполнена, запустите sudo router-manager.")
	return nil
}

func InstallGeoIPTimer(ctx context.Context, runner shell.Runner) error {
	service := `[Unit]
Description=Update Router Manager RU GeoIP rules

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/router-manager geoip update
`
	timer := `[Unit]
Description=Daily Router Manager RU GeoIP update

[Timer]
OnCalendar=daily
Persistent=true
RandomizedDelaySec=30m

[Install]
WantedBy=timers.target
`
	if err := config.WriteSensitiveText("/etc/systemd/system/router-manager-geoip-update.service", service); err != nil {
		return err
	}
	if err := os.Chmod("/etc/systemd/system/router-manager-geoip-update.service", 0o644); err != nil {
		return err
	}
	if err := config.WriteSensitiveText("/etc/systemd/system/router-manager-geoip-update.timer", timer); err != nil {
		return err
	}
	if err := os.Chmod("/etc/systemd/system/router-manager-geoip-update.timer", 0o644); err != nil {
		return err
	}
	if err := runner.Run(ctx, "systemctl", "daemon-reload"); err != nil {
		return err
	}
	return runner.Run(ctx, "systemctl", "enable", "--now", "router-manager-geoip-update.timer")
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
	if _, err := os.Stat(paths.InfraRules); os.IsNotExist(err) {
		if err := config.WriteSensitiveText(paths.InfraRules, "{\n  \"version\": 3,\n  \"rules\": []\n}\n"); err != nil {
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
	tmp, err := os.CreateTemp(filepath.Dir(target), ".router-manager-*")
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
