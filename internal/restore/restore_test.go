package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreOrRemoveDeletesTargetWhenBackupMissing(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "managed.conf")
	if err := os.WriteFile(target, []byte("managed"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := restoreOrRemove(target, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target still exists or unexpected stat error: %v", err)
	}
}

func TestRestoreOrRemoveReturnsRestoreError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "managed.conf")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(dir, "backups")
	if err := os.Mkdir(backups, 0o700); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(backups, "managed.conf.20260430T120000Z.bak")
	if err := os.WriteFile(backup, []byte("backup"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := restoreOrRemove(target, backups)
	if err == nil {
		t.Fatal("expected restore error")
	}
	if !strings.Contains(err.Error(), "managed.conf") {
		t.Fatalf("error should mention target path, got %v", err)
	}
}
