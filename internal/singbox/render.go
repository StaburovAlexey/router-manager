package singbox

import (
	"encoding/json"
	"fmt"
	"os"

	"router-manager/internal/config"
	"router-manager/internal/reality"
	"router-manager/templates"
)

func RenderLocal(cfg config.Config, paths config.Paths) ([]byte, error) {
	cfg = config.NormalizeConfig(cfg)
	return RenderLocalForMode(cfg, paths, cfg.Routing.DefaultRoute)
}

func RenderLocalForMode(cfg config.Config, paths config.Paths, mode string) ([]byte, error) {
	cfg = config.NormalizeConfig(cfg)
	foreignServers, err := config.LoadForeign(paths)
	if err != nil {
		return nil, err
	}
	if cfg.Routing.VPNConnectionMode != config.VPNConnectionDirect && cfg.RUServer.IP == "" {
		return nil, fmt.Errorf("входной сервер не настроен")
	}
	if cfg.Reality.UUID == "" {
		return nil, fmt.Errorf("REALITY UUID не настроен")
	}
	if cfg.Routing.VPNConnectionMode != config.VPNConnectionDirect && (cfg.Reality.PublicKey == "" || cfg.Reality.ShortID == "") {
		return nil, fmt.Errorf("REALITY параметры входного сервера не настроены")
	}
	return renderLocalJSON(cfg, paths, normalizeRouteMode(mode), foreignServers)
}

func normalizeRouteMode(mode string) string {
	switch mode {
	case "", "tunnel", config.DefaultRouteVPN:
		return config.DefaultRouteVPN
	case "selective", config.DefaultRouteDirect:
		return config.DefaultRouteDirect
	default:
		return mode
	}
}

