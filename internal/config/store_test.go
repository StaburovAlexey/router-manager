package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadConfigAndPermissions(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := EnsureDirs(paths); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultConfig()
	cfg.MiniPC.WANInterface = "enp1s0"
	if err := Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MiniPC.WANInterface != "enp1s0" {
		t.Fatalf("unexpected WAN: %s", loaded.MiniPC.WANInterface)
	}
	info, err := os.Stat(paths.Config)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions = %o", got)
	}
}

func TestWriteSensitiveTextCreatesParent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "secret.txt")
	if err := WriteSensitiveText(path, "secret\n"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "secret\n" {
		t.Fatalf("unexpected contents: %q", string(data))
	}
}
