package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func EnsureDirs(paths Paths) error {
	dirs := []string{paths.BaseDir, paths.RulesDir, paths.ModesDir, paths.BackupsDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("не удалось создать %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("не удалось выставить права на %s: %w", dir, err)
		}
	}
	return nil
}

func Load(paths Paths) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(paths.Config)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultConfig(), nil
	}
	if err != nil {
		return cfg, fmt.Errorf("не удалось прочитать config.json: %w", err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config.json содержит некорректный JSON: %w", err)
	}
	return cfg, nil
}

func Save(paths Paths, cfg Config) error {
	return writeJSON(paths.Config, cfg, 0o600)
}

func LoadForeign(paths Paths) (ForeignServers, error) {
	var servers ForeignServers
	data, err := os.ReadFile(paths.ForeignServers)
	if errors.Is(err, os.ErrNotExist) {
		return servers, nil
	}
	if err != nil {
		return servers, fmt.Errorf("не удалось прочитать foreign-servers.json: %w", err)
	}
	if err := json.Unmarshal(data, &servers); err != nil {
		return servers, fmt.Errorf("foreign-servers.json содержит некорректный JSON: %w", err)
	}
	return servers, nil
}

func SaveForeign(paths Paths, servers ForeignServers) error {
	return writeJSON(paths.ForeignServers, servers, 0o600)
}

func WriteSensitiveText(path string, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(value); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func writeJSON(path string, value any, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
