package network

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

var PrivateCIDRs = []string{
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"127.0.0.0/8",
	"169.254.0.0/16",
	"fc00::/7",
	"fe80::/10",
}

func DefaultWANInterface(ctx context.Context, runner shell.Runner) (string, error) {
	out, err := runner.Output(ctx, "ip", "route", "show", "default")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	for i := 0; i < len(fields)-1; i++ {
		if fields[i] == "dev" {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("не удалось определить входящий интернет из default route")
}

type InterfaceInfo struct {
	Name      string
	State     string
	Kind      string
	IPv4      []string
	IsDefault bool
}

func Interfaces(ctx context.Context, runner shell.Runner) ([]InterfaceInfo, error) {
	defaultWAN, _ := DefaultWANInterface(ctx, runner)
	out, err := runner.Output(ctx, "ip", "-o", "link", "show")
	if err != nil {
		return nil, err
	}
	var result []InterfaceInfo
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		info, ok := parseLinkLine(line)
		if !ok || info.Name == "lo" {
			continue
		}
		info.IsDefault = info.Name == defaultWAN
		info.IPv4 = ipv4Addrs(ctx, runner, info.Name)
		result = append(result, info)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].IsDefault != result[j].IsDefault {
			return result[i].IsDefault
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func parseLinkLine(line string) (InterfaceInfo, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return InterfaceInfo{}, false
	}
	name := strings.TrimSuffix(fields[1], ":")
	if at, _, ok := strings.Cut(name, "@"); ok {
		name = at
	}
	info := InterfaceInfo{Name: name, State: "UNKNOWN", Kind: "network"}
	for i := 0; i < len(fields); i++ {
		switch fields[i] {
		case "state":
			if i+1 < len(fields) {
				info.State = fields[i+1]
			}
		case "link/ether":
			info.Kind = "ethernet"
		case "link/loopback":
			info.Kind = "loopback"
		}
	}
	return info, true
}

func ipv4Addrs(ctx context.Context, runner shell.Runner, iface string) []string {
	out, err := runner.Output(ctx, "ip", "-o", "-4", "addr", "show", "dev", iface)
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		for i := 0; i < len(fields)-1; i++ {
			if fields[i] == "inet" {
				result = append(result, fields[i+1])
			}
		}
	}
	return result
}

func HasInternet(ctx context.Context, runner shell.Runner) error {
	if err := runner.Run(ctx, "ping", "-c", "1", "-W", "3", "1.1.1.1"); err != nil {
		return fmt.Errorf("нет доступа в интернет на мини-ПК: %w", err)
	}
	return nil
}

func ConfigureLANInterface(ctx context.Context, runner shell.Runner, cfg config.Config) error {
	if cfg.MiniPC.APInterface == "" {
		return fmt.Errorf("Wi-Fi адаптер для раздачи не настроен")
	}
	gateway, err := GatewayCIDR(cfg.MiniPC.LANGateway, cfg.MiniPC.LANCIDR)
	if err != nil {
		return err
	}
	if err := runner.Run(ctx, "ip", "link", "set", cfg.MiniPC.APInterface, "up"); err != nil {
		return err
	}
	return runner.Run(ctx, "ip", "addr", "replace", gateway, "dev", cfg.MiniPC.APInterface)
}

func EnableIPv4Forwarding(ctx context.Context, runner shell.Runner, sysctlPath string) error {
	if sysctlPath == "" {
		sysctlPath = "/etc/sysctl.d/99-router-manager.conf"
	}
	if err := os.MkdirAll(filepath.Dir(sysctlPath), 0o755); err != nil {
		return fmt.Errorf("не удалось создать каталог для %s: %w", sysctlPath, err)
	}
	if err := os.WriteFile(sysctlPath, []byte("net.ipv4.ip_forward=1\n"), 0o644); err != nil {
		return fmt.Errorf("не удалось записать %s: %w", sysctlPath, err)
	}
	return runner.Run(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1")
}

func CheckIPv4Forwarding(ctx context.Context, runner shell.Runner) error {
	out, err := runner.Output(ctx, "sysctl", "-n", "net.ipv4.ip_forward")
	if err != nil {
		return fmt.Errorf("не удалось проверить IPv4 forwarding: %w", err)
	}
	if strings.TrimSpace(out) != "1" {
		return fmt.Errorf("IPv4 forwarding выключен")
	}
	return nil
}

func IPv4ForwardingStatus(ctx context.Context, runner shell.Runner) string {
	if err := CheckIPv4Forwarding(ctx, runner); err != nil {
		return err.Error()
	}
	return "ok"
}

func GatewayCIDR(gateway string, lanCIDR string) (string, error) {
	gatewayIP := net.ParseIP(strings.TrimSpace(gateway))
	if gatewayIP == nil || gatewayIP.To4() == nil {
		return "", fmt.Errorf("LAN gateway должен быть IPv4-адресом")
	}
	_, network, err := net.ParseCIDR(strings.TrimSpace(lanCIDR))
	if err != nil {
		return "", fmt.Errorf("LAN subnet содержит некорректный CIDR: %w", err)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return "", fmt.Errorf("LAN subnet должен быть IPv4 CIDR")
	}
	if !network.Contains(gatewayIP) {
		return "", fmt.Errorf("LAN gateway %s не входит в subnet %s", gateway, lanCIDR)
	}
	return fmt.Sprintf("%s/%d", gatewayIP.String(), ones), nil
}

func PublicIP(ctx context.Context, runner shell.Runner) string {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := runner.Output(ctx, "curl", "-fsS", "--connect-timeout", "1", "--max-time", "2", "https://api.ipify.org")
	if err != nil {
		return "не удалось определить"
	}
	if net.ParseIP(strings.TrimSpace(out)) == nil {
		return "не удалось определить"
	}
	return strings.TrimSpace(out)
}
