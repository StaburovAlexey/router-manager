package singbox

import (
	"encoding/json"
	"os"
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
	for _, want := range []string{`"server": "203.0.113.10"`, `"server_name": "www.cloudflare.com"`, `"public_key": "ru-public"`, `"short_id": "ru-short"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("local config does not contain %q:\n%s", want, text)
		}
	}
}

func TestRenderLocalFirstHopUsesStableRealityProfile(t *testing.T) {
	cfg := baseConfig()
	paths := config.NewPaths(t.TempDir())
	data, err := RenderLocal(cfg, paths)
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)
	proxy := outboundByTag(t, payload, "proxy")
	if proxy["flow"] != nil {
		t.Fatalf("mini-pc -> ru first hop must not use xtls flow:\n%s", string(data))
	}
	tls := proxy["tls"].(map[string]any)
	if tls["server_name"] != "www.cloudflare.com" {
		t.Fatalf("first hop server_name = %v, want www.cloudflare.com:\n%s", tls["server_name"], string(data))
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

func TestRenderLocalVPNRouteAddsRussianAutoDirectRules(t *testing.T) {
	cfg := baseConfig()
	cfg.Routing.DefaultRoute = config.DefaultRouteVPN
	paths := config.NewPaths(t.TempDir())
	if err := os.MkdirAll(paths.RulesDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.RUGeoIP, []byte(`{"version":3,"rules":[{"ip_cidr":["192.0.2.0/24"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := RenderLocalForMode(cfg, paths, config.DefaultRouteVPN)
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)
	if !hasRuleSet(payload, "geoip-ru", paths.RUGeoIP) {
		t.Fatalf("missing geoip-ru rule-set:\n%s", string(data))
	}
	if !hasRouteRule(payload, "geoip-ru", "direct") {
		t.Fatalf("missing geoip-ru direct route:\n%s", string(data))
	}
	if !hasDomainSuffixRoute(payload, "ru", "direct") || !hasDNSDomainSuffix(payload, "ru", "local") {
		t.Fatalf("missing ru domain direct rules:\n%s", string(data))
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

func TestRenderLocalDirectForeignManualUsesSelectedForeign(t *testing.T) {
	cfg := baseConfig()
	cfg.Routing.VPNConnectionMode = config.VPNConnectionDirect
	cfg.Routing.LocalForeignMode = config.LocalForeignModeManual
	cfg.Routing.SelectedLocalForeign = "nl-1"
	paths := config.NewPaths(t.TempDir())
	if err := config.SaveForeign(paths, config.ForeignServers{Servers: []config.ForeignServer{
		foreignServer("de-1"),
		foreignServerWithIP("nl-1", "198.51.100.30"),
	}}); err != nil {
		t.Fatal(err)
	}

	data, err := RenderLocalForMode(cfg, paths, config.DefaultRouteVPN)
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)
	route := payload["route"].(map[string]any)
	if route["final"] != "proxy" {
		t.Fatalf("route.final = %v, want proxy:\n%s", route["final"], string(data))
	}
	proxy := outboundByTag(t, payload, "proxy")
	if proxy["type"] != "vless" || proxy["server"] != "198.51.100.30" {
		t.Fatalf("proxy outbound must point to selected foreign server:\n%s", string(data))
	}
	if proxy["flow"] != "xtls-rprx-vision" {
		t.Fatalf("direct foreign outbound must use xtls flow:\n%s", string(data))
	}
	tls := proxy["tls"].(map[string]any)
	if tls["server_name"] != "nl-1.example.com" {
		t.Fatalf("proxy server_name = %v, want nl-1.example.com:\n%s", tls["server_name"], string(data))
	}
	if !hasDirectCIDR(payload, "198.51.100.30/32") {
		t.Fatalf("selected foreign server IP must be routed directly:\n%s", string(data))
	}
	if hasDirectCIDR(payload, cfg.RUServer.IP+"/32") {
		t.Fatalf("direct foreign mode must not pin the RU server IP as proxy infra:\n%s", string(data))
	}
}

func TestRenderLocalDirectForeignAutoUsesURLTest(t *testing.T) {
	cfg := baseConfig()
	cfg.Routing.VPNConnectionMode = config.VPNConnectionDirect
	cfg.Routing.LocalForeignMode = config.LocalForeignModeAuto
	paths := config.NewPaths(t.TempDir())
	if err := config.SaveForeign(paths, config.ForeignServers{Servers: []config.ForeignServer{
		foreignServerWithIP("de-1", "198.51.100.20"),
		foreignServerWithIP("nl-1", "198.51.100.30"),
	}}); err != nil {
		t.Fatal(err)
	}

	data, err := RenderLocalForMode(cfg, paths, config.DefaultRouteVPN)
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)
	if !hasOutbound(payload, "de-1", "vless") || !hasOutbound(payload, "nl-1", "vless") {
		t.Fatalf("missing direct foreign vless outbounds:\n%s", string(data))
	}
	urltest := outboundByTag(t, payload, "proxy")
	if urltest["type"] != "urltest" {
		t.Fatalf("proxy outbound = %v, want urltest:\n%s", urltest["type"], string(data))
	}
	if !hasDirectCIDR(payload, "198.51.100.20/32") || !hasDirectCIDR(payload, "198.51.100.30/32") {
		t.Fatalf("foreign server IPs must be routed directly:\n%s", string(data))
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

func TestRenderRUFirstHopUsesStableRealityProfile(t *testing.T) {
	data, err := RenderRU(baseConfig(), "ru-private", config.ForeignServers{Servers: []config.ForeignServer{
		foreignServer("de-1"),
	}}, "")
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeConfig(t, data)
	inbound := payload["inbounds"].([]any)[0].(map[string]any)
	user := inbound["users"].([]any)[0].(map[string]any)
	if user["flow"] != nil {
		t.Fatalf("ru inbound for mini-pc must not require xtls flow:\n%s", string(data))
	}
	tls := inbound["tls"].(map[string]any)
	if tls["server_name"] != "www.cloudflare.com" {
		t.Fatalf("ru inbound server_name = %v, want www.cloudflare.com:\n%s", tls["server_name"], string(data))
	}
	handshake := tls["reality"].(map[string]any)["handshake"].(map[string]any)
	if handshake["server"] != "www.cloudflare.com" {
		t.Fatalf("ru inbound handshake server = %v, want www.cloudflare.com:\n%s", handshake["server"], string(data))
	}

	foreign := outboundByTag(t, payload, "de-1")
	if foreign["flow"] != "xtls-rprx-vision" {
		t.Fatalf("ru -> foreign hop must keep xtls flow:\n%s", string(data))
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
	return foreignServerWithIP(name, "198.51.100.20")
}

func foreignServerWithIP(name string, ip string) config.ForeignServer {
	return config.ForeignServer{
		Name:       name,
		IP:         ip,
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

func outboundByTag(t *testing.T, payload map[string]any, tag string) map[string]any {
	t.Helper()
	for _, item := range payload["outbounds"].([]any) {
		outbound := item.(map[string]any)
		if outbound["tag"] == tag {
			return outbound
		}
	}
	t.Fatalf("missing outbound %q", tag)
	return nil
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

func hasDirectCIDR(payload map[string]any, cidr string) bool {
	return hasRouteRuleCIDR(payload, cidr, "direct")
}

func hasRouteRuleCIDR(payload map[string]any, cidr string, outbound string) bool {
	route := payload["route"].(map[string]any)
	for _, item := range route["rules"].([]any) {
		rule := item.(map[string]any)
		if rule["outbound"] != outbound {
			continue
		}
		cidrs, ok := rule["ip_cidr"].([]any)
		if !ok {
			continue
		}
		for _, value := range cidrs {
			if value == cidr {
				return true
			}
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

func hasDomainSuffixRoute(payload map[string]any, suffix string, outbound string) bool {
	route := payload["route"].(map[string]any)
	for _, item := range route["rules"].([]any) {
		rule := item.(map[string]any)
		if rule["outbound"] != outbound {
			continue
		}
		suffixes, ok := rule["domain_suffix"].([]any)
		if !ok {
			continue
		}
		for _, value := range suffixes {
			if value == suffix {
				return true
			}
		}
	}
	return false
}

func hasDNSDomainSuffix(payload map[string]any, suffix string, server string) bool {
	dns := payload["dns"].(map[string]any)
	for _, item := range dns["rules"].([]any) {
		rule := item.(map[string]any)
		if rule["server"] != server {
			continue
		}
		suffixes, ok := rule["domain_suffix"].([]any)
		if !ok {
			continue
		}
		for _, value := range suffixes {
			if value == suffix {
				return true
			}
		}
	}
	return false
}
