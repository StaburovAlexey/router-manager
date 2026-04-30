package summary

import (
	"fmt"
	"os"
	"strings"

	"vpn-router/internal/config"
)

func Generate(paths config.Paths) (string, error) {
	cfg, err := config.Load(paths)
	if err != nil {
		return "", err
	}
	foreignServers, err := config.LoadForeign(paths)
	if err != nil {
		return "", err
	}
	clientLink := ""
	if data, err := os.ReadFile(paths.ClientLink); err == nil {
		clientLink = strings.TrimSpace(string(data))
	}

	var b strings.Builder
	fmt.Fprintln(&b, "VPN Router Manager")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Настроено на мини-ПК:")
	fmt.Fprintf(&b, "  WAN interface: %s\n", show(cfg.MiniPC.WANInterface))
	fmt.Fprintf(&b, "  AP interface: %s\n", show(cfg.MiniPC.APInterface))
	fmt.Fprintf(&b, "  Wi-Fi SSID: %s\n", show(cfg.MiniPC.SSID))
	fmt.Fprintf(&b, "  Wi-Fi band: %s GHz\n", show(cfg.WiFi.Band))
	fmt.Fprintf(&b, "  Wi-Fi channel: %d\n", cfg.WiFi.Channel)
	fmt.Fprintf(&b, "  Wi-Fi channel width: %d MHz\n", cfg.WiFi.ChannelWidth)
	fmt.Fprintf(&b, "  LAN subnet: %s\n", show(cfg.MiniPC.LANCIDR))
	fmt.Fprintf(&b, "  LAN gateway: %s\n", show(cfg.MiniPC.LANGateway))
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "RU-сервер: %s:%d, SSH %s:%d\n", show(cfg.RUServer.IP), cfg.RUServer.VPNPort, show(cfg.RUServer.SSHUser), cfg.RUServer.SSHPort)
	fmt.Fprintln(&b, "Foreign-серверы:")
	if len(foreignServers.Servers) == 0 {
		fmt.Fprintln(&b, "  не добавлены")
	}
	for _, server := range foreignServers.Servers {
		fmt.Fprintf(&b, "  %s: %s:%d, SNI: %s\n", server.Name, server.IP, server.VPNPort, show(server.Reality.SNI))
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Текущий режим: %s\n", show(cfg.CurrentMode))
	fmt.Fprintf(&b, "VPN policy: %s, custom direct: %t\n", cfg.VPNPolicy.Mode, cfg.VPNPolicy.CustomDirectEnabled)
	fmt.Fprintf(&b, "SNI: auto, выбран: %s\n", show(cfg.Reality.SNI))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "VLESS/REALITY ссылка:")
	if clientLink == "" {
		fmt.Fprintln(&b, "  ещё не создана")
	} else {
		fmt.Fprintf(&b, "  %s\n", clientLink)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "QR-код:")
	fmt.Fprintln(&b, "  sudo vpn-router qr")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Основные команды:")
	for _, command := range []string{
		"sudo vpn-router",
		"sudo vpn-router bootstrap",
		"sudo vpn-router setup",
		"sudo vpn-router vpn",
		"sudo vpn-router direct",
		"sudo vpn-router status",
		"sudo vpn-router logs",
		"sudo vpn-router info",
		"sudo vpn-router qr",
	} {
		fmt.Fprintf(&b, "  %s\n", command)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Команды Wi-Fi:")
	for _, command := range []string{
		"sudo vpn-router wifi status",
		"sudo vpn-router wifi scan",
		"sudo vpn-router wifi set-band 2.4",
		"sudo vpn-router wifi set-band 5",
		"sudo vpn-router wifi set-channel <channel>",
		"sudo vpn-router wifi set-width <20|40|80>",
		"sudo vpn-router wifi auto-channel",
		"sudo vpn-router wifi restart",
	} {
		fmt.Fprintf(&b, "  %s\n", command)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Примеры сайтов/IP без VPN:")
	fmt.Fprintln(&b, "  sudo vpn-router direct-add suffix gosuslugi.ru")
	fmt.Fprintln(&b, "  sudo vpn-router direct-add suffix sberbank.ru")
	fmt.Fprintln(&b, "  sudo vpn-router direct-add domain login.example.com")
	fmt.Fprintln(&b, "  sudo vpn-router direct-add ip 1.2.3.4")
	fmt.Fprintln(&b, "  sudo vpn-router direct-add cidr 203.0.113.0/24")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Файлы конфигурации:")
	fmt.Fprintf(&b, "  %s\n", paths.BaseDir)
	fmt.Fprintf(&b, "  %s\n", paths.Config)
	fmt.Fprintf(&b, "  %s\n", paths.ForeignServers)
	fmt.Fprintf(&b, "  %s\n", paths.CustomDirect)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Снова открыть эту информацию:")
	fmt.Fprintln(&b, "  sudo vpn-router info")
	return b.String(), nil
}

func Save(paths config.Paths) (string, error) {
	text, err := Generate(paths)
	if err != nil {
		return "", err
	}
	return text, config.WriteSensitiveText(paths.InstallSummary, text)
}

func show(value string) string {
	if strings.TrimSpace(value) == "" {
		return "не настроено"
	}
	return value
}
