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
	return SetDefaultRoute(ctx, runner, paths, config.DefaultRouteVPN)
}

func EnableSelective(ctx context.Context, runner shell.Runner, paths config.Paths) error {
	return SetDefaultRoute(ctx, runner, paths, config.DefaultRouteDirect)
}

func SetDefaultRoute(ctx context.Context, runner shell.Runner, paths config.Paths, defaultRoute string) error {
	if defaultRoute != config.DefaultRouteVPN && defaultRoute != config.DefaultRouteDirect {
		return fmt.Errorf("неизвестный основной маршрут: %s", defaultRoute)
	}
	return enableSingBoxMode(ctx, runner, paths, defaultRoute)
}

func Reapply(ctx context.Context, runner shell.Runner, paths config.Paths) error {
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if cfg.Routing.DefaultRoute == config.DefaultRouteVPN || cfg.Routing.DefaultRoute == config.DefaultRouteDirect {
		return enableSingBoxMode(ctx, runner, paths, cfg.Routing.DefaultRoute)
	}
	return enableSingBoxMode(ctx, runner, paths, config.DefaultRouteDirect)
}

func enableSingBoxMode(ctx context.Context, runner shell.Runner, paths config.Paths, defaultRoute string) error {
	if err := requireRoot(); err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if cfg.Routing.VPNConnectionMode != config.VPNConnectionDirect && cfg.RUServer.IP == "" {
		return fmt.Errorf("входной сервер не настроен")
	}
	if err := ensureReality(&cfg, cfg.Routing.VPNConnectionMode != config.VPNConnectionDirect); err != nil {
		return err
	}
	if err := rules.EnsureDefault(paths); err != nil {
		return err
	}
	if err := network.EnableIPv4Forwarding(ctx, runner, paths.SysctlConf); err != nil {
		return err
	}
	cfg.Routing.DefaultRoute = defaultRoute
	cfg.CurrentMode = config.CurrentModeForDefaultRoute(defaultRoute)
	data, err := singbox.RenderLocalForMode(cfg, paths, defaultRoute)
	if err != nil {
		return err
	}
	if err := singbox.ApplyConfig(ctx, runner, paths, data); err != nil {
		return err
	}
	firewallMode := "direct"
	if defaultRoute == config.DefaultRouteVPN {
		firewallMode = "tunnel"
	}
	if err := nftables.Apply(ctx, runner, paths, cfg, firewallMode); err != nil {
		return err
	}
	if err := config.Save(paths, cfg); err != nil {
		return err
	}
	if cfg.RUServer.IP != "" && cfg.Reality.PublicKey != "" && cfg.Reality.ShortID != "" {
		link := reality.ClientLink(cfg.Reality.UUID, cfg.RUServer.IP, cfg.RUServer.TunnelPort, cfg.Reality.PublicKey, cfg.Reality.ShortID, reality.PreferredSNI, "router-manager-ru")
		return config.WriteSensitiveText(paths.ClientLink, link+"\n")
	}
	return nil
}

func EnableDirect(ctx context.Context, runner shell.Runner, paths config.Paths) error {
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if cfg.RUServer.IP != "" && cfg.Reality.SNI != "" && cfg.Reality.PublicKey != "" && cfg.Reality.ShortID != "" {
		return SetDefaultRoute(ctx, runner, paths, config.DefaultRouteDirect)
	}
	if err := requireRoot(); err != nil {
		return err
	}
	if err := network.EnableIPv4Forwarding(ctx, runner, paths.SysctlConf); err != nil {
		return err
	}
	_ = system.Systemctl(ctx, runner, "stop", singbox.Service)
	if err := nftables.Apply(ctx, runner, paths, cfg, "direct"); err != nil {
		return err
	}
	cfg.Routing.DefaultRoute = config.DefaultRouteDirect
	cfg.CurrentMode = "direct"
	return config.Save(paths, cfg)
}

func SetVPNConnection(ctx context.Context, runner shell.Runner, paths config.Paths, mode string, localForeignMode string, selected string) error {
	if mode != config.VPNConnectionViaRU && mode != config.VPNConnectionDirect {
		return fmt.Errorf("неизвестный режим VPN подключения: %s", mode)
	}
	if localForeignMode == "" {
		localForeignMode = config.LocalForeignModeAuto
	}
	if localForeignMode != config.LocalForeignModeAuto && localForeignMode != config.LocalForeignModeManual {
		return fmt.Errorf("неизвестный режим выбора выходного сервера: %s", localForeignMode)
	}
	if mode == config.VPNConnectionDirect && localForeignMode == config.LocalForeignModeManual && selected == "" {
		return fmt.Errorf("выберите выходной сервер для прямого подключения")
	}
	if mode == config.VPNConnectionDirect {
		servers, err := config.LoadForeign(paths)
		if err != nil {
			return err
		}
		if len(servers.Servers) == 0 {
			return fmt.Errorf("выходные серверы не настроены")
		}
		if localForeignMode == config.LocalForeignModeManual && !containsForeignServer(servers, selected) {
			return fmt.Errorf("выходной сервер %s не найден", selected)
		}
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	previous := cfg
	cfg.Routing.VPNConnectionMode = mode
	cfg.Routing.LocalForeignMode = localForeignMode
	cfg.Routing.SelectedLocalForeign = selected
	if err := config.Save(paths, cfg); err != nil {
		return err
	}
	if err := Reapply(ctx, runner, paths); err != nil {
		_ = config.Save(paths, previous)
		return err
	}
	return nil
}

func containsForeignServer(servers config.ForeignServers, name string) bool {
	for _, server := range servers.Servers {
		if server.Name == name {
			return true
		}
	}
	return false
}

func ensureReality(cfg *config.Config, requireRUKeys bool) error {
	if cfg.Reality.UUID == "" {
		uuid, err := reality.NewUUID()
		if err != nil {
			return err
		}
		cfg.Reality.UUID = uuid
	}
	if requireRUKeys && (cfg.Reality.PublicKey == "" || cfg.Reality.ShortID == "") {
		return fmt.Errorf("REALITY параметры входного сервера не настроены")
	}
	return nil
}
