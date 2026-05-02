package singbox

import (
	"encoding/json"
	"strings"
	"testing"

	"router-manager/internal/config"
)

func TestRenderLocalUsesRUReality(t *testing.T) {
	cfg := baseConfig()
	paths := config.NewPaths(t.TempDir())
	data, err := RenderLocal(cfg, paths)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"server": "203.0.113.10"`, `"server_name": "ru.example.com"`, `"public_key": "ru-public"`, `"short_id": "ru-short"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("local config does not contain %q:\n%s", want, text)
		}
	}
}

func TestRenderedConfigsUseWarnLogLevel(t *testing.T) {
	cfg := baseConfig()
	paths := config.NewPaths(t.TempDir())
	cases := []struct {
		name   string
		render func() ([]byte, error)
	}{
		{
			name: "local",
			render: func() ([]byte, error) {
				return RenderLocal(cfg, paths)
			},
		},
		{
			name: "ru",
			render: func() ([]byte, error) {
				return RenderRU(cfg, "ru-private", config.ForeignServers{Servers: []config.ForeignServer{
					foreignServer("de-1"),
				}}, "")
			},
		},
		{
			name: "foreign",
			render: func() ([]byte, error) {
				server := foreignServer("de-1")
				return RenderForeign(server, cfg.Reality.UUID, "foreign-private")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := tc.render()
			if err != nil {
				t.Fatal(err)
			}
			payload := decodeConfig(t, data)
			log := payload["log"].(map[string]any)
			if log["level"] != "warn" {
				t.Fatalf("log.level = %v, want warn:\n%s", log["level"], string(data))
			}
			if strings.Contains(string(data), `"level": "info"`) {
				t.Fatalf("config must not use info log level:\n%s", string(data))
			}
		})
	}
}

func TestRenderLocalUsesSingBox112DNSFormat(t *testing.T) {
	cfg := baseConfig()
	paths := config.NewPaths(t.TempDir())
	data, err := RenderLocal(cfg, paths)
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)
	dns := payload["dns"].(map[string]any)
	servers := dns["servers"].([]any)

	local := servers[0].(map[string]any)
	if local["type"] != "local" || local["address"] != nil {
		t.Fatalf("local DNS server must use sing-box 1.12 format:\n%s", string(data))
	}

	remote := servers[1].(map[string]any)
	if remote["type"] != "https" || remote["server"] != "1.1.1.1" || remote["address"] != nil {
		t.Fatalf("remote DNS server must use sing-box 1.12 format:\n%s", string(data))
	}

	route := payload["route"].(map[string]any)
	if route["default_domain_resolver"] != "local" {
		t.Fatalf("route.default_domain_resolver must be set for sing-box 1.12+:\n%s", string(data))
	}
}

func TestRenderLocalTunnelRoutesDefaultToProxyAndCustomDirectToDirect(t *testing.T) {
	cfg := baseConfig()
	paths := config.NewPaths(t.TempDir())
	data, err := RenderLocalForMode(cfg, paths, "tunnel")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)

	route := payload["route"].(map[string]any)
	if route["final"] != "proxy" {
		t.Fatalf("route.final = %v, want proxy:\n%s", route["final"], string(data))
	}
	if !hasRuleSet(payload, "custom-direct", paths.CustomDirect) {
		t.Fatalf("missing custom-direct rule-set:\n%s", string(data))
	}
	if !hasRouteRule(payload, "custom-direct", "direct") {
		t.Fatalf("missing custom-direct -> direct route rule:\n%s", string(data))
	}
	if !hasDNSRule(payload, "custom-direct", "local") {
		t.Fatalf("missing custom-direct -> local DNS rule:\n%s", string(data))
	}
	dns := payload["dns"].(map[string]any)
	if dns["final"] != "remote" {
		t.Fatalf("dns.final = %v, want remote:\n%s", dns["final"], string(data))
	}
}

func TestRenderLocalSelectiveRoutesDefaultToDirectAndCustomProxyToProxy(t *testing.T) {
	cfg := baseConfig()
	paths := config.NewPaths(t.TempDir())
	data, err := RenderLocalForMode(cfg, paths, "selective")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)

	route := payload["route"].(map[string]any)
	if route["final"] != "direct" {
		t.Fatalf("route.final = %v, want direct:\n%s", route["final"], string(data))
	}
	if !hasRuleSet(payload, "custom-proxy", paths.CustomProxy) {
		t.Fatalf("missing custom-proxy rule-set:\n%s", string(data))
	}
	if !hasRouteRule(payload, "custom-proxy", "proxy") {
		t.Fatalf("missing custom-proxy -> proxy route rule:\n%s", string(data))
	}
	if !hasDNSRule(payload, "custom-proxy", "remote") {
		t.Fatalf("missing custom-proxy -> remote DNS rule:\n%s", string(data))
	}
	dns := payload["dns"].(map[string]any)
	if dns["final"] != "local" {
		t.Fatalf("dns.final = %v, want local:\n%s", dns["final"], string(data))
	}
}

