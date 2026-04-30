package singbox

import (
	"encoding/json"
	"fmt"

	"vpn-router/internal/config"
	"vpn-router/templates"
)

type LocalTemplateData struct {
	RUServerIP       string
	RUServerPort     int
	UUID             string
	SNI              string
	PublicKey        string
	ShortID          string
	CustomDirectPath string
	LANCIDR          string
}

func RenderLocal(cfg config.Config, paths config.Paths) ([]byte, error) {
	if cfg.RUServer.IP == "" {
		return nil, fmt.Errorf("RU-сервер не настроен")
	}
	if cfg.Reality.UUID == "" || cfg.Reality.SNI == "" || cfg.Reality.PublicKey == "" || cfg.Reality.ShortID == "" {
		return nil, fmt.Errorf("REALITY параметры RU-сервера не настроены")
	}
	return templates.Render("singbox-local.json.tmpl", LocalTemplateData{
		RUServerIP:       cfg.RUServer.IP,
		RUServerPort:     cfg.RUServer.VPNPort,
		UUID:             cfg.Reality.UUID,
		SNI:              cfg.Reality.SNI,
		PublicKey:        cfg.Reality.PublicKey,
		ShortID:          cfg.Reality.ShortID,
		CustomDirectPath: paths.CustomDirect,
		LANCIDR:          cfg.MiniPC.LANCIDR,
	})
}

func RenderForeign(server config.ForeignServer, uuid string, privateKey string) ([]byte, error) {
	if uuid == "" {
		return nil, fmt.Errorf("REALITY UUID не настроен")
	}
	if server.VPNPort == 0 {
		return nil, fmt.Errorf("VPN port foreign-сервера не настроен")
	}
	if server.Reality.SNI == "" || server.Reality.ShortID == "" || privateKey == "" {
		return nil, fmt.Errorf("REALITY параметры foreign-сервера не настроены")
	}
	return templates.Render("singbox-foreign.json.tmpl", map[string]any{
		"ListenPort": server.VPNPort,
		"UUID":       uuid,
		"SNI":        server.Reality.SNI,
		"PrivateKey": privateKey,
		"ShortID":    server.Reality.ShortID,
	})
}

func RenderRU(cfg config.Config, privateKey string, servers config.ForeignServers, selected string) ([]byte, error) {
	if cfg.Reality.UUID == "" || cfg.Reality.SNI == "" || cfg.Reality.ShortID == "" || privateKey == "" {
		return nil, fmt.Errorf("REALITY параметры RU-сервера не настроены")
	}
	if cfg.RUServer.VPNPort == 0 {
		return nil, fmt.Errorf("VPN port RU-сервера не настроен")
	}

	outbounds := make([]any, 0, len(servers.Servers)+2)
	foreignTags := make([]string, 0, len(servers.Servers))
	selectedFound := selected == ""
	for _, server := range servers.Servers {
		if server.Name == "" {
			return nil, fmt.Errorf("foreign-сервер без имени")
		}
		if selected == server.Name {
			selectedFound = true
		}
		foreignTags = append(foreignTags, server.Name)
		outbounds = append(outbounds, map[string]any{
			"type":        "vless",
			"tag":         server.Name,
			"server":      server.IP,
			"server_port": server.VPNPort,
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
		return nil, fmt.Errorf("foreign-сервер %s не найден", selected)
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
			"level":     "info",
			"timestamp": true,
		},
		"inbounds": []any{
			map[string]any{
				"type":        "vless",
				"tag":         "mini-pc-in",
				"listen":      "::",
				"listen_port": cfg.RUServer.VPNPort,
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
