package system

import (
	"context"
	"testing"

	"vpn-router/internal/shell"
)

func TestUnmaskEnable(t *testing.T) {
	runner := &shell.DryRunner{}
	if err := UnmaskEnable(context.Background(), runner, "hostapd"); err != nil {
		t.Fatal(err)
	}
	want := []string{"systemctl unmask hostapd", "systemctl enable hostapd"}
	if len(runner.Calls) != len(want) {
		t.Fatalf("calls = %#v", runner.Calls)
	}
	for i := range want {
		if runner.Calls[i] != want[i] {
			t.Fatalf("call %d = %q, want %q", i, runner.Calls[i], want[i])
		}
	}
}
