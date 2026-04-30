package cli

import (
	"testing"

	"vpn-router/internal/config"
	"vpn-router/internal/shell"
)

func TestCommandSurface(t *testing.T) {
	root := NewRoot(Options{Paths: config.NewPaths(t.TempDir()), Runner: &shell.DryRunner{}})
	for _, command := range []string{"bootstrap", "setup", "vpn", "direct", "status", "logs", "info", "qr", "restore-network", "direct-add", "direct-remove", "direct-list", "direct-edit", "ru", "foreign", "wifi"} {
		if _, _, err := root.Find([]string{command}); err != nil {
			t.Fatalf("command %s not found: %v", command, err)
		}
	}
	if cmd, _, err := root.Find([]string{"geo"}); err == nil && cmd != root {
		t.Fatalf("geo command must not exist")
	}
}
