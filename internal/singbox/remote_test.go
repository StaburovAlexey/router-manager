package singbox

import (
	"context"
	"errors"
	"strings"
	"testing"

	"router-manager/internal/shell"
	"router-manager/internal/sshclient"
)

func TestEnsureRemoteInstalledUsesVersionedReleaseAndSystemdUnit(t *testing.T) {
	t.Setenv("ROUTER_MANAGER_SING_BOX_VERSION", "1.12.0")
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

func TestApplyRemoteConfigChecksListenPortBeforeReplacingConfig(t *testing.T) {
	runner := &shell.DryRunner{}
	ssh := sshclient.Client{Runner: runner}
	target := sshclient.Target{User: "root", IP: "203.0.113.10", Port: 22}

	if err := ApplyRemoteConfig(context.Background(), ssh, target, []byte(`{"inbounds":[]}`), 443); err != nil {
		t.Fatal(err)
	}
	if len(runner.Calls) != 2 {
		t.Fatalf("calls = %#v, want port check and config apply", runner.Calls)
	}
	checkCall := runner.Calls[0]
	applyCall := runner.Calls[1]
	for _, want := range []string{
		"port=443",
		"command -v ss",
		`ss -H -ltnp "sport = :$port"`,
		"systemctl show -p MainPID --value sing-box",
		`grep -v "pid=${main_pid},"`,
		"порт подключения $port уже занят на сервере",
		"Освободите порт или выберите другой порт подключения.",
	} {
		if !strings.Contains(checkCall, want) {
			t.Fatalf("port check command does not contain %q:\n%s", want, checkCall)
		}
	}
	if strings.Contains(checkCall, `mv "$tmp" /etc/sing-box/config.json`) {
		t.Fatalf("port check must not replace config:\n%s", checkCall)
	}
	if !strings.Contains(applyCall, `mv "$tmp" /etc/sing-box/config.json`) {
		t.Fatalf("apply command must replace config after successful port check:\n%s", applyCall)
	}
}

func TestApplyRemoteConfigStopsBeforeReplaceWhenListenPortBusy(t *testing.T) {
	runner := &busyPortRunner{}
	ssh := sshclient.Client{Runner: runner}
	target := sshclient.Target{User: "root", IP: "203.0.113.10", Port: 22}

	err := ApplyRemoteConfig(context.Background(), ssh, target, []byte(`{"inbounds":[]}`), 443)
	if err == nil {
		t.Fatal("expected busy port error")
	}
	var busyErr RemoteListenPortBusyError
	if !errors.As(err, &busyErr) {
		t.Fatalf("error type = %T, want RemoteListenPortBusyError: %v", err, err)
	}
	if busyErr.Port != 443 {
		t.Fatalf("busy port = %d, want 443", busyErr.Port)
	}
	if !strings.Contains(err.Error(), "порт подключения 443") {
		t.Fatalf("error does not mention listen port: %v", err)
	}
	for _, leak := range []string{"ssh -o BatchMode=yes", "set -eu"} {
		if strings.Contains(err.Error(), leak) {
			t.Fatalf("busy port error should stay concise and not include %q:\n%v", leak, err)
		}
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v, want only port check", runner.calls)
	}
	if strings.Contains(runner.calls[0], `mv "$tmp" /etc/sing-box/config.json`) {
		t.Fatalf("config must not be replaced when port check fails:\n%s", runner.calls[0])
	}
}

type busyPortRunner struct {
	calls []string
}

func (r *busyPortRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return nil
}

func (r *busyPortRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	key := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, key)
	if strings.Contains(key, `ss -H -ltnp "sport = :$port"`) {
		return "", errors.New(`ssh -o BatchMode=yes -o ConnectTimeout=8 -p 22 root@203.0.113.10 set -eu
port=443
exit status 98: __ROUTER_MANAGER_PORT_BUSY__
порт подключения 443 уже занят на сервере:
LISTEN 0      4096   *:443 *:* users:(("xray",pid=926,fd=3))
Освободите порт или выберите другой порт подключения.`)
	}
	return "", nil
}
