package ux

import (
	"fmt"
	"strings"
)

func FriendlyError(err error) string {
	if err == nil {
		return ""
	}
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return "Не получилось выполнить действие."
	}
	var b strings.Builder
	fmt.Fprintln(&b, "Не получилось выполнить действие.")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Причина: %s\n", text)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Что можно сделать:")
	for _, action := range actionsFor(text) {
		fmt.Fprintf(&b, "- %s\n", action)
	}
	return strings.TrimRight(b.String(), "\n")
}

func actionsFor(text string) []string {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "подготовка системы не завершена"):
		return []string{
			"исправьте исходную причину ошибки выше",
			"запустите подготовку снова: sudo router-manager",
			"если нужна ручная очистка локальных данных и команда router-manager уже доступна: sudo router-manager uninstall --keep-deps",
		}
	case strings.Contains(lower, "root") || strings.Contains(lower, "sudo"):
		return []string{"запустите приложение с правами администратора: sudo router-manager"}
	case strings.Contains(lower, "qr-ссылка") || strings.Contains(lower, "client-link"):
		return []string{"завершите первичную настройку", "после настройки откройте: Подключить устройство -> Показать QR-код клиента"}
	case strings.Contains(lower, "wi-fi адаптер") || strings.Contains(lower, "ap mode") || strings.Contains(lower, "p2p-device"):
		return []string{"откройте Wi-Fi -> Показать Wi-Fi настройки", "выберите обычный Wi-Fi адаптер для раздачи, не P2P-device", "если сомневаетесь, используйте отдельный USB Wi-Fi адаптер"}
	case strings.Contains(lower, "канал") || strings.Contains(lower, "ширина"):
		return []string{"откройте Wi-Fi -> Автоподбор канала", "если проблема повторится, выберите диапазон 2.4 GHz и ширину 20 MHz"}
	case strings.Contains(lower, "hostapd"):
		return []string{"откройте Проблемы и диагностика -> Показать логи", "попробуйте Wi-Fi -> Перезапустить Wi-Fi"}
	case strings.Contains(lower, "dnsmasq") || strings.Contains(lower, "dns"):
		return []string{"откройте Проблемы и диагностика -> Показать логи", "попробуйте Интернет -> Отключить маршрут через сервер, затем Интернет -> Включить маршрут через сервер"}
	case strings.Contains(lower, "sing-box") || strings.Contains(lower, "tunnel"):
		return []string{"откройте Проблемы и диагностика -> Показать логи", "проверьте доступность входного и выходного серверов"}
	case strings.Contains(lower, "ssh"):
		return []string{"проверьте IP, порт и пользователя VPS", "если парольный SSH отключён, добавьте ключ мини-ПК на сервер через панель провайдера"}
	case strings.Contains(lower, "не найден") || strings.Contains(lower, "not found"):
		return []string{"откройте Проблемы и диагностика -> Собрать отчёт без секретов", "если это первая установка, запустите: sudo router-manager"}
	default:
		return []string{"откройте Проблемы и диагностика -> Краткая диагностика", "если непонятно, соберите отчёт без секретов и проверьте пункты с ошибками"}
	}
}
