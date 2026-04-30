package singbox

import (
	"context"
	"strings"
	"testing"

	"vpn-router/internal/shell"
	"vpn-router/internal/sshclient"
)

func TestEnsureRemoteInstalledUsesVersionedReleaseAndSystemdUnit(t *testing.T) {
	t.Setenv("VPN_ROUTER_SING_BOX_VERSION", "1.12.0")
	runner := &shell.DryRunner{}
	ssh := sshclient.Client{Runner: runner}
	target := sshclient.Target{User: "root", IP: "203.0.113.10", Port: 22}
	if err := (Installer{}).EnsureRemoteInstalled(context.Background(), ssh, target); err != nil {
		t.Fatal(err)
	}
	if len(runner.Calls) != 1 {
		t.Fatalf("calls = %#v", runner.Calls)
	}
	call := runner.Calls[0]
	for _, want := range []string{
		"ssh -o BatchMode=yes -o ConnectTimeout=8 -p 22 root@203.0.113.10",
		"sing-box-1.12.0-linux-${arch}.tar.gz",
		"install -m 755",
		"/usr/local/bin/sing-box",
		"/etc/systemd/system/sing-box.service",
		"systemctl daemon-reload",
	} {
		if !strings.Contains(call, want) {
			t.Fatalf("remote install command does not contain %q:\n%s", want, call)
		}
	}
}
