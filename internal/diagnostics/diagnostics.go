package diagnostics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vpn-router/internal/config"
	"vpn-router/internal/network"
	"vpn-router/internal/shell"
	"vpn-router/internal/singbox"
	"vpn-router/internal/system"
)

type Status struct {
	Mode            string
	WANInterface    string
	APInterface     string
	SSID            string
	WiFiBand        string
	WiFiChannel     int
	WiFiWidth       int
	SingBox         string
	Hostapd         string
	Dnsmasq         string
	Nftables        string
	RUServer        string
	ForeignMode     string
	SelectedForeign string
	PublicIP        string
	DNS             string
	WiFiClientCount string
	RFKill          string
}

func Collect(ctx context.Context, runner shell.Runner, paths config.Paths) (Status, error) {
	cfg, err := config.Load(paths)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Mode:            cfg.CurrentMode,
		WANInterface:    cfg.MiniPC.WANInterface,
		APInterface:     cfg.MiniPC.APInterface,
		SSID:            cfg.MiniPC.SSID,
		WiFiBand:        cfg.WiFi.Band,
		WiFiChannel:     cfg.WiFi.Channel,
		WiFiWidth:       cfg.WiFi.ChannelWidth,
		SingBox:         singbox.Status(ctx, runner),
		Hostapd:         system.ServiceStatus(ctx, runner, "hostapd"),
		Dnsmasq:         system.ServiceStatus(ctx, runner, "dnsmasq"),
		Nftables:        nftablesStatus(ctx, runner),
		RUServer:        cfg.RUServer.IP,
		PublicIP:        network.PublicIP(ctx, runner),
		DNS:             dnsStatus(ctx, runner),
		WiFiClientCount: wifiClients(ctx, runner, cfg.MiniPC.APInterface),
		RFKill:          rfkillStatus(ctx, runner),
	}
	status.ForeignMode, status.SelectedForeign = ruState(ctx, runner, cfg)
	return status, nil
}

func Format(status Status) string {
	var b strings.Builder
	fmt.Fprintln(&b, "Краткий статус:")
	fmt.Fprintf(&b, "Интернет на устройстве: %s\n", internetSummary(status))
	fmt.Fprintf(&b, "Wi-Fi раздача: %s\n", wifiSummary(status))
	fmt.Fprintf(&b, "VPN: %s\n", vpnSummary(status))
	fmt.Fprintf(&b, "DNS: %s\n", dnsSummary(status))
	fmt.Fprintf(&b, "Клиентов Wi-Fi: %s\n", value(status.WiFiClientCount))
	recommendations := recommendations(status)
	if len(recommendations) > 0 {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, "Что проверить:")
		for _, item := range recommendations {
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Технические детали:")
	fmt.Fprintf(&b, "Текущий режим: %s\n", value(status.Mode))
	fmt.Fprintf(&b, "Входящий интернет: %s\n", value(status.WANInterface))
	fmt.Fprintf(&b, "Wi-Fi адаптер для раздачи: %s\n", value(status.APInterface))
	fmt.Fprintf(&b, "Wi-Fi SSID: %s\n", value(status.SSID))
	fmt.Fprintf(&b, "Wi-Fi: %s GHz, канал %d, ширина %d MHz\n", value(status.WiFiBand), status.WiFiChannel, status.WiFiWidth)
	fmt.Fprintf(&b, "sing-box: %s\n", status.SingBox)
	fmt.Fprintf(&b, "hostapd: %s\n", status.Hostapd)
	fmt.Fprintf(&b, "dnsmasq: %s\n", status.Dnsmasq)
	fmt.Fprintf(&b, "nftables: %s\n", status.Nftables)
	fmt.Fprintf(&b, "Входной VPN-сервер: %s\n", value(status.RUServer))
	fmt.Fprintf(&b, "Режим выходных серверов: %s\n", value(status.ForeignMode))
	fmt.Fprintf(&b, "Выбранный выходной сервер: %s\n", value(status.SelectedForeign))
	fmt.Fprintf(&b, "Публичный IP: %s\n", value(status.PublicIP))
	fmt.Fprintf(&b, "DNS: %s\n", value(status.DNS))
	fmt.Fprintf(&b, "rfkill: %s\n", value(status.RFKill))
	return b.String()
}

func internetSummary(status Status) string {
	if status.PublicIP != "" && status.PublicIP != "не удалось определить" {
		return "OK (" + status.PublicIP + ")"
	}
	return "ошибка"
}

func wifiSummary(status Status) string {
	if status.Hostapd == "active" && status.Dnsmasq == "active" && status.RFKill == "ok" {
		return "OK"
	}
	return "ошибка"
}

func vpnSummary(status Status) string {
	switch status.Mode {
	case "vpn":
		if status.SingBox == "active" {
			return "включён"
		}
		return "ошибка"
	case "direct":
		return "выключен, прямой интернет"
	default:
		return value(status.Mode)
	}
}

func dnsSummary(status Status) string {
	if status.DNS == "ok" {
		return "OK"
	}
	return "ошибка"
}

func recommendations(status Status) []string {
	var result []string
	if internetSummary(status) == "ошибка" {
		result = append(result, "на мини-ПК нет внешнего интернета или не определяется публичный IP")
	}
	if status.RFKill != "ok" {
		result = append(result, "Wi-Fi заблокирован rfkill; выполните rfkill unblock wifi")
	}
	if status.Hostapd != "active" {
		result = append(result, "Wi-Fi точка доступа не запущена; попробуйте перезапустить Wi-Fi")
	}
	if status.Dnsmasq != "active" {
		result = append(result, "DHCP/DNS для Wi-Fi не запущен; проверьте логи dnsmasq")
	}
	if status.Mode == "vpn" && status.SingBox != "active" {
		result = append(result, "VPN включён в настройках, но sing-box не запущен; откройте логи")
	}
	if status.DNS != "ok" {
		result = append(result, "DNS не отвечает; перезапустите VPN-режим или проверьте логи sing-box")
	}
	return result
}

func Logs(ctx context.Context, runner shell.Runner) (string, error) {
	out, err := runner.Output(ctx, "journalctl", "-u", "sing-box", "-u", "hostapd", "-u", "dnsmasq", "-n", "200", "--no-pager")
	if err != nil {
		return "", err
	}
	return out, nil
}

func dnsStatus(ctx context.Context, runner shell.Runner) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := runner.Run(ctx, "resolvectl", "query", "example.com"); err == nil {
		return "ok"
	}
	if err := runner.Run(ctx, "dig", "+short", "example.com"); err == nil {
		return "ok"
	}
	return "ошибка"
}

