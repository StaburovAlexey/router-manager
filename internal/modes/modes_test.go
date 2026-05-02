package modes

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func TestEnableTunnelEnablesIPv4Forwarding(t *testing.T) {
	paths := testModePaths(t)
	cfg := testModeConfig()
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	runner := testModeRunner()
	withRootForTest(t)

	if err := EnableTunnel(context.Background(), runner, paths); err != nil {
		t.Fatal(err)
	}

	assertFileContents(t, paths.SysctlConf, "net.ipv4.ip_forward=1\n")
	if !called(runner.Calls, "sysctl -w net.ipv4.ip_forward=1") {
		t.Fatalf("forwarding was not enabled: %#v", runner.Calls)
	}
	if indexOf(runner.Calls, "sysctl -w net.ipv4.ip_forward=1") > firstCallWithPrefix(runner.Calls, "nft ") {
		t.Fatalf("forwarding must be enabled before nftables apply: %#v", runner.Calls)
	}
}

func TestEnableDirectEnablesIPv4Forwarding(t *testing.T) {
	paths := testModePaths(t)
	cfg := testModeConfig()
	cfg.CurrentMode = "tunnel"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	runner := testModeRunner()
	withRootForTest(t)

	if err := EnableDirect(context.Background(), runner, paths); err != nil {
		t.Fatal(err)
	}

	assertFileContents(t, paths.SysctlConf, "net.ipv4.ip_forward=1\n")
	if !called(runner.Calls, "sysctl -w net.ipv4.ip_forward=1") {
		t.Fatalf("forwarding was not enabled: %#v", runner.Calls)
	}
	if indexOf(runner.Calls, "sysctl -w net.ipv4.ip_forward=1") > firstCallWithPrefix(runner.Calls, "nft ") {
		t.Fatalf("forwarding must be enabled before nftables apply: %#v", runner.Calls)
	}
}

func TestEnableSelectiveUsesSingBoxAndTunnelFirewall(t *testing.T) {
	paths := testModePaths(t)
	cfg := testModeConfig()
	cfg.CurrentMode = "direct"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	runner := testModeRunner()
	withRootForTest(t)

	if err := EnableSelective(context.Background(), runner, paths); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentMode != "selective" {
		t.Fatalf("CurrentMode = %q, want selective", loaded.CurrentMode)
	}
	data, err := os.ReadFile(paths.SingBoxLocalConf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"final": "direct"`) || !strings.Contains(string(data), `"custom-proxy"`) {
		t.Fatalf("selective sing-box config must route default direct and custom-proxy via VPN:\n%s", string(data))
	}
	nftData, err := os.ReadFile(paths.NftablesConf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(nftData), `oifname "tun0" accept`) || !strings.Contains(string(nftData), `iifname "wlan0" drop`) {
		t.Fatalf("selective mode must use tunnel firewall shape:\n%s", string(nftData))
	}
}

func testModePaths(t *testing.T) config.Paths {
	t.Helper()
	dir := t.TempDir()
	paths := config.NewPaths(dir)
	paths.SysctlConf = filepath.Join(dir, "sysctl.d", "99-router-manager.conf")
	paths.NftablesConf = filepath.Join(dir, "nftables.d", "router-manager.nft")
	paths.NftablesMainConf = filepath.Join(dir, "nftables.conf")
	paths.SingBoxLocalConf = filepath.Join(dir, "sing-box", "config.json")
	return paths
}

func testModeConfig() config.Config {
	cfg := config.DefaultConfig()
	cfg.MiniPC.APInterface = "wlan0"
	cfg.MiniPC.WANInterface = "auto"
	cfg.RUServer.IP = "203.0.113.10"
	cfg.RUServer.TunnelPort = 443
	cfg.Reality.UUID = "11111111-1111-1111-1111-111111111111"
	cfg.Reality.SNI = "www.microsoft.com"
	cfg.Reality.PublicKey = "public-key"
	cfg.Reality.ShortID = "abcd1234"
	return cfg
}

func testModeRunner() *shell.DryRunner {
	return &shell.DryRunner{Outputs: map[string]string{
		"systemctl is-active sing-box": "active",
	}}
}

func withRootForTest(t *testing.T) {
	t.Helper()
	old := requireRoot
	requireRoot = func() error { return nil }
	t.Cleanup(func() { requireRoot = old })
}

func assertFileContents(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, string(data), want)
	}
}

func called(calls []string, want string) bool {
	return indexOf(calls, want) >= 0
}

func indexOf(calls []string, want string) int {
	for i, call := range calls {
		if call == want {
			return i
		}
	}
	return -1
}

func firstCallWithPrefix(calls []string, prefix string) int {
	for i, call := range calls {
		if len(call) >= len(prefix) && call[:len(prefix)] == prefix {
			return i
		}
	}
	return len(calls)
}
