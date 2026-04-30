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
	fmt.Fprintln(&b, "Настройка завершена.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Что делать дальше:")
	fmt.Fprintf(&b, "  1. Подключите телефон или ноутбук к Wi-Fi сети %s.\n", show(cfg.MiniPC.SSID))
	fmt.Fprintln(&b, "  2. Откройте меню: sudo vpn-router")
	fmt.Fprintln(&b, "  3. Выберите \"Включить VPN-режим\" и проверьте интернет.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Wi-Fi раздача:")
	fmt.Fprintf(&b, "  Сеть: %s\n", show(cfg.MiniPC.SSID))
	fmt.Fprintf(&b, "  Адаптер: %s\n", show(cfg.MiniPC.APInterface))
	fmt.Fprintf(&b, "  Диапазон: %s GHz, канал %d, ширина %d MHz\n", show(cfg.WiFi.Band), cfg.WiFi.Channel, cfg.WiFi.ChannelWidth)
	fmt.Fprintf(&b, "  Подсеть: %s, шлюз: %s\n", show(cfg.MiniPC.LANCIDR), show(cfg.MiniPC.LANGateway))
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Входной VPN-сервер: %s:%d, SSH %s:%d\n", show(cfg.RUServer.IP), cfg.RUServer.VPNPort, show(cfg.RUServer.SSHUser), cfg.RUServer.SSHPort)
	fmt.Fprintln(&b, "Выходные VPN-серверы:")
	if len(foreignServers.Servers) == 0 {
		fmt.Fprintln(&b, "  не добавлены")
	}
	for _, server := range foreignServers.Servers {
		fmt.Fprintf(&b, "  %s: %s:%d, SNI: %s\n", server.Name, server.IP, server.VPNPort, show(server.Reality.SNI))
	}
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Текущий режим: %s\n", show(cfg.CurrentMode))
	fmt.Fprintf(&b, "SNI: auto, выбран: %s\n", show(cfg.Reality.SNI))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Ссылка для QR-кода:")
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
		"sudo vpn-router                  открыть меню",
		"sudo vpn-router status           проверить состояние",
		"sudo vpn-router qr               показать QR-код",
		"sudo vpn-router restore-network  откатить локальную сеть",
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
