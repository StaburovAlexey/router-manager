package wifi

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func TestSwitchAccessPointAppliesRecommendationAndReleasesOldAdapter(t *testing.T) {
	paths := testWiFiPaths(t)
	cfg := config.DefaultConfig()
	cfg.MiniPC.APInterface = "wlan0"
	cfg.MiniPC.SSID = "TestAP"
	cfg.WiFi.Password = "password123"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	runner := testWiFiRunner()

	result, err := SwitchAccessPoint(context.Background(), runner, paths, "wlan1")
	if err != nil {
		t.Fatal(err)
	}
	if result.NewInterface != "wlan1" || result.OldInterface != "wlan0" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Recommendation.Band != "5" || result.Recommendation.Channel != 36 || result.Recommendation.ChannelWidth != 80 {
		t.Fatalf("unexpected recommendation: %#v", result.Recommendation)
	}
	loaded, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MiniPC.APInterface != "wlan1" || loaded.WiFi.Band != "5" || loaded.WiFi.Channel != 36 || loaded.WiFi.ChannelWidth != 80 {
		t.Fatalf("unexpected saved config: %#v", loaded)
	}
	if !called(runner.Calls, "nmcli device set wlan0 managed yes") {
		t.Fatalf("old adapter was not released: %#v", runner.Calls)
	}
}

func TestSwitchAccessPointRollsBackConfigWhenNewAdapterFails(t *testing.T) {
	paths := testWiFiPaths(t)
	cfg := config.DefaultConfig()
	cfg.MiniPC.APInterface = "wlan0"
	cfg.MiniPC.SSID = "TestAP"
	cfg.WiFi.Password = "password123"
	cfg.WiFi.Band = "2.4"
	cfg.WiFi.Channel = 6
	cfg.WiFi.ChannelWidth = 20
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	runner := testWiFiRunner()
	runner.Errors = map[string]error{
		"ip addr replace 10.77.0.1/24 dev wlan1": errTest{},
	}

	_, err := SwitchAccessPoint(context.Background(), runner, paths, "wlan1")
	if err == nil || !strings.Contains(err.Error(), "настройки возвращены") {
		t.Fatalf("expected rollback error, got %v", err)
	}
	loaded, loadErr := config.Load(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if loaded.MiniPC.APInterface != "wlan0" || loaded.WiFi.Band != "2.4" || loaded.WiFi.Channel != 6 || loaded.WiFi.ChannelWidth != 20 {
		t.Fatalf("config was not rolled back: %#v", loaded)
	}
	if !called(runner.Calls, "ip addr replace 10.77.0.1/24 dev wlan0") {
		t.Fatalf("old adapter was not reapplied: %#v", runner.Calls)
	}
}

func testWiFiPaths(t *testing.T) config.Paths {
	t.Helper()
	dir := t.TempDir()
	paths := config.NewPaths(dir)
	paths.HostapdConf = filepath.Join(dir, "hostapd.conf")
	paths.DnsmasqConf = filepath.Join(dir, "dnsmasq.conf")
	paths.NftablesMainConf = filepath.Join(dir, "nftables.conf")
	paths.NftablesConf = filepath.Join(dir, "router-manager.nft")
	paths.SingBoxLocalConf = filepath.Join(dir, "sing-box.json")
	return paths
}

func testWiFiRunner() *shell.DryRunner {
	return &shell.DryRunner{Outputs: map[string]string{
		"iw dev": `
phy#0
	Interface wlan0
		type managed
phy#1
	Interface wlan1
		type managed
`,
		"iw list": `
Wiphy phy0
	Supported interface modes:
		 * managed
		 * AP
	Band 1:
		Capabilities: 0x19ef
			HT20/HT40
		Frequencies:
			* 2412 MHz [1] (20.0 dBm)
			* 2437 MHz [6] (20.0 dBm)
			* 2462 MHz [11] (20.0 dBm)
Wiphy phy1
	Supported interface modes:
		 * managed
		 * AP
	Band 1:
		Capabilities: 0x19ef
			HT20/HT40
		Frequencies:
			* 2412 MHz [1] (20.0 dBm)
			* 2437 MHz [6] (20.0 dBm)
			* 2462 MHz [11] (20.0 dBm)
	Band 2:
		VHT Capabilities (0x0)
		Frequencies:
			* 5180 MHz [36] (20.0 dBm)
			* 5200 MHz [40] (20.0 dBm)
`,
		"iw dev wlan1 scan":           "",
		"systemctl is-active hostapd": "active",
		"systemctl is-active dnsmasq": "active",
	}}
}

func called(calls []string, want string) bool {
	for _, call := range calls {
		if call == want {
			return true
		}
	}
	return false
}

type errTest struct{}

func (errTest) Error() string { return "test error" }
