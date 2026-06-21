package reality

import (
	"strings"
	"testing"
)

func TestNewUUIDShape(t *testing.T) {
	uuid, err := NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(uuid, "-")
	if len(parts) != 5 {
		t.Fatalf("bad uuid: %s", uuid)
	}
	if len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		t.Fatalf("bad uuid lengths: %s", uuid)
	}
}

func TestClientLink(t *testing.T) {
	link := ClientLink("uuid", "203.0.113.10", 443, "pub", "abcd", "www.cloudflare.com", "router manager")
	for _, want := range []string{"vless://uuid@203.0.113.10:443", "security=reality", "sni=www.cloudflare.com", "pbk=pub", "sid=abcd", "#router-manager"} {
		if !strings.Contains(link, want) {
			t.Fatalf("link %q does not contain %q", link, want)
		}
	}
	if strings.Contains(link, "flow=xtls-rprx-vision") {
		t.Fatalf("first-hop client link must not include xtls flow: %q", link)
	}
}
