package restore

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"vpn-router/internal/config"
	"vpn-router/internal/shell"
	"vpn-router/internal/singbox"
	"vpn-router/internal/system"
)

type Service struct {
	Paths  config.Paths
	Runner shell.Runner
	Out    io.Writer
}

func (s Service) Run(ctx context.Context) error {
	if err := system.RequireRoot(); err != nil {
		return err
	}
	if s.Out == nil {
		s.Out = os.Stdout
	}
	cfg, _ := config.Load(s.Paths)
	steps := []step{
		{"Остановка служб vpn-router", func() error {
			_ = system.Systemctl(ctx, s.Runner, "stop", singbox.Service)
			_ = system.Systemctl(ctx, s.Runner, "disable", singbox.Service)
			_ = system.Systemctl(ctx, s.Runner, "stop", "hostapd")
			_ = system.Systemctl(ctx, s.Runner, "disable", "hostapd")
			_ = system.Systemctl(ctx, s.Runner, "stop", "dnsmasq")
			_ = system.Systemctl(ctx, s.Runner, "disable", "dnsmasq")
			return nil
		}},
		{"Удаление nftables table vpn_router", func() error {
			_ = s.Runner.Run(ctx, "nft", "delete", "table", "inet", "vpn_router")
			_ = os.Remove(s.Paths.NftablesConf)
			_ = removeLineContaining(s.Paths.NftablesMainConf, s.Paths.NftablesConf)
			_ = system.Systemctl(ctx, s.Runner, "restart", "nftables")
			return nil
		}},
		{"Отключение IPv4 forwarding vpn-router", func() error {
			_ = os.Remove(s.Paths.SysctlConf)
			_ = s.Runner.Run(ctx, "sysctl", "-w", "net.ipv4.ip_forward=0")
			return nil
		}},
		{"Возврат Wi-Fi адаптеров в NetworkManager", func() error {
			if cfg.MiniPC.APInterface != "" {
				_ = s.Runner.Run(ctx, "ip", "addr", "flush", "dev", cfg.MiniPC.APInterface)
				_ = s.Runner.Run(ctx, "ip", "link", "set", cfg.MiniPC.APInterface, "down")
				_ = s.Runner.Run(ctx, "nmcli", "device", "set", cfg.MiniPC.APInterface, "managed", "yes")
				_ = s.Runner.Run(ctx, "ip", "link", "set", cfg.MiniPC.APInterface, "up")
			}
			_ = s.Runner.Run(ctx, "rfkill", "unblock", "wifi")
			_ = system.Systemctl(ctx, s.Runner, "restart", "NetworkManager")
			return nil
		}},
		{"Восстановление управляемых конфигов из backup или удаление", func() error {
			restoreOrRemove(s.Paths.HostapdConf, s.Paths.BackupsDir)
			restoreOrRemove(s.Paths.DnsmasqConf, s.Paths.BackupsDir)
			restoreOrRemove(s.Paths.SingBoxLocalConf, s.Paths.BackupsDir)
			return nil
		}},
		{"Обновление адреса WAN через DHCP", func() error {
			if cfg.MiniPC.WANInterface != "" {
				_ = s.Runner.Run(ctx, "dhclient", "-r", cfg.MiniPC.WANInterface)
				_ = s.Runner.Run(ctx, "dhclient", cfg.MiniPC.WANInterface)
			}
			return nil
		}},
	}
	var errors []string
	for _, step := range steps {
		fmt.Fprintf(s.Out, "%s...\n", step.name)
		if err := step.run(); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", step.name, err))
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("откат выполнен частично:\n%s", strings.Join(errors, "\n"))
	}
	fmt.Fprintln(s.Out, "Локальные сетевые изменения vpn-router отключены.")
	fmt.Fprintln(s.Out, "Удалённые RU/foreign серверы не изменялись.")
	return nil
}

type step struct {
	name string
	run  func() error
}

func restoreOrRemove(path string, backupsDir string) {
	backup := latestBackupFor(path, backupsDir)
	if backup == "" {
		_ = os.Remove(path)
		return
	}
	_ = system.RestoreFile(backup, path, 0o600)
}

func latestBackupFor(path string, backupsDir string) string {
	if path == "" || backupsDir == "" {
		return ""
	}
	base := filepath.Base(path) + "."
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		return ""
	}
	var matches []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, base) && strings.HasSuffix(name, ".bak") {
			matches = append(matches, filepath.Join(backupsDir, name))
		}
	}
	sort.Strings(matches)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

func removeLineContaining(path string, needle string) error {
	if path == "" || needle == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, needle) {
			continue
		}
		lines = append(lines, line)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
