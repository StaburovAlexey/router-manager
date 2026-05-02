package modes

import (
	"context"
	"fmt"

	"router-manager/internal/config"
	"router-manager/internal/network"
	"router-manager/internal/nftables"
	"router-manager/internal/reality"
	"router-manager/internal/rules"
	"router-manager/internal/shell"
	"router-manager/internal/singbox"
	"router-manager/internal/system"
)

var requireRoot = system.RequireRoot

func EnableTunnel(ctx context.Context, runner shell.Runner, paths config.Paths) error {
	return enableSingBoxMode(ctx, runner, paths, "tunnel")
}

func EnableSelective(ctx context.Context, runner shell.Runner, paths config.Paths) error {
	return enableSingBoxMode(ctx, runner, paths, "selective")
}

func enableSingBoxMode(ctx context.Context, runner shell.Runner, paths config.Paths, mode string) error {
	if err := requireRoot(); err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if cfg.RUServer.IP == "" {
		return fmt.Errorf("входной сервер не настроен")
	}
	if err := ensureReality(&cfg); err != nil {
		return err
	}
	if err := rules.EnsureDefault(paths); err != nil {
		return err
	}
	if err := network.EnableIPv4Forwarding(ctx, runner, paths.SysctlConf); err != nil {
		return err
	}
	data, err := singbox.RenderLocalForMode(cfg, paths, mode)
	if err != nil {
		return err
	}
	if err := singbox.ApplyConfig(ctx, runner, paths, data); err != nil {
		return err
	}
	if err := nftables.Apply(ctx, runner, paths, cfg, mode); err != nil {
		return err
	}
	cfg.CurrentMode = mode
	if err := config.Save(paths, cfg); err != nil {
		return err
	}
	link := reality.ClientLink(cfg.Reality.UUID, cfg.RUServer.IP, cfg.RUServer.TunnelPort, cfg.Reality.PublicKey, cfg.Reality.ShortID, cfg.Reality.SNI, "router-manager-ru")
	return config.WriteSensitiveText(paths.ClientLink, link+"\n")
}

func EnableDirect(ctx context.Context, runner shell.Runner, paths config.Paths) error {
	if err := requireRoot(); err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if err := network.EnableIPv4Forwarding(ctx, runner, paths.SysctlConf); err != nil {
		return err
	}
	_ = system.Systemctl(ctx, runner, "stop", singbox.Service)
	if err := nftables.Apply(ctx, runner, paths, cfg, "direct"); err != nil {
		return err
	}
	cfg.CurrentMode = "direct"
	return config.Save(paths, cfg)
}

func ensureReality(cfg *config.Config) error {
	if cfg.Reality.UUID == "" {
		uuid, err := reality.NewUUID()
		if err != nil {
			return err
		}
		cfg.Reality.UUID = uuid
	}
	if cfg.Reality.SNI == "" || cfg.Reality.PublicKey == "" || cfg.Reality.ShortID == "" {
		return fmt.Errorf("REALITY параметры входного сервера не настроены")
	}
	return nil
}