func TestRenderLocalSniffsBeforeDomainRouting(t *testing.T) {
	cfg := baseConfig()
	paths := config.NewPaths(t.TempDir())
	data, err := RenderLocalForMode(cfg, paths, "selective")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)
	route := payload["route"].(map[string]any)
	routeRules := route["rules"].([]any)
	if len(routeRules) < 2 {
		t.Fatalf("route rules are too short:\n%s", string(data))
	}
	first := routeRules[0].(map[string]any)
	if first["action"] != "sniff" || first["timeout"] != "1s" {
		t.Fatalf("first route rule must sniff domains before DNS and domain routing:\n%s", string(data))
	}
	second := routeRules[1].(map[string]any)
	if second["action"] != "hijack-dns" {
		t.Fatalf("DNS hijack must remain after sniff action:\n%s", string(data))
	}
}

func TestRenderLocalTunAddressDoesNotMatchPrivateDirectRules(t *testing.T) {
	cfg := baseConfig()
	paths := config.NewPaths(t.TempDir())
	data, err := RenderLocal(cfg, paths)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `"172.19.0.1/30"`) {
		t.Fatalf("tun DNS endpoint must not be inside 172.16.0.0/12 direct rule:\n%s", text)
	}
	if !strings.Contains(text, `"198.18.0.1/30"`) {
		t.Fatalf("local config does not contain expected tun address:\n%s", text)
	}
}

func TestRenderLocalHijacksTunDNS(t *testing.T) {
	cfg := baseConfig()
	paths := config.NewPaths(t.TempDir())
	data, err := RenderLocal(cfg, paths)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"port": 53`, `"action": "hijack-dns"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("local config does not contain %q:\n%s", want, text)
		}
	}
}

func TestRenderRUAutoUsesURLTest(t *testing.T) {
	data, err := RenderRU(baseConfig(), "ru-private", config.ForeignServers{Servers: []config.ForeignServer{
		foreignServer("de-1"),
		foreignServer("nl-1"),
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)
	route := payload["route"].(map[string]any)
	if route["final"] != "foreign-auto" {
		t.Fatalf("route.final = %v", route["final"])
	}
	if !hasOutbound(payload, "foreign-auto", "urltest") {
		t.Fatalf("expected foreign-auto urltest outbound:\n%s", string(data))
	}
}

func TestRenderRUManualUsesSelectedForeign(t *testing.T) {
	data, err := RenderRU(baseConfig(), "ru-private", config.ForeignServers{Servers: []config.ForeignServer{
		foreignServer("de-1"),
		foreignServer("nl-1"),
	}}, "nl-1")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)
	route := payload["route"].(map[string]any)
	if route["final"] != "nl-1" {
		t.Fatalf("route.final = %v", route["final"])
	}
	if hasOutbound(payload, "foreign-auto", "urltest") {
		t.Fatalf("manual config must not contain foreign-auto urltest:\n%s", string(data))
	}
}

func TestExtractRealityPrivateKey(t *testing.T) {
	key, err := ExtractRealityPrivateKey([]byte(`{
	  "inbounds": [
	    {"tls": {"reality": {"private_key": "secret-key"}}}
	  ]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if key != "secret-key" {
		t.Fatalf("key = %q", key)
	}
}

func baseConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.RUServer.IP = "203.0.113.10"
	cfg.RUServer.TunnelPort = 443
	cfg.Reality.UUID = "11111111-1111-4111-8111-111111111111"
	cfg.Reality.SNI = "ru.example.com"
	cfg.Reality.PublicKey = "ru-public"
	cfg.Reality.ShortID = "ru-short"
	return cfg
}

func foreignServer(name string) config.ForeignServer {
	return config.ForeignServer{
		Name:       name,
		IP:         "198.51.100.20",
		TunnelPort: 443,
		Reality: config.RealityConfig{
			SNI:       name + ".example.com",
			PublicKey: name + "-public",
			ShortID:   name + "-short",
		},
	}
}

func decodeConfig(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func hasOutbound(payload map[string]any, tag string, typ string) bool {
	for _, item := range payload["outbounds"].([]any) {
		outbound := item.(map[string]any)
		if outbound["tag"] == tag && outbound["type"] == typ {
			return true
		}
	}
	return false
}

func hasRuleSet(payload map[string]any, tag string, path string) bool {
	route := payload["route"].(map[string]any)
	for _, item := range route["rule_set"].([]any) {
		ruleSet := item.(map[string]any)
		if ruleSet["tag"] == tag && ruleSet["path"] == path {
			return true
		}
	}
	return false
}

func hasRouteRule(payload map[string]any, ruleSet string, outbound string) bool {
	route := payload["route"].(map[string]any)
	for _, item := range route["rules"].([]any) {
		rule := item.(map[string]any)
		if rule["rule_set"] == ruleSet && rule["outbound"] == outbound {
			return true
		}
	}
	return false
}

func hasDNSRule(payload map[string]any, ruleSet string, server string) bool {
	dns := payload["dns"].(map[string]any)
	for _, item := range dns["rules"].([]any) {
		rule := item.(map[string]any)
		if rule["rule_set"] == ruleSet && rule["server"] == server {
			return true
		}
	}
	return false
}
