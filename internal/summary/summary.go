package summary

import (
	"fmt"
	"os"
	"strings"

	"router-manager/internal/config"
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
	fmt.Fprintln(&b, "Router Manager")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Настройка завершена.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Что делать дальше обычному пользователю:")
	fmt.Fprintf(&b, "  1. Подключите телефон или ноутбук к Wi-Fi сети: %s.\n", show(cfg.MiniPC.SSID))
	fmt.Fprintln(&b, "  2. Откройте меню: sudo router-manager")
	fmt.Fprintln(&b, "  3. Выберите: Интернет -> Включить маршрут через сервер.")
	fmt.Fprintln(&b, "  4. Если интернет не работает, выберите: Проблемы и диагностика -> Краткая диагностика.")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Быстрые действия:")
	fmt.Fprintln(&b, "  sudo router-manager                  открыть меню")
	fmt.Fprintln(&b, "  sudo router-manager status           проверить состояние")
	fmt.Fprintln(&b, "  sudo router-manager report           собрать отчёт без секретов")
	fmt.Fprintln(&b, "  sudo router-manager qr               показать QR-код клиента")
	fmt.Fprintln(&b, "  sudo router-manager direct-add site example.com")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Подключение:")
	fmt.Fprintf(&b, "  Сеть: %s\n", show(cfg.MiniPC.SSID))
	fmt.Fprintf(&b, "  Текущий режим: %s\n", show(cfg.CurrentMode))
	fmt.Fprintln(&b, "  QR-код клиента: sudo router-manager qr")
	if clientLink == "" {
		fmt.Fprintln(&b, "  Ссылка клиента: ещё не создана")
	} else {
		fmt.Fprintln(&b, "  Ссылка клиента: создана")
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Технические сведения:")
	fmt.Fprintln(&b, "  Wi-Fi раздача:")
	fmt.Fprintf(&b, "    Адаптер: %s\n", show(cfg.MiniPC.APInterface))
	fmt.Fprintf(&b, "    Диапазон: %s GHz, канал %d, ширина %d MHz\n", show(cfg.WiFi.Band), cfg.WiFi.Channel, cfg.WiFi.ChannelWidth)
	fmt.Fprintf(&b, "    Подсеть: %s, шлюз: %s\n", show(cfg.MiniPC.LANCIDR), show(cfg.MiniPC.LANGateway))
	fmt.Fprintf(&b, "  Входной сервер: %s:%d, SSH %s:%d\n", show(cfg.RUServer.IP), cfg.RUServer.TunnelPort, show(cfg.RUServer.SSHUser), cfg.RUServer.SSHPort)
	fmt.Fprintln(&b, "  Выходные серверы:")
	if len(foreignServers.Servers) == 0 {
		fmt.Fprintln(&b, "    не добавлены")
	}
	for _, server := range foreignServers.Servers {
		fmt.Fprintf(&b, "    %s: %s:%d, SNI: %s\n", server.Name, server.IP, server.TunnelPort, show(server.Reality.SNI))
	}
	fmt.Fprintf(&b, "  SNI: auto, выбран: %s\n", show(cfg.Reality.SNI))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Файлы конфигурации:")
	fmt.Fprintf(&b, "  %s\n", paths.BaseDir)
	fmt.Fprintf(&b, "  %s\n", paths.Config)
	fmt.Fprintf(&b, "  %s\n", paths.ForeignServers)
	fmt.Fprintf(&b, "  %s\n", paths.CustomDirect)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Снова открыть эту информацию:")
	fmt.Fprintln(&b, "  sudo router-manager info")
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
