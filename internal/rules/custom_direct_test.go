package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddNormalizesValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-direct.json")
	if err := Save(path, EmptyRuleSet()); err != nil {
		t.Fatal(err)
	}
	if got, err := Add(path, "suffix", ".Gosuslugi.RU"); err != nil || got != "gosuslugi.ru" {
		t.Fatalf("suffix = %q, %v", got, err)
	}
	if got, err := Add(path, "ip", "1.2.3.4"); err != nil || got != "1.2.3.4/32" {
		t.Fatalf("ip = %q, %v", got, err)
	}
	set, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Rules[0].DomainSuffix) != 1 || set.Rules[0].DomainSuffix[0] != "gosuslugi.ru" {
		t.Fatalf("unexpected suffix rules: %#v", set.Rules[0].DomainSuffix)
	}
	if len(set.Rules[2].IPCIDR) != 1 || set.Rules[2].IPCIDR[0] != "1.2.3.4/32" {
		t.Fatalf("unexpected cidr rules: %#v", set.Rules[2].IPCIDR)
	}
}

func TestRemoveBareIPRemovesStoredCIDR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-direct.json")
	if err := Save(path, EmptyRuleSet()); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(path, "ip", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	removed, err := Remove(path, "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected rule to be removed")
	}
	set, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Rules[2].IPCIDR) != 0 {
		t.Fatalf("unexpected cidr rules: %#v", set.Rules[2].IPCIDR)
	}
}

func TestRemoveURLRemovesStoredSite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-direct.json")
	if err := Save(path, EmptyRuleSet()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddAuto(path, "login.example.com"); err != nil {
		t.Fatal(err)
	}
	removed, err := Remove(path, "https://login.example.com/path")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("expected URL to remove stored site")
	}
}

func TestAddAutoAcceptsURLDomainIPAndCIDR(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-direct.json")
	if err := Save(path, EmptyRuleSet()); err != nil {
		t.Fatal(err)
	}
	if kind, got, err := AddAuto(path, "https://Login.Example.com/path"); err != nil || kind != "site" || got != "login.example.com" {
		t.Fatalf("url = %q %q, %v", kind, got, err)
	}
	if kind, got, err := AddAuto(path, "1.2.3.4"); err != nil || kind != "ip" || got != "1.2.3.4/32" {
		t.Fatalf("ip = %q %q, %v", kind, got, err)
	}
	if kind, got, err := AddAuto(path, "203.0.113.0/24"); err != nil || kind != "cidr" || got != "203.0.113.0/24" {
		t.Fatalf("cidr = %q %q, %v", kind, got, err)
	}
}

func TestLoadRejectsWrongVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-direct.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected version error")
	}
}

func TestFormatHumanShowsReadableRules(t *testing.T) {
	set := EmptyRuleSet()
	set.Rules[0].DomainSuffix = []string{"gosuslugi.ru"}
	set.Rules[2].IPCIDR = []string{"1.2.3.4/32"}
	text := FormatHuman(set)
	for _, want := range []string{"Сайты и адреса прямого доступа", "gosuslugi.ru и его поддомены", "1.2.3.4/32"} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output does not contain %q:\n%s", want, text)
		}
	}
}

func TestAddRejectsInvalidDomain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-direct.json")
	if err := Save(path, EmptyRuleSet()); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(path, "domain", "bad_domain"); err == nil {
		t.Fatal("expected invalid domain error")
	}
}

func TestSaveOmitsEmptyRulesForSingBox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-direct.json")
	if err := Save(path, EmptyRuleSet()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var payload RuleSet
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Rules) != 0 {
		t.Fatalf("empty rules must not be written to sing-box rule-set: %s", string(data))
	}
	if _, err := Add(path, "domain", "login.example.com"); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Rules) != 1 || len(payload.Rules[0].Domain) != 1 || payload.Rules[0].Domain[0] != "login.example.com" {
		t.Fatalf("unexpected saved rules: %s", string(data))
	}
}

func TestLoadNormalizesCompactRulesForEditing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-direct.json")
	if err := os.WriteFile(path, []byte(`{"version":3,"rules":[{"domain":["login.example.com"]}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Rules) != 3 {
		t.Fatalf("expected normalized buckets, got %#v", set.Rules)
	}
	if len(set.Rules[1].Domain) != 1 || set.Rules[1].Domain[0] != "login.example.com" {
		t.Fatalf("domain bucket not normalized: %#v", set.Rules)
	}
}
