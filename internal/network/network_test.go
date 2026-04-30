package network

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func TestDefaultWANInterface(t *testing.T) {
	runner := &shell.DryRunner{Outputs: map[string]string{
		"ip route show default": "default via 192.0.2.1 dev enp1s0 proto dhcp src 192.0.2.10 metric 100",
	}}
	got, err := DefaultWANInterface(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	if got != "enp1s0" {
		t.Fatalf("got %q", got)
	}
}

func TestInterfacesMarksDefaultAndIPv4(t *testing.T) {
	runner := &shell.DryRunner{Outputs: map[string]string{
		"ip route show default":         "default via 192.0.2.1 dev enp3s0 proto dhcp",
		"ip -o link show":               "1: lo: <LOOPBACK> mtu 65536 state UNKNOWN mode DEFAULT group default qlen 1000\n2: enp3s0: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 state UP mode DEFAULT group default qlen 1000\n3: wlan0: <BROADCAST,MULTICAST> mtu 1500 state DOWN mode DEFAULT group default qlen 1000",
		"ip -o -4 addr show dev enp3s0": "2: enp3s0    inet 10.42.0.152/24 brd 10.42.0.255 scope global dynamic enp3s0",
		"ip -o -4 addr show dev wlan0":  "",
	}}
	interfaces, err := Interfaces(context.Background(), runner)
	if err != nil {
		t.Fatal(err)
	}
	if len(interfaces) != 2 {
		t.Fatalf("interface count = %d", len(interfaces))
	}
	if interfaces[0].Name != "enp3s0" || !interfaces[0].IsDefault {
		t.Fatalf("default not first: %#v", interfaces)
	}
	if len(interfaces[0].IPv4) != 1 || interfaces[0].IPv4[0] != "10.42.0.152/24" {
		t.Fatalf("unexpected IPv4: %#v", interfaces[0].IPv4)
	}
}

func TestGatewayCIDR(t *testing.T) {
	got, err := GatewayCIDR("10.77.0.1", "10.77.0.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if got != "10.77.0.1/24" {
		t.Fatalf("gateway cidr = %q", got)
	}
}

func TestConfigureLANInterface(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MiniPC.APInterface = "wlan0"
	runner := &shell.DryRunner{}
	if err := ConfigureLANInterface(context.Background(), runner, cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ip link set wlan0 up",
		"ip addr replace 10.77.0.1/24 dev wlan0",
	}
	for i, call := range want {
		if runner.Calls[i] != call {
			t.Fatalf("call %d = %q, want %q", i, runner.Calls[i], call)
		}
	}
}

func TestEnableIPv4Forwarding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sysctl.d", "99-router-manager.conf")
	runner := &shell.DryRunner{}
	if err := EnableIPv4Forwarding(context.Background(), runner, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "net.ipv4.ip_forward=1\n" {
		t.Fatalf("unexpected sysctl contents: %q", string(data))
	}
	if len(runner.Calls) != 1 || runner.Calls[0] != "sysctl -w net.ipv4.ip_forward=1" {
		t.Fatalf("unexpected calls: %#v", runner.Calls)
	}
}