func nftablesStatus(ctx context.Context, runner shell.Runner) string {
	service := system.ServiceStatus(ctx, runner, "nftables")
	out, err := runner.Output(ctx, "nft", "list", "table", "inet", "vpn_router")
	if err == nil && strings.TrimSpace(out) != "" {
		if service == "active" {
			return "active"
		}
		return "rules loaded, service " + service
	}
	return service
}

func wifiClients(ctx context.Context, runner shell.Runner, iface string) string {
	if iface == "" {
		return "неизвестно"
	}
	out, err := runner.Output(ctx, "iw", "dev", iface, "station", "dump")
	if err != nil {
		return "неизвестно"
	}
	count := strings.Count(out, "Station ")
	return fmt.Sprintf("%d", count)
}

func rfkillStatus(ctx context.Context, runner shell.Runner) string {
	out, err := runner.Output(ctx, "rfkill", "list")
	if err != nil {
		return "неизвестно"
	}
	if strings.Contains(out, "Wireless LAN") && strings.Contains(out, "Soft blocked: yes") {
		return "Wi-Fi soft blocked"
	}
	return "ok"
}

func ruState(ctx context.Context, runner shell.Runner, cfg config.Config) (string, string) {
	if cfg.RUServer.IP == "" {
		return "не настроено", ""
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := runner.Output(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=2", "-p", fmt.Sprint(cfg.RUServer.SSHPort), cfg.RUServer.SSHUser+"@"+cfg.RUServer.IP, "cat /etc/ru-vpn/state.json 2>/dev/null || true")
	if err != nil || out == "" {
		return "неизвестно", ""
	}
	mode := "unknown"
	selected := ""
	if strings.Contains(out, `"mode":"auto"`) || strings.Contains(out, `"mode": "auto"`) {
		mode = "auto"
	}
	if strings.Contains(out, `"mode":"manual"`) || strings.Contains(out, `"mode": "manual"`) {
		mode = "manual"
	}
	for _, marker := range []string{`"selected_foreign":"`, `"selected_foreign": "`} {
		idx := strings.Index(out, marker)
		if idx >= 0 {
			rest := out[idx+len(marker):]
			end := strings.Index(rest, `"`)
			if end >= 0 {
				selected = rest[:end]
			}
		}
	}
	return mode, selected
}

func value(value string) string {
	if strings.TrimSpace(value) == "" {
		return "не настроено"
	}
	return value
}
