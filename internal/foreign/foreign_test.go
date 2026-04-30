package foreign

import (
	"testing"

	"router-manager/internal/config"
)

func TestValidSelectedForeignFallsBackToAutoWhenMissing(t *testing.T) {
	servers := config.ForeignServers{Servers: []config.ForeignServer{{Name: "1"}}}
	if got := validSelectedForeign(servers, "finland u1 host"); got != "" {
		t.Fatalf("selected = %q, want auto fallback", got)
	}
}

func TestValidSelectedForeignKeepsExistingSelection(t *testing.T) {
	servers := config.ForeignServers{Servers: []config.ForeignServer{{Name: "1"}}}
	if got := validSelectedForeign(servers, "1"); got != "1" {
		t.Fatalf("selected = %q, want %q", got, "1")
	}
}
