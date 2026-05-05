package geoip

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"router-manager/internal/config"
	"router-manager/internal/rules"
)

func TestParseIPDenyConvertsCIDRs(t *testing.T) {
	set, err := ParseIPDeny([]byte("192.0.2.0/24\n198.51.100.0/25\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(set.Rules[0].IPCIDR); got != 2 {
		t.Fatalf("CIDR count = %d, want 2", got)
	}
}

func TestParseIPDenyRejectsEmptyOrInvalidList(t *testing.T) {
	for _, input := range []string{"\n# empty\n", "not-a-cidr\n"} {
		if _, err := ParseIPDeny([]byte(input)); err == nil {
			t.Fatalf("expected error for %q", input)
		}
	}
}

func TestUpdateKeepsOldFileOnInvalidDownload(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	old := rules.RuleSet{Version: 3, Rules: []rules.Rule{{IPCIDR: []string{"192.0.2.0/24"}}}}
	if err := rules.Save(paths.RUGeoIP, old); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("bad\n"))
	}))
	defer server.Close()
	cfg := config.DefaultConfig()
	cfg.Routing.GeoIPSource = server.URL
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}

	_, err := (Service{Paths: paths}).Update(context.Background())
	if err == nil {
		t.Fatal("expected update error")
	}
	data, readErr := os.ReadFile(paths.RUGeoIP)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(data), "192.0.2.0/24") {
		t.Fatalf("old GeoIP file was not preserved:\n%s", string(data))
	}
	loaded, loadErr := config.Load(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if loaded.Routing.GeoIPLastError == "" {
		t.Fatal("expected last error to be recorded")
	}
}

func TestUpdateWritesValidListAndStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("192.0.2.0/24\n198.51.100.0/24\n"))
	}))
	defer server.Close()
	paths := config.NewPaths(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Routing.GeoIPSource = server.URL
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}

	result, err := (Service{
		Paths: paths,
		Now:   func() time.Time { return time.Date(2026, 5, 5, 4, 12, 0, 0, time.UTC) },
	}).Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Count != 2 {
		t.Fatalf("unexpected result: %#v", result)
	}
	status, err := (Service{Paths: paths}).Status()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "CIDR в локальном списке: 2") || !strings.Contains(status, "2026-05-05T04:12:00Z") {
		t.Fatalf("unexpected status:\n%s", status)
	}
}
