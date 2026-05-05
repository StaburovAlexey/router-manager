package geoip

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"router-manager/internal/config"
	"router-manager/internal/rules"
)

type Service struct {
	Paths      config.Paths
	HTTPClient *http.Client
	Now        func() time.Time
}

type UpdateResult struct {
	Changed bool
	Count   int
}

func (s Service) Update(ctx context.Context) (UpdateResult, error) {
	cfg, err := config.Load(s.Paths)
	if err != nil {
		return UpdateResult{}, err
	}
	source := strings.TrimSpace(cfg.Routing.GeoIPSource)
	if source == "" {
		source = config.DefaultGeoIPSource
	}
	body, err := s.download(ctx, source)
	if err != nil {
		return UpdateResult{}, s.recordError(cfg, err)
	}
	set, err := ParseIPDeny(body)
	if err != nil {
		return UpdateResult{}, s.recordError(cfg, err)
	}
	old, _ := os.ReadFile(s.Paths.RUGeoIP)
	var next bytes.Buffer
	if err := rules.Write(&next, set); err != nil {
		return UpdateResult{}, s.recordError(cfg, err)
	}
	changed := !bytes.Equal(bytes.TrimSpace(old), bytes.TrimSpace(next.Bytes()))
	if changed {
		if err := rules.Save(s.Paths.RUGeoIP, set); err != nil {
			return UpdateResult{}, s.recordError(cfg, err)
		}
	}
	cfg.Routing.GeoIPLastUpdate = s.now().Format(time.RFC3339)
	cfg.Routing.GeoIPLastError = ""
	if err := config.Save(s.Paths, cfg); err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{Changed: changed, Count: countCIDRs(set)}, nil
}

func (s Service) Status() (string, error) {
	cfg, err := config.Load(s.Paths)
	if err != nil {
		return "", err
	}
	set, err := rules.Load(s.Paths.RUGeoIP)
	count := 0
	if err == nil {
		count = countCIDRs(set)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "GeoIP Россия: %s\n", enabledText(cfg.Routing.RUGeoIPDirectEnabled))
	fmt.Fprintf(&b, "Источник: %s\n", value(cfg.Routing.GeoIPSource))
	fmt.Fprintf(&b, "CIDR в локальном списке: %d\n", count)
	fmt.Fprintf(&b, "Последнее обновление: %s\n", value(cfg.Routing.GeoIPLastUpdate))
	fmt.Fprintf(&b, "Последняя ошибка: %s\n", value(cfg.Routing.GeoIPLastError))
	return b.String(), nil
}

func ParseIPDeny(data []byte) (rules.RuleSet, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var cidrs []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		_, network, err := net.ParseCIDR(line)
		if err != nil {
			return rules.RuleSet{}, fmt.Errorf("GeoIP список содержит некорректную подсеть %q: %w", line, err)
		}
		cidrs = append(cidrs, network.String())
	}
	if err := scanner.Err(); err != nil {
		return rules.RuleSet{}, err
	}
	if len(cidrs) == 0 {
		return rules.RuleSet{}, fmt.Errorf("GeoIP список пустой")
	}
	return rules.RuleSet{Version: 3, Rules: []rules.Rule{{IPCIDR: cidrs}}}, nil
}

func (s Service) download(ctx context.Context, source string) ([]byte, error) {
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("не удалось скачать GeoIP: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("источник GeoIP вернул %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (s Service) recordError(cfg config.Config, err error) error {
	cfg.Routing.GeoIPLastError = err.Error()
	_ = config.Save(s.Paths, cfg)
	return err
}

func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func countCIDRs(set rules.RuleSet) int {
	total := 0
	for _, rule := range set.Rules {
		total += len(rule.IPCIDR)
	}
	return total
}

func enabledText(enabled bool) string {
	if enabled {
		return "включено"
	}
	return "выключено"
}

func value(text string) string {
	if strings.TrimSpace(text) == "" {
		return "нет"
	}
	return strings.TrimSpace(text)
}
