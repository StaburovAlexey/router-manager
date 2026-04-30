package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestLoadRejectsWrongVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom-direct.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"rules":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected version error")
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
