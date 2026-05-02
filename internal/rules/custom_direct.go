package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"router-manager/internal/config"
	"router-manager/internal/shell"
	"router-manager/internal/singbox"
)

type RuleSet struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`
}

type Rule struct {
	DomainSuffix []string `json:"domain_suffix,omitempty"`
	Domain       []string `json:"domain,omitempty"`
	IPCIDR       []string `json:"ip_cidr,omitempty"`
}

var domainRE = regexp.MustCompile(`(?i)^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$`)

func EmptyRuleSet() RuleSet {
	return RuleSet{
		Version: 3,
		Rules: []Rule{
			{DomainSuffix: []string{}},
			{Domain: []string{}},
			{IPCIDR: []string{}},
		},
	}
}

func EnsureDefault(paths config.Paths) error {
	for _, path := range []string{paths.CustomDirect, paths.CustomProxy} {
		if err := EnsureDefaultPath(path); err != nil {
			return err
		}
	}
	return nil
}

func EnsureDefaultPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return Save(path, EmptyRuleSet())
}

func Load(path string) (RuleSet, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return EmptyRuleSet(), nil
	}
	if err != nil {
		return RuleSet{}, err
	}
	var set RuleSet
	if err := json.Unmarshal(data, &set); err != nil {
		return RuleSet{}, err
	}
	if set.Version != 3 {
		return RuleSet{}, fmt.Errorf("custom-direct.json должен иметь version=3")
	}
	return normalize(set), nil
}

func Save(path string, set RuleSet) error {
	set = normalize(set)
	set.Rules = nonEmptyRules(set.Rules)
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return config.WriteSensitiveText(path, string(data))
}

func Add(path string, kind string, value string) (string, error) {
	set, err := Load(path)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", fmt.Errorf("значение не может быть пустым")
	}
	normalized, err := normalizeValue(kind, value)
	if err != nil {
		return "", err
	}
	switch kind {
	case "suffix":
		set.Rules[0].DomainSuffix = appendIfMissing(set.Rules[0].DomainSuffix, normalized)
	case "domain":
		set.Rules[1].Domain = appendIfMissing(set.Rules[1].Domain, normalized)
	case "ip", "cidr":
		set.Rules[2].IPCIDR = appendIfMissing(set.Rules[2].IPCIDR, normalized)
	default:
		return "", fmt.Errorf("неизвестный тип правила: %s", kind)
	}
	return normalized, Save(path, set)
}

func AddAuto(path string, value string) (string, string, error) {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "", "", fmt.Errorf("значение не может быть пустым")
	}
	if _, cidr, err := net.ParseCIDR(value); err == nil {
		normalized, err := Add(path, "cidr", cidr.String())
		return "cidr", normalized, err
	}
	if ip := net.ParseIP(value); ip != nil {
		normalized, err := Add(path, "ip", ip.String())
		return "ip", normalized, err
	}
	host := hostFromUserInput(value)
	if ip := net.ParseIP(host); ip != nil {
		normalized, err := Add(path, "ip", ip.String())
		return "ip", normalized, err
	}
	normalized, err := Add(path, "suffix", host)
	return "site", normalized, err
}

func Remove(path string, value string) (bool, error) {
	set, err := Load(path)
	if err != nil {
		return false, err
	}
	value = strings.TrimSpace(strings.ToLower(value))
	candidates := removeCandidates(value)
	if _, _, err := net.ParseCIDR(value); err != nil {
		if host := hostFromUserInput(value); host != "" && host != value {
			for candidate := range removeCandidates(host) {
				candidates[candidate] = true
			}
		}
	}
	removed := false
	for i := range set.Rules {
		set.Rules[i].DomainSuffix, removed = removeString(set.Rules[i].DomainSuffix, candidates, removed)
		set.Rules[i].Domain, removed = removeString(set.Rules[i].Domain, candidates, removed)
		set.Rules[i].IPCIDR, removed = removeString(set.Rules[i].IPCIDR, candidates, removed)
	}
	if !removed {
		return false, nil
	}
	return true, Save(path, set)
}

func Edit(ctx context.Context, path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "nano"
	}
	cmd := exec.CommandContext(ctx, editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	_, err := Load(path)
	return err
}

func AfterChange(ctx context.Context, runner shell.Runner, paths config.Paths) error {
	if _, err := Load(paths.CustomDirect); err != nil {
		return fmt.Errorf("custom-direct.json не прошёл проверку: %w", err)
	}
	if _, err := Load(paths.CustomProxy); err != nil {
		return fmt.Errorf("custom-proxy.json не прошёл проверку: %w", err)
	}
	if _, err := os.Stat(paths.SingBoxLocalConf); err == nil {
		if err := singbox.CheckConfig(ctx, runner, paths.SingBoxLocalConf); err != nil {
			return err
		}
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if cfg.CurrentMode == "tunnel" || cfg.CurrentMode == "selective" {
		return singbox.Restart(ctx, runner)
	}
	return nil
}

func FormatHuman(set RuleSet) string {
	return FormatHumanWithTitle(set, "Сайты и адреса прямого доступа")
}

func FormatHumanWithTitle(set RuleSet, title string) string {
	set = normalize(set)
	var b strings.Builder
	fmt.Fprintln(&b, title+":")
	hasRules := false
	for _, value := range set.Rules[0].DomainSuffix {
		hasRules = true
		fmt.Fprintf(&b, "- %s и его поддомены\n", value)
	}
	for _, value := range set.Rules[1].Domain {
		hasRules = true
		fmt.Fprintf(&b, "- только %s\n", value)
	}
	for _, value := range set.Rules[2].IPCIDR {
		hasRules = true
		fmt.Fprintf(&b, "- %s\n", value)
	}
	if !hasRules {
		fmt.Fprintln(&b, "  пока ничего не добавлено")
	}
	return strings.TrimRight(b.String(), "\n")
}

func normalize(set RuleSet) RuleSet {
	if set.Version == 0 {
		set.Version = 3
	}
	base := EmptyRuleSet()
	for _, rule := range set.Rules {
		base.Rules[0].DomainSuffix = appendUniqueStrings(base.Rules[0].DomainSuffix, rule.DomainSuffix...)
		base.Rules[1].Domain = appendUniqueStrings(base.Rules[1].Domain, rule.Domain...)
		base.Rules[2].IPCIDR = appendUniqueStrings(base.Rules[2].IPCIDR, rule.IPCIDR...)
	}
	base.Version = set.Version
	return base
}

func nonEmptyRules(rules []Rule) []Rule {
	var result []Rule
	for _, rule := range rules {
		if len(rule.DomainSuffix) > 0 {
			result = append(result, Rule{DomainSuffix: rule.DomainSuffix})
		}
		if len(rule.Domain) > 0 {
			result = append(result, Rule{Domain: rule.Domain})
		}
		if len(rule.IPCIDR) > 0 {
			result = append(result, Rule{IPCIDR: rule.IPCIDR})
		}
	}
	if result == nil {
		return []Rule{}
	}
	return result
}

func appendUniqueStrings(values []string, additions ...string) []string {
	for _, value := range additions {
		if strings.TrimSpace(value) == "" {
			continue
		}
		values = appendIfMissing(values, value)
	}
	return values
}

func normalizeValue(kind string, value string) (string, error) {
	switch kind {
	case "suffix", "domain":
		value = strings.TrimPrefix(value, ".")
		if !domainRE.MatchString(value) {
			return "", fmt.Errorf("некорректный домен: %s", value)
		}
		return value, nil
	case "ip":
		ip := net.ParseIP(value)
		if ip == nil {
			return "", fmt.Errorf("некорректный IP: %s", value)
		}
		if ip.To4() != nil {
			return ip.String() + "/32", nil
		}
		return ip.String() + "/128", nil
	case "cidr":
		_, cidr, err := net.ParseCIDR(value)
		if err != nil {
			return "", fmt.Errorf("некорректный CIDR: %s", value)
		}
		return cidr.String(), nil
	default:
		return "", fmt.Errorf("неизвестный тип правила: %s", kind)
	}
}

func hostFromUserInput(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		value = parsed.Host
	}
	if before, _, ok := strings.Cut(value, "/"); ok {
		value = before
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.TrimPrefix(value, "*.")
	value = strings.TrimPrefix(value, ".")
	return strings.TrimSpace(value)
}

func appendIfMissing(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func removeCandidates(value string) map[string]bool {
	candidates := map[string]bool{value: true}
	if ip := net.ParseIP(value); ip != nil {
		if ip.To4() != nil {
			candidates[ip.String()+"/32"] = true
		} else {
			candidates[ip.String()+"/128"] = true
		}
	}
	if _, cidr, err := net.ParseCIDR(value); err == nil {
		candidates[cidr.String()] = true
	}
	return candidates
}

func removeString(values []string, candidates map[string]bool, removed bool) ([]string, bool) {
	result := values[:0]
	for _, existing := range values {
		if candidates[existing] {
			removed = true
			continue
		}
		result = append(result, existing)
	}
	return result, removed
}
