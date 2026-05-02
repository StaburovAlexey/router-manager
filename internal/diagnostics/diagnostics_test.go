package diagnostics

import (
	"context"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func TestDiagnosticsReportsDisabledIPv4Forwarding(t *testing.T) {
	paths := testDiagnosticsPaths(t)
	runner := testDiagnosticsRunner("0")

	status, err := Collect(context.Background(), runner, paths)
	if err != nil {
		t.Fatal(err)
	}
	out := Format(status)

	if !strings.Contains(out, "IPv4 forwarding: IPv4 forwarding выключен") {
		t.Fatalf("missing forwarding detail:\n%s", out)
	}
	if !strings.Contains(out, "Маршрут: ошибка: IPv4 forwarding выключен") {
		t.Fatalf("route summary must fail when forwarding is disabled:\n%s", out)
	}
	if !strings.Contains(out, "sudo router-manager tunnel или sudo router-manager direct") {
		t.Fatalf("missing forwarding recommendation:\n%s", out)
	}
}

func TestDiagnosticsAcceptsEnabledIPv4Forwarding(t *testing.T) {
	paths := testDiagnosticsPaths(t)
	runner := testDiagnosticsRunner("1")

	status, err := Collect(context.Background(), runner, paths)
	if err != nil {
		t.Fatal(err)
	}
	out := Format(status)

	if !strings.Contains(out, "IPv4 forwarding: ok") {
		t.Fatalf("missing forwarding ok detail:\n%s", out)
	}
	if !strings.Contains(out, "Маршрут: включён") {
		t.Fatalf("route summary should be enabled:\n%s", out)
	}
	if strings.Contains(out, "IPv4 forwarding выключен") {
		t.Fatalf("unexpected forwarding warning:\n%s", out)
	}
}

func testDiagnosticsPaths(t *testing.T) config.Paths {
	t.Helper()
	paths := config.NewPaths(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.CurrentMode = "tunnel"
	cfg.MiniPC.APInterface = "wlan0"
	cfg.RUServer.IP = "203.0.113.10"
	cfg.RUServer.SSHUser = "root"
	cfg.RUServer.SSHPort = 22
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths
}

func testDiagnosticsRunner(forwarding string) *shell.DryRunner {
	return &shell.DryRunner{Outputs: map[string]string{
		"systemctl is-active sing-box":                                     "active",
		"systemctl is-active hostapd":                                      "active",
		"systemctl is-active dnsmasq":                                      "active",
		"systemctl is-active nftables":                                     "active",
		"nft list table inet router_manager":                               "table inet router_manager {}",
		"sysctl -n net.ipv4.ip_forward":                                    forwarding,
		"curl -fsS --connect-timeout 1 --max-time 2 https://api.ipify.org": "198.51.100.10",
		"iw dev wlan0 station dump":                                        "",
		"rfkill list":                                                      "",
		"ssh -o BatchMode=yes -o ConnectTimeout=2 -p 22 root@203.0.113.10 cat /etc/ru-tunnel/state.json 2>/dev/null || true": "",
	}}
}
