package preflight

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"router-manager/internal/config"
	"router-manager/internal/shell"
	"router-manager/internal/sshclient"
)

func TestCollectFindsManagedFilesAndRuntimeConflicts(t *testing.T) {
	dir := t.TempDir()
	paths := config.NewPaths(dir)
	paths.SingBoxLocalConf = filepath.Join(dir, "sing-box", "config.json")
	paths.HostapdConf = filepath.Join(dir, "hostapd.conf")
	paths.DnsmasqConf = filepath.Join(dir, "dnsmasq.conf")
	paths.NftablesConf = filepath.Join(dir, "router-manager.nft")
	paths.SysctlConf = filepath.Join(dir, "sysctl.conf")
	for _, path := range []string{paths.SingBoxLocalConf, paths.HostapdConf, paths.DnsmasqConf, paths.NftablesConf, paths.SysctlConf} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runner := &shell.DryRunner{Outputs: map[string]string{
		"ip link show tun0":                  "1: tun0: <POINTOPOINT>",
		"nft list table inet router_manager": "table inet router_manager {}",
		"systemctl is-active sing-box":       "active",
	}}
	findings := Collect(context.Background(), runner, paths)
	text := Format(findings)
	for _, want := range []string{"Локальный sing-box config", "hostapd config", "TUN interface tun0", "nftables table inet router_manager", "Служба sing-box уже active", "да"} {
		if !strings.Contains(text, want) {
			t.Fatalf("preflight output does not contain %q:\n%s", want, text)
		}
	}
}

func TestCollectRemoteFindsSingBoxConfigAndActiveService(t *testing.T) {
	runner := remoteRunner{outputs: []string{"exists", "active"}}
	ssh := sshclient.Client{Runner: &runner}
	findings := CollectRemote(context.Background(), ssh, sshclient.Target{User: "root", IP: "203.0.113.10", Port: 22}, "RU-сервер")
	text := Format(findings)
	for _, want := range []string{"RU-сервер: /etc/sing-box/config.json", "RU-сервер: sing-box.service уже active"} {
		if !strings.Contains(text, want) {
			t.Fatalf("remote preflight output does not contain %q:\n%s", want, text)
		}
	}
}

func TestPrepareOverwriteStopsSingBoxAndDeletesTun(t *testing.T) {
	runner := &shell.DryRunner{}
	if err := PrepareOverwrite(context.Background(), runner); err != nil {
		t.Fatal(err)
	}
	want := []string{"systemctl stop sing-box", "ip link delete tun0"}
	for i, call := range want {
		if runner.Calls[i] != call {
			t.Fatalf("call %d = %q, want %q", i, runner.Calls[i], call)
		}
	}
}

type remoteRunner struct {
	outputs []string
	calls   int
}

func (r *remoteRunner) Run(context.Context, string, ...string) error {
	return nil
}

func (r *remoteRunner) Output(context.Context, string, ...string) (string, error) {
	out := ""
	if r.calls < len(r.outputs) {
		out = r.outputs[r.calls]
	}
	r.calls++
	return out, nil
}
