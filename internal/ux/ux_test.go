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
