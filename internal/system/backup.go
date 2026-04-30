package system

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

func BackupFile(path string, backupsDir string) (string, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	if err := os.MkdirAll(backupsDir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s.%s.bak", filepath.Base(path), time.Now().UTC().Format("20060102T150405Z"))
	dst := filepath.Join(backupsDir, name)
	if err := copyFile(path, dst, 0o600); err != nil {
		return "", err
	}
	return dst, nil
}

func RestoreFile(backup string, target string, perm os.FileMode) error {
	if backup == "" {
		return nil
	}
	return copyFile(backup, target, perm)
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
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
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, dst)
}