func renderLocalJSON(cfg config.Config, paths config.Paths, defaultRoute string, foreignServers config.ForeignServers) ([]byte, error) {
	if defaultRoute != config.DefaultRouteVPN && defaultRoute != config.DefaultRouteDirect {
		return nil, fmt.Errorf("неизвестный основной маршрут: %s", defaultRoute)
	}
	proxyOutbounds, proxyTag, proxyIPs, err := localProxyOutbounds(cfg, foreignServers)
	if err != nil {
		return nil, err
	}
	ruleSets := []any{}
	addRuleSet := func(tag string, path string) {
		ruleSets = append(ruleSets, map[string]any{
			"tag":    tag,
			"type":   "local",
			"format": "source",
			"path":   path,
		})
	}
	addRuleSet("custom-direct", paths.CustomDirect)
	addRuleSet("custom-proxy", paths.CustomProxy)
	geoIPEnabled := defaultRoute == config.DefaultRouteVPN && cfg.Routing.RUGeoIPDirectEnabled && fileExists(paths.RUGeoIP)
	if geoIPEnabled {
		addRuleSet("geoip-ru", paths.RUGeoIP)
	}

	dnsRules := []any{}
	infraCIDRs := []string{
		cfg.MiniPC.LANCIDR,
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
	for _, ip := range proxyIPs {
		infraCIDRs = append([]string{ip + "/32"}, infraCIDRs...)
	}

	routeRules := []any{
		map[string]any{
			"action":  "sniff",
			"timeout": "1s",
		},
		map[string]any{
			"port":   53,
			"action": "hijack-dns",
		},
		map[string]any{
			"ip_cidr":  infraCIDRs,
			"outbound": "direct",
		},
	}
	dnsFinal := "local"
	routeFinal := "direct"
	if defaultRoute == config.DefaultRouteVPN {
		dnsFinal = "remote"
		routeFinal = proxyTag
		dnsRules = append(dnsRules, map[string]any{"rule_set": "custom-direct", "server": "local"})
		if cfg.Routing.RUDomainsDirectEnabled {
			dnsRules = append(dnsRules, map[string]any{"domain_suffix": ruDomainSuffixes(), "server": "local"})
			routeRules = append(routeRules, map[string]any{"domain_suffix": ruDomainSuffixes(), "outbound": "direct"})
		}
		routeRules = append(routeRules,
			map[string]any{"rule_set": "custom-direct", "outbound": "direct"},
			map[string]any{"rule_set": "custom-proxy", "outbound": proxyTag},
		)
		if geoIPEnabled {
			routeRules = append(routeRules, map[string]any{"rule_set": "geoip-ru", "outbound": "direct"})
		}
	} else {
		dnsRules = append(dnsRules, map[string]any{"rule_set": "custom-proxy", "server": "remote"})
		routeRules = append(routeRules, map[string]any{"rule_set": "custom-proxy", "outbound": proxyTag})
	}

	payload := map[string]any{
		"log": map[string]any{
			"level":     "warn",
			"timestamp": true,
		},
		"dns": map[string]any{
			"servers": []any{
				map[string]any{"tag": "local", "type": "local"},
				map[string]any{"tag": "remote", "type": "https", "server": "1.1.1.1", "detour": proxyTag},
			},
			"rules": dnsRules,
			"final": dnsFinal,
		},
		"inbounds": []any{
			map[string]any{
				"type":           "tun",
				"tag":            "tun-in",
				"interface_name": "tun0",
				"address":        []string{"198.18.0.1/30"},
				"auto_route":     true,
				"strict_route":   true,
			},
		},
		"outbounds": append(proxyOutbounds,
			map[string]any{"type": "direct", "tag": "direct"},
			map[string]any{"type": "block", "tag": "block"},
		),
		"route": map[string]any{
			"default_domain_resolver": "local",
			"rule_set":                ruleSets,
			"rules":                   routeRules,
			"final":                   routeFinal,
			"auto_detect_interface":   true,
		},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func localProxyOutbounds(cfg config.Config, foreignServers config.ForeignServers) ([]any, string, []string, error) {
	if cfg.Routing.VPNConnectionMode != config.VPNConnectionDirect {
		return []any{vlessOutbound("proxy", cfg.RUServer.IP, cfg.RUServer.TunnelPort, cfg.Reality.UUID, reality.PreferredSNI, cfg.Reality.PublicKey, cfg.Reality.ShortID)}, "proxy", []string{cfg.RUServer.IP}, nil
	}
	servers := foreignServers.Servers
	if len(servers) == 0 {
		return nil, "", nil, fmt.Errorf("выходные серверы не настроены")
	}
	proxyIPs := make([]string, 0, len(servers))
	if cfg.Routing.LocalForeignMode == config.LocalForeignModeManual {
		server, ok := findForeignServer(servers, cfg.Routing.SelectedLocalForeign)
		if !ok {
			return nil, "", nil, fmt.Errorf("выбранный локальный выходной сервер %s не найден", cfg.Routing.SelectedLocalForeign)
		}
		return []any{foreignOutbound("proxy", server, cfg.Reality.UUID)}, "proxy", []string{server.IP}, nil
	}
	outbounds := make([]any, 0, len(servers)+1)
	tags := make([]string, 0, len(servers))
	for _, server := range servers {
		outbounds = append(outbounds, foreignOutbound(server.Name, server, cfg.Reality.UUID))
		tags = append(tags, server.Name)
		proxyIPs = append(proxyIPs, server.IP)
	}
	outbounds = append(outbounds, map[string]any{
		"type":      "urltest",
		"tag":       "proxy",
		"outbounds": tags,
		"url":       "https://www.gstatic.com/generate_204",
		"interval":  "1m",
		"tolerance": 50,
	})
	return outbounds, "proxy", proxyIPs, nil
}

func foreignOutbound(tag string, server config.ForeignServer, uuid string) any {
	outbound := vlessOutbound(tag, server.IP, server.TunnelPort, uuid, server.Reality.SNI, server.Reality.PublicKey, server.Reality.ShortID)
	outbound["flow"] = "xtls-rprx-vision"
	return outbound
}

func vlessOutbound(tag string, server string, port int, uuid string, sni string, publicKey string, shortID string) map[string]any {
	return map[string]any{
		"type":        "vless",
		"tag":         tag,
		"server":      server,
		"server_port": port,
		"uuid":        uuid,
		"tls": map[string]any{
			"enabled":     true,
			"server_name": sni,
			"reality": map[string]any{
				"enabled":    true,
				"public_key": publicKey,
				"short_id":   shortID,
			},
			"utls": map[string]any{
				"enabled":     true,
				"fingerprint": "chrome",
			},
		},
	}
}

func findForeignServer(servers []config.ForeignServer, name string) (config.ForeignServer, bool) {
	for _, server := range servers {
		if server.Name == name {
			return server, true
		}
	}
	return config.ForeignServer{}, false
}

func ruDomainSuffixes() []string {
	return []string{"ru", "рф", "su"}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func RenderForeign(server config.ForeignServer, uuid string, privateKey string) ([]byte, error) {
	if uuid == "" {
		return nil, fmt.Errorf("REALITY UUID не настроен")
	}
	if server.TunnelPort == 0 {
		return nil, fmt.Errorf("порт подключения выходного сервера не настроен")
	}
	if server.Reality.SNI == "" || server.Reality.ShortID == "" || privateKey == "" {
		return nil, fmt.Errorf("REALITY параметры выходного сервера не настроены")
	}
	return templates.Render("singbox-foreign.json.tmpl", map[string]any{
		"ListenPort": server.TunnelPort,
		"UUID":       uuid,
		"SNI":        server.Reality.SNI,
		"PrivateKey": privateKey,
		"ShortID":    server.Reality.ShortID,
	})
}

func RenderRU(cfg config.Config, privateKey string, servers config.ForeignServers, selected string) ([]byte, error) {
	if cfg.Reality.UUID == "" || cfg.Reality.ShortID == "" || privateKey == "" {
		return nil, fmt.Errorf("REALITY параметры входного сервера не настроены")
	}
	if cfg.RUServer.TunnelPort == 0 {
		return nil, fmt.Errorf("порт подключения входного сервера не настроен")
	}

	outbounds := make([]any, 0, len(servers.Servers)+2)
	foreignTags := make([]string, 0, len(servers.Servers))
	selectedFound := selected == ""
	for _, server := range servers.Servers {
		if server.Name == "" {
			return nil, fmt.Errorf("выходной сервер без имени")
		}
		if selected == server.Name {
			selectedFound = true
		}
		foreignTags = append(foreignTags, server.Name)
		outbounds = append(outbounds, map[string]any{
			"type":        "vless",
			"tag":         server.Name,
			"server":      server.IP,
			"server_port": server.TunnelPort,
			"uuid":        cfg.Reality.UUID,
			"flow":        "xtls-rprx-vision",
			"tls": map[string]any{
				"enabled":     true,
				"server_name": server.Reality.SNI,
				"reality": map[string]any{
					"enabled":    true,
					"public_key": server.Reality.PublicKey,
					"short_id":   server.Reality.ShortID,
				},
				"utls": map[string]any{
					"enabled":     true,
					"fingerprint": "chrome",
				},
			},
		})
	}
	if !selectedFound {
		return nil, fmt.Errorf("выходной сервер %s не найден", selected)
	}

	final := "direct"
	if selected != "" {
		final = selected
	} else if len(foreignTags) > 0 {
		final = "foreign-auto"
		outbounds = append(outbounds, map[string]any{
			"type":      "urltest",
			"tag":       "foreign-auto",
			"outbounds": foreignTags,
			"url":       "https://www.gstatic.com/generate_204",
			"interval":  "1m",
			"tolerance": 50,
		})
	}
	outbounds = append(outbounds, map[string]any{
		"type": "direct",
		"tag":  "direct",
	})

	payload := map[string]any{
		"log": map[string]any{
			"level":     "warn",
			"timestamp": true,
		},
		"inbounds": []any{
			map[string]any{
				"type":        "vless",
				"tag":         "mini-pc-in",
				"listen":      "::",
				"listen_port": cfg.RUServer.TunnelPort,
				"users": []any{
					map[string]any{
						"uuid": cfg.Reality.UUID,
					},
				},
				"tls": map[string]any{
					"enabled":     true,
					"server_name": reality.PreferredSNI,
					"reality": map[string]any{
						"enabled": true,
						"handshake": map[string]any{
							"server":      reality.PreferredSNI,
							"server_port": 443,
						},
						"private_key": privateKey,
						"short_id":    []string{cfg.Reality.ShortID},
					},
				},
			},
		},
		"outbounds": outbounds,
		"route": map[string]any{
			"final": final,
		},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
