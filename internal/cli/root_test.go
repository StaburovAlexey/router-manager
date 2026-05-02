package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func TestCommandSurface(t *testing.T) {
	root := NewRoot(Options{Paths: config.NewPaths(t.TempDir()), Runner: &shell.DryRunner{}})
	for _, command := range []string{"bootstrap", "setup", "tunnel", "direct", "status", "report", "logs", "info", "qr", "update", "restore-network", "uninstall", "direct-add", "direct-remove", "direct-list", "direct-edit", "ru", "foreign", "wifi"} {
		if _, _, err := root.Find([]string{command}); err != nil {
			t.Fatalf("command %s not found: %v", command, err)
		}
	}
	if cmd, _, err := root.Find([]string{"geo"}); err == nil && cmd != root {
		t.Fatalf("geo command must not exist")
	}
}

func TestBootstrapAndSetupAreHidden(t *testing.T) {
	root := NewRoot(Options{Paths: config.NewPaths(t.TempDir()), Runner: &shell.DryRunner{}})
	for _, name := range []string{"bootstrap", "setup"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if !cmd.Hidden {
			t.Fatalf("%s command must be hidden", name)
		}
	}
}

func TestShouldRunInitialSetupWhenConfigMissing(t *testing.T) {
	if !shouldRunInitialSetup(config.NewPaths(t.TempDir())) {
		t.Fatal("missing config must trigger initial setup")
	}
}

func TestShouldRunInitialSetupWhenConfigured(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.MiniPC.APInterface = "wlan0"
	cfg.RUServer.IP = "203.0.113.10"
	cfg.Reality.UUID = "11111111-1111-4111-8111-111111111111"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveForeign(paths, config.ForeignServers{Servers: []config.ForeignServer{{Name: "de-1"}}}); err != nil {
		t.Fatal(err)
	}
	if shouldRunInitialSetup(paths) {
		t.Fatal("complete config must not trigger initial setup")
	}
}

func TestInitialSetupBootstrapFailureHasRecoveryGuidance(t *testing.T) {
	var setupCalled bool
	root := NewRoot(Options{
		Paths:  config.NewPaths(t.TempDir()),
		Runner: &shell.DryRunner{},
		RunBootstrap: func(_ context.Context, _ io.Writer) error {
			return errors.New("apt install failed")
		},
		RunSetup: func(_ context.Context, _ io.Reader, _ io.Writer) error {
			setupCalled = true
			return nil
		},
	})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected bootstrap error")
	}
	if setupCalled {
		t.Fatal("setup must not run after bootstrap failure")
	}
	if !strings.Contains(err.Error(), "подготовка системы не завершена") {
		t.Fatalf("missing recovery context: %v", err)
	}
	if !strings.Contains(err.Error(), "sudo router-manager") {
		t.Fatalf("missing rerun guidance: %v", err)
	}
	if !strings.Contains(err.Error(), "apt install failed") {
		t.Fatalf("missing original cause: %v", err)
	}
	if !strings.Contains(out.String(), "Первый запуск") {
		t.Fatalf("missing first-run notice: %q", out.String())
	}
}

func TestInitialSetupRunsSetupAfterBootstrap(t *testing.T) {
	var calls []string
	root := NewRoot(Options{
		Paths:  config.NewPaths(t.TempDir()),
		Runner: &shell.DryRunner{},
		RunBootstrap: func(_ context.Context, _ io.Writer) error {
			calls = append(calls, "bootstrap")
			return nil
		},
		RunSetup: func(_ context.Context, _ io.Reader, _ io.Writer) error {
			calls = append(calls, "setup")
			return nil
		},
	})
	root.SetArgs([]string{})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "bootstrap,setup" {
		t.Fatalf("unexpected calls: %v", calls)
	}
}
