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
