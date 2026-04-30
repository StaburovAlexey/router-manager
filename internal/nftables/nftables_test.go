package nftables

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func TestRenderDirectAllowsAPToAnyCurrentUplink(t *testing.T) {
	text := renderForTest(t, "direct")
	for _, want := range []string{
		`iifname "wlan0" accept`,
		`ip saddr 10.77.0.0/24 masquerade`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("direct nft config does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `oifname "eth0"`) {
		t.Fatalf("direct config must not bind routing to a fixed WAN:\n%s", text)
	}
	if strings.Contains(text, `oifname "tun0" accept`) {
		t.Fatalf("direct config must not route AP clients to tun0:\n%s", text)
	}
}

func TestRenderTunnelAllowsTunAndDropsDirectWAN(t *testing.T) {
	text := renderForTest(t, "tunnel")
	for _, want := range []string{
		`iifname "wlan0" oifname "tun0" accept`,
		`iifname "wlan0" ip daddr 203.0.113.10 accept`,
		`iifname "wlan0" drop`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("tunnel nft config does not contain %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `oifname "eth0"`) {
		t.Fatalf("tunnel config must not bind routing to a fixed WAN:\n%s", text)
	}
}

func TestApplyDoesNotNeedWANInterface(t *testing.T) {
	dir := t.TempDir()
	paths := config.NewPaths(dir)
	paths.NftablesConf = filepath.Join(dir, "nftables.d", "router-manager.nft")
	paths.NftablesMainConf = filepath.Join(dir, "nftables.conf")
	cfg := config.DefaultConfig()
	cfg.MiniPC.WANInterface = "auto"
	cfg.MiniPC.APInterface = "wlan0"
	runner := &shell.DryRunner{Outputs: map[string]string{
		"ip route show default": "default via 192.0.2.1 dev eth1 proto dhcp",
	}}

	if err := Apply(context.Background(), runner, paths, cfg, "direct"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.NftablesConf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `eth1`) || !strings.Contains(string(data), `ip saddr 10.77.0.0/24 masquerade`) {
		t.Fatalf("nft config must not depend on default route interface:\n%s", string(data))
	}
}

func TestEnsureMainIncludeCreatesConfig(t *testing.T) {
	dir := t.TempDir()
	paths := config.NewPaths(dir)
	paths.NftablesConf = filepath.Join(dir, "nftables.d", "router-manager.nft")
	paths.NftablesMainConf = filepath.Join(dir, "nftables.conf")
	if err := ensureMainInclude(paths); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.NftablesMainConf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `include "`+paths.NftablesConf+`"`) {
		t.Fatalf("main config does not include router-manager nft file:\n%s", string(data))
	}
}

func renderForTest(t *testing.T, mode string) string {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.MiniPC.WANInterface = "eth0"
	cfg.MiniPC.APInterface = "wlan0"
	cfg.RUServer.IP = "203.0.113.10"
	data, err := Render(cfg, mode)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
