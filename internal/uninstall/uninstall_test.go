package uninstall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func TestRunRemovesDataBinaryAndDependencies(t *testing.T) {
	dir := t.TempDir()
	paths := config.NewPaths(filepath.Join(dir, "etc"))
	if err := os.MkdirAll(paths.BaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "router-manager")
	if err := os.WriteFile(binary, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &shell.DryRunner{}
	var out strings.Builder
	svc := Service{
		Paths:              paths,
		Runner:             runner,
		Out:                &out,
		RemoveDependencies: true,
		TargetBinaries:     []string{binary},
		SingBoxUnitPath:    filepath.Join(dir, "missing-sing-box.service"),
		SingBoxBinaryPath:  filepath.Join(dir, "missing-sing-box"),
		Restore:            func(context.Context) error { return nil },
		RequireRoot:        func() error { return nil },
	}

	if err := svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.BaseDir); !os.IsNotExist(err) {
		t.Fatalf("base dir still exists or unexpected stat error: %v", err)
	}
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("binary still exists or unexpected stat error: %v", err)
	}
	if !called(runner.Calls, "apt remove -y "+strings.Join(RemovableAptPackages, " ")) {
		t.Fatalf("apt remove was not called: %#v", runner.Calls)
	}
	if !called(runner.Calls, "apt autoremove -y") {
		t.Fatalf("apt autoremove was not called: %#v", runner.Calls)
	}
}

func TestRunCanKeepDependencies(t *testing.T) {
	dir := t.TempDir()
	paths := config.NewPaths(filepath.Join(dir, "etc"))
	runner := &shell.DryRunner{}
	svc := Service{
		Paths:              paths,
		Runner:             runner,
		RemoveDependencies: false,
		TargetBinaries:     []string{filepath.Join(dir, "missing")},
		SingBoxUnitPath:    filepath.Join(dir, "missing-sing-box.service"),
		SingBoxBinaryPath:  filepath.Join(dir, "missing-sing-box"),
		Restore:            func(context.Context) error { return nil },
		RequireRoot:        func() error { return nil },
	}

	if err := svc.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.Calls {
		if strings.HasPrefix(call, "apt ") {
			t.Fatalf("dependencies must not be removed, calls: %#v", runner.Calls)
		}
	}
}

func TestRemoveSingBoxKeepsForeignInstallation(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "sing-box.service")
	binary := filepath.Join(dir, "sing-box")
	if err := os.WriteFile(unit, []byte("[Service]\nExecStart=/usr/local/bin/sing-box run -c /etc/sing-box/config.json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &shell.DryRunner{}
	svc := Service{Runner: runner, SingBoxUnitPath: unit, SingBoxBinaryPath: binary}

	if err := svc.removeSingBox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unit); err != nil {
		t.Fatalf("foreign unit must stay: %v", err)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("foreign binary must stay: %v", err)
	}
	for _, call := range runner.Calls {
		if strings.Contains(call, "sing-box") {
			t.Fatalf("foreign sing-box service must not be touched, calls: %#v", runner.Calls)
		}
	}
}

func TestRemoveSingBoxRemovesRouterManagerInstallation(t *testing.T) {
	dir := t.TempDir()
	unit := filepath.Join(dir, "sing-box.service")
	binary := filepath.Join(dir, "sing-box")
	if err := os.WriteFile(unit, []byte("[Unit]\nDescription=sing-box service for Router Manager\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &shell.DryRunner{}
	svc := Service{Runner: runner, SingBoxUnitPath: unit, SingBoxBinaryPath: binary}

	if err := svc.removeSingBox(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(unit); !os.IsNotExist(err) {
		t.Fatalf("router-manager unit still exists or unexpected stat error: %v", err)
	}
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("router-manager binary still exists or unexpected stat error: %v", err)
	}
	for _, want := range []string{"systemctl stop sing-box", "systemctl disable sing-box", "systemctl daemon-reload"} {
		if !called(runner.Calls, want) {
			t.Fatalf("missing %q in calls: %#v", want, runner.Calls)
		}
	}
}

func called(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
