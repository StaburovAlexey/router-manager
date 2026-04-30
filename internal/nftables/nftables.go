package nftables

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vpn-router/internal/config"
	"vpn-router/internal/shell"
	"vpn-router/internal/system"
	"vpn-router/templates"
)

type TemplateData struct {
	Mode         string
	WANInterface string
	APInterface  string
	RUServerIP   string
	LANCIDR      string
}

func Render(cfg config.Config, mode string) ([]byte, error) {
	return templates.Render("nftables.nft.tmpl", TemplateData{
		Mode:         mode,
		WANInterface: cfg.MiniPC.WANInterface,
		APInterface:  cfg.MiniPC.APInterface,
		RUServerIP:   cfg.RUServer.IP,
		LANCIDR:      cfg.MiniPC.LANCIDR,
	})
}

func Apply(ctx context.Context, runner shell.Runner, paths config.Paths, cfg config.Config, mode string) error {
	data, err := Render(cfg, mode)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(paths.NftablesConf), 0o700); err != nil {
		return err
	}
	if _, err := system.BackupFile(paths.NftablesConf, paths.BackupsDir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(paths.NftablesConf), ".tmp-*")
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
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	if err := runner.Run(ctx, "nft", "-c", "-f", tmpName); err != nil {
		return err
	}
	if err := runner.Run(ctx, "nft", "-f", tmpName); err != nil {
		return err
	}
	if err := os.Rename(tmpName, paths.NftablesConf); err != nil {
		return err
	}
	if err := ensureMainInclude(paths); err != nil {
		return err
	}
	if err := system.UnmaskEnable(ctx, runner, "nftables"); err != nil {
		return err
	}
	return system.Systemctl(ctx, runner, "start", "nftables")
}

func ensureMainInclude(paths config.Paths) error {
	if paths.NftablesMainConf == "" || paths.NftablesConf == "" {
		return nil
	}
	includeLine := fmt.Sprintf(`include "%s"`, paths.NftablesConf)
	data, err := os.ReadFile(paths.NftablesMainConf)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(paths.NftablesMainConf), 0o755); err != nil {
			return err
		}
		contents := "#!/usr/sbin/nft -f\nflush ruleset\n" + includeLine + "\n"
		return os.WriteFile(paths.NftablesMainConf, []byte(contents), 0o644)
	}
	if err != nil {
		return err
	}
	text := string(data)
	if strings.Contains(text, includeLine) || strings.Contains(text, `include "/etc/nftables.d/*.nft"`) {
		return nil
	}
	if _, err := system.BackupFile(paths.NftablesMainConf, paths.BackupsDir); err != nil {
		return err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += includeLine + "\n"
	return os.WriteFile(paths.NftablesMainConf, []byte(text), 0o644)
}
