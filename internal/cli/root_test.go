package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func TestCommandSurface(t *testing.T) {
	root := NewRoot(Options{Paths: config.NewPaths(t.TempDir()), Runner: &shell.DryRunner{}})
	for _, command := range []string{"bootstrap", "setup", "tunnel", "selective", "direct", "route", "geoip", "status", "report", "logs", "info", "qr", "update", "restore-network", "uninstall", "direct-add", "direct-remove", "direct-list", "direct-edit", "proxy-add", "proxy-remove", "proxy-list", "proxy-edit", "ru", "foreign", "wifi"} {
		if _, _, err := root.Find([]string{command}); err != nil {
			t.Fatalf("command %s not found: %v", command, err)
		}
	}
	for _, args := range [][]string{{"geoip", "update"}, {"geoip", "status"}, {"geoip", "enable"}, {"geoip", "disable"}} {
		if _, _, err := root.Find(args); err != nil {
			t.Fatalf("command %s not found: %v", strings.Join(args, " "), err)
		}
	}
	for _, args := range [][]string{{"wifi", "set-ssid"}, {"wifi", "set-password"}} {
		if _, _, err := root.Find(args); err != nil {
			t.Fatalf("command %s not found: %v", strings.Join(args, " "), err)
		}
	}
}

func TestBootstrapAndSetupAreHidden(t *testing.T) {
	root := NewRoot(Options{Paths: config.NewPaths(t.TempDir()), Runner: &shell.DryRunner{}})
	for _, name := range []string{"bootstrap", "setup"} {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Fatal(err)
		}
		if !cmd.Hidden {
			t.Fatalf("%s command must be hidden", name)
		}
	}
}

func TestShouldRunInitialSetupWhenConfigMissing(t *testing.T) {
	if !shouldRunInitialSetup(config.NewPaths(t.TempDir())) {
		t.Fatal("missing config must trigger initial setup")
	}
}

func TestShouldRunInitialSetupWhenConfigured(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.MiniPC.APInterface = "wlan0"
	cfg.RUServer.IP = "203.0.113.10"
	cfg.Reality.UUID = "11111111-1111-4111-8111-111111111111"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveForeign(paths, config.ForeignServers{Servers: []config.ForeignServer{{Name: "de-1"}}}); err != nil {
		t.Fatal(err)
	}
	if shouldRunInitialSetup(paths) {
		t.Fatal("complete config must not trigger initial setup")
	}
}

func TestInitialSetupBootstrapFailureHasRecoveryGuidance(t *testing.T) {
	var setupCalled bool
	root := NewRoot(Options{
		Paths:  config.NewPaths(t.TempDir()),
		Runner: &shell.DryRunner{},
		RunBootstrap: func(_ context.Context, _ io.Writer) error {
			return errors.New("apt install failed")
		},
		RunSetup: func(_ context.Context, _ io.Reader, _ io.Writer) error {
			setupCalled = true
			return nil
		},
	})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected bootstrap error")
	}
	if setupCalled {
		t.Fatal("setup must not run after bootstrap failure")
	}
	if !strings.Contains(err.Error(), "подготовка системы не завершена") {
		t.Fatalf("missing recovery context: %v", err)
	}
	if !strings.Contains(err.Error(), "sudo router-manager") {
		t.Fatalf("missing rerun guidance: %v", err)
	}
	if !strings.Contains(err.Error(), "apt install failed") {
		t.Fatalf("missing original cause: %v", err)
	}
	if !strings.Contains(out.String(), "Первый запуск") {
		t.Fatalf("missing first-run notice: %q", out.String())
	}
}

func TestInitialSetupRunsSetupAfterBootstrap(t *testing.T) {
	var calls []string
	root := NewRoot(Options{
		Paths:  config.NewPaths(t.TempDir()),
		Runner: &shell.DryRunner{},
		RunBootstrap: func(_ context.Context, _ io.Writer) error {
			calls = append(calls, "bootstrap")
			return nil
		},
		RunSetup: func(_ context.Context, _ io.Reader, _ io.Writer) error {
			calls = append(calls, "setup")
			return nil
		},
	})
	root.SetArgs([]string{})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "bootstrap,setup" {
		t.Fatalf("unexpected calls: %v", calls)
	}
}

func TestWiFiSetSSIDUpdatesConfigAndAppliesAccessPoint(t *testing.T) {
	paths := testWiFiPaths(t)
	runner := testWiFiRunner()
	root := NewRoot(Options{Paths: paths, Runner: runner, RequireRoot: func() error { return nil }})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"wifi", "set-ssid", "NewAP"})

	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MiniPC.SSID != "NewAP" {
		t.Fatalf("SSID = %q, want NewAP", cfg.MiniPC.SSID)
	}
	if !called(runner.Calls, "systemctl restart hostapd") {
		t.Fatalf("access point was not applied: %#v", runner.Calls)
	}
}

func TestWiFiSetPasswordUpdatesConfigAndAppliesAccessPoint(t *testing.T) {
	paths := testWiFiPaths(t)
	runner := testWiFiRunner()
	root := NewRoot(Options{Paths: paths, Runner: runner, RequireRoot: func() error { return nil }})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"wifi", "set-password"})
	withStdin(t, "newpassword\n", func() {
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WiFi.Password != "newpassword" {
		t.Fatalf("password = %q, want newpassword", cfg.WiFi.Password)
	}
	if strings.Contains(out.String(), "newpassword") {
		t.Fatalf("password leaked to output:\n%s", out.String())
	}
	if !called(runner.Calls, "systemctl restart hostapd") {
		t.Fatalf("access point was not applied: %#v", runner.Calls)
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
	cfg := config.DefaultConfig()
	cfg.MiniPC.APInterface = "wlan1"
	cfg.MiniPC.SSID = "OldAP"
	cfg.WiFi.Password = "oldpassword"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths
}

func testWiFiRunner() *shell.DryRunner {
	return &shell.DryRunner{Outputs: map[string]string{
		"iw dev": `
phy#1
	Interface wlan1
		type managed
`,
		"iw list": `
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
`,
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

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = old
		_ = r.Close()
	}()
	if _, err := w.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	fn()
}
