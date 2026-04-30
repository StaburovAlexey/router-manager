package uninstall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vpn-router/internal/config"
	"vpn-router/internal/shell"
)

func TestRunRemovesDataBinaryAndDependencies(t *testing.T) {
	dir := t.TempDir()
	paths := config.NewPaths(filepath.Join(dir, "etc"))
	if err := os.MkdirAll(paths.BaseDir, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "vpn-router")
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

func called(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}
