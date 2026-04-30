package singbox

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("dns failed")
}

func TestVersionFallsBackWhenLatestReleaseUnavailable(t *testing.T) {
	installer := Installer{HTTPClient: &http.Client{Transport: failingRoundTripper{}}}
	if got := installer.version(context.Background()); got != FallbackVersion {
		t.Fatalf("version = %q, want %q", got, FallbackVersion)
	}
}

func TestVersionUsesEnvironmentOverride(t *testing.T) {
	t.Setenv("VPN_ROUTER_SING_BOX_VERSION", "v1.12.1")
	installer := Installer{HTTPClient: &http.Client{Transport: failingRoundTripper{}}}
	if got := installer.version(context.Background()); got != "1.12.1" {
		t.Fatalf("version = %q", got)
	}
}
