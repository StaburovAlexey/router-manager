package uninstall

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"router-manager/internal/config"
	"router-manager/internal/restore"
	"router-manager/internal/shell"
	"router-manager/internal/singbox"
	"router-manager/internal/system"
)

var RemovableAptPackages = []string{
	"hostapd",
	"dnsmasq",
	"nftables",
	"iw",
	"rfkill",
	"sshpass",
	"qrencode",
	"nano",
	"iputils-ping",
	"dnsutils",
}

var KeptSystemPackages = []string{
	"iproute2",
	"curl",
	"openssh-client",
	"procps",
	"ca-certificates",
	"openssl",
}

type Service struct {
	Paths              config.Paths
	Runner             shell.Runner
	Out                io.Writer
	RemoveDependencies bool
	TargetBinaries     []string
	SingBoxBinaryPath  string
	SingBoxUnitPath    string
	Restore            func(context.Context) error
	RequireRoot        func() error
}

func (s Service) Run(ctx context.Context) error {
	if s.RequireRoot == nil {
		s.RequireRoot = system.RequireRoot
	}
	if err := s.RequireRoot(); err != nil {
		return err
	}
	if s.Out == nil {
		s.Out = os.Stdout
	}
	if s.Runner == nil {
		s.Runner = shell.RealRunner{}
	}
	if s.Paths.BaseDir == "" {
		s.Paths = config.DefaultPaths()
	}
	steps := []step{
		{"Откат локальных сетевых изменений", func() error {
			if s.Restore != nil {
				return s.Restore(ctx)
			}
			return (restore.Service{Paths: s.Paths, Runner: s.Runner, Out: s.Out}).Run(ctx)
		}},
		{"Удаление данных router-manager", func() error {
			return os.RemoveAll(s.Paths.BaseDir)
		}},
		{"Удаление локального sing-box, установленного приложением", func() error {
			return s.removeSingBox(ctx)
		}},
		{"Удаление бинарника router-manager", func() error {
			return s.removeBinaries()
		}},
	}
	if s.RemoveDependencies {
		steps = append(steps, step{"Удаление прикладных зависимостей", func() error {
			return s.removeDependencies(ctx)
		}})
	}
	var errors []string
	for _, step := range steps {
		fmt.Fprintf(s.Out, "%s...\n", step.name)
		if err := step.run(); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", step.name, err))
		}
	}
	if len(KeptSystemPackages) > 0 {
		fmt.Fprintf(s.Out, "Системные пакеты оставлены: %s\n", strings.Join(KeptSystemPackages, ", "))
	}
	if len(errors) > 0 {
		return fmt.Errorf("удаление выполнено частично:\n%s", strings.Join(errors, "\n"))
	}
	fmt.Fprintln(s.Out, "router-manager удалён с устройства.")
	fmt.Fprintln(s.Out, "Удалённые серверы не изменялись.")
	return nil
}

func (s Service) removeSingBox(ctx context.Context) error {
	unitPath := s.singBoxUnitPath()
	if !isRouterManagerSingBoxUnit(unitPath) {
		return nil
	}
	_ = system.Systemctl(ctx, s.Runner, "stop", singbox.Service)
	_ = system.Systemctl(ctx, s.Runner, "disable", singbox.Service)
	if err := os.Remove(unitPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = s.Runner.Run(ctx, "systemctl", "daemon-reload")
	if err := os.Remove(s.singBoxBinaryPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s Service) singBoxBinaryPath() string {
	if strings.TrimSpace(s.SingBoxBinaryPath) != "" {
		return s.SingBoxBinaryPath
	}
	return singbox.BinaryPath
}

func (s Service) singBoxUnitPath() string {
	if strings.TrimSpace(s.SingBoxUnitPath) != "" {
		return s.SingBoxUnitPath
	}
	return singbox.UnitPath
}

func (s Service) removeBinaries() error {
	paths := s.TargetBinaries
	if len(paths) == 0 {
		paths = defaultBinaryPaths()
	}
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (s Service) removeDependencies(ctx context.Context) error {
	if _, err := exec.LookPath("apt"); err != nil {
		fmt.Fprintln(s.Out, "apt не найден, зависимости не удалялись.")
		return nil
	}
	args := append([]string{"remove", "-y"}, RemovableAptPackages...)
	if err := s.Runner.Run(ctx, "apt", args...); err != nil {
		return err
	}
	return s.Runner.Run(ctx, "apt", "autoremove", "-y")
}

type step struct {
	name string
	run  func() error
}

func defaultBinaryPaths() []string {
	paths := []string{"/usr/local/sbin/router-manager"}
	if current, err := os.Executable(); err == nil && filepath.Base(current) == "router-manager" {
		paths = append(paths, current)
	}
	if found, err := exec.LookPath("router-manager"); err == nil {
		paths = append(paths, found)
	}
	return paths
}

func isRouterManagerSingBoxUnit(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(data)
	return strings.Contains(text, "Router Manager") || strings.Contains(text, "router-manager")
}
