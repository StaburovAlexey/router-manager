package ux

import (
	"errors"
	"strings"
	"testing"
)

func TestFriendlyErrorGivesBootstrapRecoveryActions(t *testing.T) {
	text := FriendlyError(errors.New("подготовка системы не завершена: apt install failed"))
	for _, want := range []string{
		"запустите подготовку снова: sudo router-manager",
		"sudo router-manager uninstall --keep-deps",
		"apt install failed",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("FriendlyError() missing %q in:\n%s", want, text)
		}
	}
}

func TestFriendlyErrorGivesBusyPortActionsBeforeRootHint(t *testing.T) {
	text := FriendlyError(errors.New(`порт подключения 443 не прошёл проверку на сервере: ssh -p 22 root@150.241.108.217: exit status 98: порт подключения 443 уже занят на сервере:
LISTEN 0      4096   *:443 *:* users:(("xray",pid=926,fd=3))`))
	for _, want := range []string{
		"выберите другой порт подключения",
		"освободите занятый порт",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("FriendlyError() missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "правами администратора") {
		t.Fatalf("busy port error must not be treated as sudo/root error:\n%s", text)
	}
}
