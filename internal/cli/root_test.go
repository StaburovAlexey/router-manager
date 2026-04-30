package cli

import (
	"testing"

	"vpn-router/internal/config"
	"vpn-router/internal/shell"
)

func TestCommandSurface(t *testing.T) {
	root := NewRoot(Options{Paths: config.NewPaths(t.TempDir()), Runner: &shell.DryRunner{}})
	for _, command := range []string{"bootstrap", "setup", "vpn", "direct", "status", "logs", "info", "qr", "update", "restore-network", "direct-add", "direct-remove", "direct-list", "direct-edit", "ru", "foreign", "wifi"} {
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
