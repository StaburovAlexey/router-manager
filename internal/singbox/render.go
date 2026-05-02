package singbox

import (
	"encoding/json"
	"fmt"

	"router-manager/internal/config"
	"router-manager/templates"
)

type LocalTemplateData struct {
	RUServerIP        string
	RUServerPort      int
	UUID              string
	SNI               string
	PublicKey         string
	ShortID           string
	DNSRuleSet        string
	DNSRuleServer     string
	DNSFinal          string
	LANCIDR           string
	RouteRuleSet      string
	RouteRulePath     string
	RouteRuleOutbound string
	RouteFinal        string
}

func RenderLocal(cfg config.Config, paths config.Paths) ([]byte, error) {
	return RenderLocalForMode(cfg, paths, "tunnel")
}

func RenderLocalForMode(cfg config.Config, paths config.Paths, mode string) ([]byte, error) {
	if cfg.RUServer.IP == "" {
		return nil, fmt.Errorf("входной сервер не настроен")
	}
	if cfg.Reality.UUID == "" || cfg.Reality.SNI == "" || cfg.Reality.PublicKey == "" || cfg.Reality.ShortID == "" {
		return nil, fmt.Errorf("REALITY параметры входного сервера не настроены")
	}
	policy, err := localRoutingPolicy(paths, mode)
	if err != nil {
		return nil, err
	}
	return templates.Render("singbox-local.json.tmpl", LocalTemplateData{
		RUServerIP:        cfg.RUServer.IP,
		RUServerPort:      cfg.RUServer.TunnelPort,
		UUID:              cfg.Reality.UUID,
		SNI:               cfg.Reality.SNI,
		PublicKey:         cfg.Reality.PublicKey,
		ShortID:           cfg.Reality.ShortID,
		DNSRuleSet:        policy.DNSRuleSet,
		DNSRuleServer:     policy.DNSRuleServer,
		DNSFinal:          policy.DNSFinal,
		LANCIDR:           cfg.MiniPC.LANCIDR,
		RouteRuleSet:      policy.RouteRuleSet,
		RouteRulePath:     policy.RouteRulePath,
		RouteRuleOutbound: policy.RouteRuleOutbound,
		RouteFinal:        policy.RouteFinal,
	})
}

type localRoutingPolicyData struct {
	DNSRuleSet        string
	DNSRuleServer     string
	DNSFinal          string
	RouteRuleSet      string
	RouteRulePath     string
	RouteRuleOutbound string
	RouteFinal        string
}

func localRoutingPolicy(paths config.Paths, mode string) (localRoutingPolicyData, error) {
	switch mode {
	case "", "tunnel":
		return localRoutingPolicyData{
			DNSRuleSet:        "custom-direct",
			DNSRuleServer:     "local",
			DNSFinal:          "remote",
			RouteRuleSet:      "custom-direct",
			RouteRulePath:     paths.CustomDirect,
			RouteRuleOutbound: "direct",
			RouteFinal:        "proxy",
		}, nil
	case "selective":
		return localRoutingPolicyData{
			DNSRuleSet:        "custom-proxy",
			DNSRuleServer:     "remote",
			DNSFinal:          "local",
			RouteRuleSet:      "custom-proxy",
			RouteRulePath:     paths.CustomProxy,
			RouteRuleOutbound: "proxy",
			RouteFinal:        "direct",
		}, nil
	default:
		return localRoutingPolicyData{}, fmt.Errorf("неизвестный режим локальной маршрутизации: %s", mode)
	}
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
	if cfg.Reality.UUID == "" || cfg.Reality.SNI == "" || cfg.Reality.ShortID == "" || privateKey == "" {
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
						"flow": "xtls-rprx-vision",
					},
				},
				"tls": map[string]any{
					"enabled":     true,
					"server_name": cfg.Reality.SNI,
					"reality": map[string]any{
						"enabled": true,
						"handshake": map[string]any{
							"server":      cfg.Reality.SNI,
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
