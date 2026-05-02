package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"router-manager/internal/config"
	"router-manager/internal/confirm"
	"router-manager/internal/diagnostics"
	"router-manager/internal/foreign"
	"router-manager/internal/modes"
	"router-manager/internal/restore"
	"router-manager/internal/ru"
	"router-manager/internal/rules"
	"router-manager/internal/selfupdate"
	"router-manager/internal/shell"
	"router-manager/internal/sshclient"
	"router-manager/internal/summary"
	"router-manager/internal/system"
	"router-manager/internal/uninstall"
	"router-manager/internal/ux"
	"router-manager/internal/wifi"
)

type viewMode string

const (
	modeMenu    viewMode = "menu"
	modePrompt  viewMode = "prompt"
	modeForm    viewMode = "form"
	modeConfirm viewMode = "confirm"
)

type item struct {
	title  string
	action string
}

type promptState struct {
	title  string
	label  string
	def    string
	action string
	input  string
}

type formField struct {
	key   string
	label string
	def   string
}

type formState struct {
	title  string
	action string
	fields []formField
	index  int
	input  string
	values map[string]string
}

type confirmState struct {
	title  string
	body   string
	action string
	value  string
	params map[string]string
	input  string
}

type model struct {
	ctx         context.Context
	paths       config.Paths
	runner      shell.Runner
	version     string
	requireRoot func() error
	cursor      int
	menu        string
	mode        viewMode
	message     string
	prompt      promptState
	form        formState
	confirm     confirmState
}

func Run(ctx context.Context, paths config.Paths, runner shell.Runner, version string) error {
	m := model{
		ctx:     ctx,
		paths:   paths,
		runner:  runner,
		version: version,
		menu:    "main",
		mode:    modeMenu,
	}
	_, err := tea.NewProgram(m).Run()
	return err
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	if key.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.mode {
	case modePrompt:
		return m.updatePrompt(key), nil
	case modeForm:
		return m.updateForm(key), nil
	case modeConfirm:
		return m.updateConfirm(key), nil
	default:
		next := m.updateMenu(key)
		if next.menu == "quit" {
			return next, tea.Quit
		}
		return next, nil
	}
}

func (m model) updateMenu(key tea.KeyMsg) model {
	switch key.String() {
	case "q", "esc":
		if m.menu == "main" {
			m.menu = "quit"
			return m
		}
		return m.setMenu("main")
	case "0":
		if m.menu == "main" {
			m.menu = "quit"
			return m
		}
		return m.setMenu("main")
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items())-1 {
			m.cursor++
		}
	case "enter":
		return m.selectMenuItem(m.cursor)
	default:
		if len(key.Runes) == 1 && key.Runes[0] >= '1' && key.Runes[0] <= '9' {
			return m.selectMenuItem(int(key.Runes[0] - '1'))
		}
	}
	return m
}

func (m model) selectMenuItem(index int) model {
	items := m.items()
	if index < 0 || index >= len(items) {
		return m
	}
	selected := items[index]
	if selected.action == "quit" {
		m.menu = "quit"
		return m
	}
	return m.run(selected.action)
}

func (m model) updatePrompt(key tea.KeyMsg) model {
	switch key.String() {
	case "esc":
		m.mode = modeMenu
		m.prompt = promptState{}
		return m
	case "enter":
		value := strings.TrimSpace(m.prompt.input)
		if value == "" {
			value = m.prompt.def
		}
		m.mode = modeMenu
		return m.runPrompt(m.prompt.action, value)
	case "backspace", "ctrl+h":
		m.prompt.input = trimLastRune(m.prompt.input)
	default:
		if len(key.Runes) > 0 {
			m.prompt.input += string(key.Runes)
		}
	}
	return m
}

func (m model) updateForm(key tea.KeyMsg) model {
	switch key.String() {
	case "esc":
		m.mode = modeMenu
		m.form = formState{}
		return m
	case "enter":
		if len(m.form.fields) == 0 {
			m.mode = modeMenu
			return m
		}
		field := m.form.fields[m.form.index]
		value := strings.TrimSpace(m.form.input)
		if value == "" {
			value = field.def
		}
		m.form.values[field.key] = value
		m.form.index++
		m.form.input = ""
		if m.form.index >= len(m.form.fields) {
			form := m.form
			m.mode = modeMenu
			m.form = formState{}
			return m.runForm(form)
		}
	case "backspace", "ctrl+h":
		m.form.input = trimLastRune(m.form.input)
	default:
		if len(key.Runes) > 0 {
			m.form.input += string(key.Runes)
		}
	}
	return m
}

func (m model) updateConfirm(key tea.KeyMsg) model {
	switch key.String() {
	case "esc":
		m.mode = modeMenu
		m.message = "Операция отменена."
		m.confirm = confirmState{}
	case "enter":
		answer, ok := confirm.ParseYesNo(m.confirm.input)
		if ok && answer {
			confirm := m.confirm
			m.mode = modeMenu
			m.message = ""
			m.confirm = confirmState{}
			return m.runConfirm(confirm)
		}
		if ok {
			m.mode = modeMenu
			m.message = "Операция отменена."
			m.confirm = confirmState{}
			return m
		}
		m.message = confirm.RequiredMessage
		m.confirm.input = ""
	case "backspace", "ctrl+h":
		m.confirm.input = trimLastRune(m.confirm.input)
	default:
		if len(key.Runes) > 0 {
			m.confirm.input += string(key.Runes)
		}
	}
	return m
}

func (m model) View() string {
	if m.menu == "quit" {
		return ""
	}
	switch m.mode {
	case modePrompt:
		return m.viewPrompt()
	case modeForm:
		return m.viewForm()
	case modeConfirm:
		return m.viewConfirm()
	default:
		return m.viewMenu()
	}
}

func (m model) viewMenu() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Router Manager / %s\n\n", m.menuTitle())
	for i, item := range m.items() {
		cursor := " "
		if m.cursor == i {
			cursor = ">"
		}
		if item.action == "back" || item.action == "quit" {
			fmt.Fprintf(&b, "%s 0) %s\n", cursor, item.title)
			continue
		}
		fmt.Fprintf(&b, "%s %d) %s\n", cursor, i+1, item.title)
	}
	if strings.TrimSpace(m.message) != "" {
		fmt.Fprintln(&b)
		fmt.Fprintln(&b, m.message)
	}
	fmt.Fprintln(&b)
	if m.menu == "main" {
		fmt.Fprintln(&b, "Enter - выбрать, q - выход")
	} else {
		fmt.Fprintln(&b, "Enter - выбрать, Esc/q - назад")
	}
	return b.String()
}

func (m model) viewPrompt() string {
	value := m.prompt.input
	if value == "" && m.prompt.def != "" {
		value = "[" + m.prompt.def + "]"
	}
	return fmt.Sprintf("Router Manager / %s\n\n%s: %s\n\nEnter - применить, Esc - отмена\n", m.prompt.title, m.prompt.label, value)
}

func (m model) viewForm() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Router Manager / %s\n\n", m.form.title)
	for i, field := range m.form.fields {
		if i < m.form.index {
			fmt.Fprintf(&b, "%s: %s\n", field.label, m.form.values[field.key])
			continue
		}
		if i == m.form.index {
			value := m.form.input
			if value == "" && field.def != "" {
				value = "[" + field.def + "]"
			}
			fmt.Fprintf(&b, "> %s: %s\n", field.label, value)
			continue
		}
		fmt.Fprintf(&b, "%s: \n", field.label)
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Enter - дальше, Esc - отмена")
	return b.String()
}

func (m model) viewConfirm() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Router Manager / %s\n\n%s\n\n", m.confirm.title, m.confirm.body)
	if strings.TrimSpace(m.message) != "" {
		fmt.Fprintln(&b, m.message)
		fmt.Fprintln(&b)
	}
	fmt.Fprintf(&b, "Ответ: %s\n\n", m.confirm.input)
	fmt.Fprintln(&b, "Введите да для подтверждения или нет для отмены. Yes/no тоже работают. Esc - отмена")
	return b.String()
}

func (m model) setMenu(menu string) model {
	m.menu = menu
	m.cursor = 0
	m.mode = modeMenu
	return m
}

func (m model) items() []item {
	switch m.menu {
	case "internet":
		return []item{
			{"Проверить состояние интернета", "status"},
			{"Включить маршрут через сервер", "tunnel"},
			{"Отключить маршрут через сервер / прямой интернет", "direct"},
			{"Выбрать выходной сервер", "menu:foreign-switch"},
			{"Автоматически выбирать выходной сервер", "ru-auto"},
			{"Назад", "back"},
		}
	case "connect":
		return []item{
			{"Показать данные для подключения", "connect-info"},
			{"Показать QR-код клиента", "qr"},
			{"Проверить состояние", "status"},
			{"Назад", "back"},
		}
	case "direct-rules":
		return []item{
			{"Показать сайты и адреса", "direct-list-human"},
			{"Добавить сайт или IP", "direct-add:auto"},
			{"Удалить сайт или IP", "direct-remove"},
			{"Расширенно: домен с поддоменами", "direct-add:suffix"},
			{"Расширенно: только точный домен", "direct-add:domain"},
			{"Расширенно: IP", "direct-add:ip"},
			{"Расширенно: подсеть CIDR", "direct-add:cidr"},
			{"Назад", "back"},
		}
	case "ru":
		return []item{
			{"Статус входного сервера", "ru-status"},
			{"Автоматически выбирать выходной сервер", "ru-auto"},
			{"Выбрать выходной сервер вручную", "menu:foreign-switch"},
			{"Проверить выходной сервер", "menu:foreign-test"},
			{"Показать выходные серверы", "foreign-list"},
			{"Логи входного сервера", "ru-logs"},
			{"Откатить конфиг входного сервера", "ru-rollback"},
			{"Назад", "back"},
		}
	case "foreign":
		return []item{
			{"Показать выходные серверы", "foreign-list"},
			{"Переключить входной сервер на выходной", "menu:foreign-switch"},
			{"Добавить выходной сервер", "foreign-add"},
			{"Проверить SSH выходного сервера", "foreign-test"},
			{"Удалить выходной сервер из схемы", "foreign-remove"},
			{"Очистить VPS выходного сервера", "foreign-cleanup"},
			{"Назад", "back"},
		}
	case "foreign-switch":
		return m.foreignSwitchItems()
	case "foreign-test":
		return m.foreignTestItems()
	case "wifi":
		return []item{
			{"Показать Wi-Fi настройки", "wifi-status"},
			{"Изменить название сети", "wifi-ssid"},
			{"Изменить пароль", "wifi-password"},
			{"Сменить Wi-Fi адаптер для раздачи", "menu:wifi-adapter"},
			{"Сканировать соседние сети", "wifi-scan"},
			{"Автоподбор канала", "wifi-auto-channel"},
			{"Перезапустить Wi-Fi", "wifi-restart"},
			{"Выбрать диапазон", "menu:wifi-band"},
			{"Изменить канал", "wifi-channel"},
			{"Ширина канала 20 MHz", "wifi-width:20"},
			{"Ширина канала 40 MHz", "wifi-width:40"},
			{"Ширина канала 80 MHz", "wifi-width:80"},
			{"Назад", "back"},
		}
	case "wifi-adapter":
		return m.wifiAdapterItems()
	case "wifi-band":
		return m.wifiBandItems()
	case "problems":
		return []item{
			{"Краткая диагностика", "diagnostics"},
			{"Собрать отчёт без секретов", "diagnostic-report"},
			{"Показать логи", "logs"},
			{"Перезапустить Wi-Fi", "wifi-restart"},
			{"Откатить локальную сеть до состояния без приложения", "restore-network"},
			{"Назад", "back"},
		}
	case "maintenance":
		return []item{
			{"Информация по настройке", "info"},
			{"Показать QR-код клиента", "qr"},
			{"Резервные копии и откат", "menu:backup"},
			{"Обновить приложение", "update"},
			{"Назад", "back"},
		}
	case "advanced":
		return []item{
			{"Входной сервер", "menu:ru"},
			{"Выходные серверы", "menu:foreign"},
			{"Правила прямого доступа в JSON", "direct-list"},
			{"Назад", "back"},
		}
	case "backup":
		return []item{
			{"Показать локальные backups", "backup-list"},
			{"Откатить конфиг входного сервера", "ru-rollback"},
			{"Назад", "back"},
		}
	default:
		return []item{
			{"Интернет", "menu:internet"},
			{"Подключить устройство", "menu:connect"},
			{"Сайты прямого доступа", "menu:direct-rules"},
			{"Wi-Fi", "menu:wifi"},
			{"Проблемы и диагностика", "menu:problems"},
			{"Обслуживание", "menu:maintenance"},
			{"Расширенное", "menu:advanced"},
			{"Полностью удалить приложение", "uninstall"},
			{"Выход", "quit"},
		}
	}
}

func (m model) menuTitle() string {
	switch m.menu {
	case "internet":
		return "интернет"
	case "connect":
		return "подключение устройства"
	case "direct-rules":
		return "сайты прямого доступа"
	case "ru":
		return "входной сервер"
	case "foreign":
		return "выходные серверы"
	case "foreign-switch":
		return "выбор выходного сервера"
	case "foreign-test":
		return "проверка выходного сервера"
	case "wifi":
		return "Wi-Fi"
	case "wifi-adapter":
		return "смена Wi-Fi адаптера"
	case "wifi-band":
		return "выбор диапазона Wi-Fi"
	case "problems":
		return "проблемы и диагностика"
	case "maintenance":
		return "обслуживание"
	case "advanced":
		return "расширенное"
	case "backup":
		return "резервные копии"
	default:
		return "главное меню"
	}
}

func (m model) run(action string) model {
	if action == "back" {
		return m.setMenu("main")
	}
	if strings.HasPrefix(action, "menu:") {
		return m.setMenu(strings.TrimPrefix(action, "menu:"))
	}
	if strings.HasPrefix(action, "direct-add:") {
		kind := strings.TrimPrefix(action, "direct-add:")
		label := "Сайт, IP или подсеть"
		if kind != "auto" {
			label = "Значение " + kind
		}
		return m.startPrompt("Добавить правило прямого доступа", label, "", "direct-add:"+kind)
	}
	if strings.HasPrefix(action, "wifi-band:") {
		return m.applyWiFi("Wi-Fi диапазон", func(cfg *config.Config) error {
			band := strings.TrimPrefix(action, "wifi-band:")
			caps, err := wifi.InspectInterface(m.ctx, m.runner, cfg.MiniPC.APInterface)
			if err != nil {
				return err
			}
			if !supportsWiFiBand(caps, band) {
				return fmt.Errorf("выбранный AP-адаптер %s не поддерживает %s GHz; доступно: %s", cfg.MiniPC.APInterface, band, wifi.FormatCapabilities(caps))
			}
			return wifi.ConfigureBand(cfg, band)
		})
	}
	if strings.HasPrefix(action, "wifi-width:") {
		widthText := strings.TrimPrefix(action, "wifi-width:")
		width, _ := strconv.Atoi(widthText)
		return m.applyWiFi("Wi-Fi ширина канала", func(cfg *config.Config) error {
			cfg.WiFi.ChannelWidth = width
			return nil
		})
	}
	if strings.HasPrefix(action, "wifi-adapter:") {
		return m.prepareSwitchAdapter(strings.TrimPrefix(action, "wifi-adapter:"))
	}
	if strings.HasPrefix(action, "ru-use:") {
		name := strings.TrimPrefix(action, "ru-use:")
		err := m.ruService().Use(m.ctx, name)
		return m.withResult(fmt.Sprintf("Входной сервер переключён на %s.", name), err).setMenu("foreign")
	}
	if strings.HasPrefix(action, "ru-test:") {
		name := strings.TrimPrefix(action, "ru-test:")
		err := m.ruService().Test(m.ctx, name)
		return m.withResult("Проверка успешна.", err)
	}

	switch action {
	case "status", "diagnostics":
		status, err := diagnostics.Collect(m.ctx, m.runner, m.paths)
		return m.withResult(diagnostics.Format(status), err)
	case "tunnel":
		err := modes.EnableTunnel(m.ctx, m.runner, m.paths)
		return m.withResult("Режим маршрутизации включён.", err)
	case "direct":
		err := modes.EnableDirect(m.ctx, m.runner, m.paths)
		return m.withResult("Прямой интернет включён.", err)
	case "logs":
		logs, err := diagnostics.Logs(m.ctx, m.runner)
		return m.withResult(logs, err)
	case "diagnostic-report":
		report, err := diagnostics.Report(m.ctx, m.runner, m.paths)
		return m.withResult(report, err)
	case "info":
		text, err := summary.Generate(m.paths)
		return m.withResult(text, err)
	case "connect-info":
		return m.showConnectInfo()
	case "qr":
		return m.showQR()
	case "direct-list":
		return m.showDirectRules()
	case "direct-list-human":
		return m.showDirectRulesHuman()
	case "direct-remove":
		return m.startPrompt("Удалить правило прямого доступа", "Сайт, IP или подсеть", "", "direct-remove")
	case "ru-status":
		out, err := m.ruService().Status(m.ctx)
		return m.withResult(out, err)
	case "ru-auto":
		err := m.ruService().Auto(m.ctx)
		return m.withResult("Автоматический выбор выходного сервера включён.", err)
	case "ru-use":
		return m.setMenu("foreign-switch")
	case "ru-test":
		return m.setMenu("foreign-test")
	case "ru-logs":
		out, err := m.ruService().Logs(m.ctx)
		return m.withResult(out, err)
	case "ru-rollback":
		return m.startConfirm("Откат входного сервера", "Будет восстановлен последний backup /etc/sing-box/config.json на входном сервере.", "ru-rollback", "", nil)
	case "foreign-list":
		return m.showForeignList()
	case "foreign-add":
		return m.startForeignForm()
	case "foreign-test":
		return m.startPrompt("Проверить выходной сервер", "Имя выходного сервера", m.firstForeignName(), "foreign-test")
	case "foreign-remove":
		return m.startPrompt("Удалить выходной сервер", "Имя выходного сервера", m.firstForeignName(), "foreign-remove")
	case "foreign-cleanup":
		return m.startPrompt("Очистить VPS выходного сервера", "Имя выходного сервера", m.firstForeignName(), "foreign-cleanup")
	case "wifi-status":
		return m.showWiFiStatus()
	case "wifi-ssid":
		return m.startPrompt("Изменить название сети", "SSID", "", "wifi-ssid")
	case "wifi-password":
		return m.startPrompt("Изменить пароль Wi-Fi", "Новый пароль Wi-Fi", "", "wifi-password")
	case "wifi-scan":
		return m.showWiFiScan()
	case "wifi-channel":
		return m.startPrompt("Изменить Wi-Fi канал", "Канал", "", "wifi-channel")
	case "wifi-auto-channel":
		return m.prepareAutoChannel()
	case "wifi-restart":
		return m.startConfirm("Перезапуск Wi-Fi", "Подключённые клиенты временно потеряют соединение.", "wifi-restart", "", nil)
	case "backup-list":
		return m.showBackups()
	case "restore-network":
		return m.startConfirm("Откат сети", "Будут остановлены hostapd/dnsmasq/sing-box, удалены nftables правила router-manager и Wi-Fi будет возвращён в NetworkManager. Продолжить?", "restore-network", "", nil)
	case "update":
		return m.startConfirm("Обновить приложение", "Будет скачан последний GitHub Release, проверен checksum, сделан backup текущего бинарника и установлен новый router-manager. После обновления нужно заново открыть меню.", "update", "", nil)
	case "uninstall":
		return m.startConfirm("Полное удаление", "Будут удалены локальные настройки, данные, бинарник router-manager, локальный sing-box и прикладные зависимости. Удалённые серверы не изменяются.\n\nПосле удаления это меню больше не откроется. Продолжить?", "uninstall", "", nil)
	default:
		return m
	}
}

func (m model) runPrompt(action string, value string) model {
	if strings.HasPrefix(action, "direct-add:") {
		kind := strings.TrimPrefix(action, "direct-add:")
		if err := system.RequireRoot(); err != nil {
			return m.withResult("", err)
		}
		if _, err := system.BackupFile(m.paths.CustomDirect, m.paths.BackupsDir); err != nil {
			return m.withResult("", err)
		}
		added := ""
		var err error
		if kind == "auto" {
			_, added, err = rules.AddAuto(m.paths.CustomDirect, value)
		} else {
			added, err = rules.Add(m.paths.CustomDirect, kind, value)
		}
		if err == nil {
			err = rules.AfterChange(m.ctx, m.runner, m.paths)
		}
		return m.withResult(fmt.Sprintf("Добавлено правило прямого доступа: %s", added), err)
	}
	switch action {
	case "direct-remove":
		if err := system.RequireRoot(); err != nil {
			return m.withResult("", err)
		}
		if _, err := system.BackupFile(m.paths.CustomDirect, m.paths.BackupsDir); err != nil {
			return m.withResult("", err)
		}
		removed, err := rules.Remove(m.paths.CustomDirect, value)
		if err == nil && !removed {
			err = fmt.Errorf("правило не найдено: %s", value)
		}
		if err == nil {
			err = rules.AfterChange(m.ctx, m.runner, m.paths)
		}
		return m.withResult("Правило удалено.", err)
	case "ru-use":
		err := m.ruService().Use(m.ctx, value)
		return m.withResult(fmt.Sprintf("Входной сервер переключён на %s.", value), err)
	case "ru-test":
		err := m.ruService().Test(m.ctx, value)
		return m.withResult("Проверка успешна.", err)
	case "foreign-test":
		err := m.foreignService().Test(m.ctx, value)
		return m.withResult("Проверка успешна.", err)
	case "foreign-remove":
		return m.startConfirm("Удалить выходной сервер", "Сервер будет удалён из схемы. Если он выбран вручную, входной сервер будет переключён в автоматический режим.", "foreign-remove", value, nil)
	case "foreign-cleanup":
		return m.startConfirm("Очистить VPS выходного сервера", "На выходном сервере будет остановлен sing-box и активный config будет перенесён в backup.", "foreign-cleanup", value, nil)
	case "wifi-channel":
		channel, err := strconv.Atoi(value)
		if err != nil {
			return m.withResult("", fmt.Errorf("канал должен быть числом"))
		}
		return m.applyWiFi("Wi-Fi канал", func(cfg *config.Config) error {
			cfg.WiFi.Channel = channel
			return nil
		})
	case "wifi-ssid":
		ssid := strings.TrimSpace(value)
		return m.applyWiFi("Wi-Fi настройки", func(cfg *config.Config) error {
			if err := wifi.ValidateSSID(ssid); err != nil {
				return err
			}
			cfg.MiniPC.SSID = ssid
			return nil
		})
	case "wifi-password":
		password := strings.TrimSpace(value)
		return m.applyWiFi("Wi-Fi настройки", func(cfg *config.Config) error {
			if err := wifi.ValidatePassword(password); err != nil {
				return err
			}
			cfg.WiFi.Password = password
			return nil
		})
	default:
		return m
	}
}

func (m model) runForm(form formState) model {
	switch form.action {
	case "foreign-add":
		sshPort, err := strconv.Atoi(form.values["ssh_port"])
		if err != nil {
			return m.withResult("", fmt.Errorf("SSH port должен быть числом"))
		}
		tunnelPort, err := strconv.Atoi(form.values["tunnel_port"])
		if err != nil {
			return m.withResult("", fmt.Errorf("порт подключения должен быть числом"))
		}
		added, err := m.foreignService().Add(m.ctx, config.ForeignServer{
			Name:       form.values["name"],
			IP:         form.values["ip"],
			SSHUser:    form.values["ssh_user"],
			SSHPort:    sshPort,
			TunnelPort: tunnelPort,
		})
		return m.withResult(fmt.Sprintf("Выходной сервер добавлен: %s, SNI: %s", added.Name, added.Reality.SNI), err)
	default:
		return m
	}
}

func (m model) runConfirm(confirm confirmState) model {
	switch confirm.action {
	case "ru-rollback":
		err := m.ruService().Rollback(m.ctx)
		return m.withResult("Откат входного сервера выполнен.", err)
	case "foreign-remove":
		err := m.foreignService().Remove(m.ctx, confirm.value, true)
		return m.withResult("Выходной сервер удалён.", err)
	case "foreign-cleanup":
		err := m.foreignService().Cleanup(m.ctx, confirm.value)
		return m.withResult("Очистка выходного сервера завершена.", err)
	case "wifi-auto-channel":
		channel, _ := strconv.Atoi(confirm.params["channel"])
		width, _ := strconv.Atoi(confirm.params["width"])
		band := confirm.params["band"]
		return m.applyWiFi("Автоподбор Wi-Fi", func(cfg *config.Config) error {
			if err := wifi.ConfigureBand(cfg, band); err != nil {
				return err
			}
			cfg.WiFi.Channel = channel
			cfg.WiFi.ChannelWidth = width
			return nil
		})
	case "wifi-switch-adapter":
		if err := system.RequireRoot(); err != nil {
			return m.withResult("", err)
		}
		result, err := wifi.SwitchAccessPoint(m.ctx, m.runner, m.paths, confirm.value)
		if err != nil {
			return m.withResult("", err)
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Wi-Fi адаптер для раздачи изменён: %s\n", result.NewInterface)
		if result.OldInterface != "" && result.OldInterface != result.NewInterface {
			fmt.Fprintf(&b, "Старый адаптер возвращён в NetworkManager: %s\n", result.OldInterface)
		}
		fmt.Fprintf(&b, "Автонастройка: %s GHz, канал %d, ширина %d MHz", result.Recommendation.Band, result.Recommendation.Channel, result.Recommendation.ChannelWidth)
		return m.withResult(b.String(), nil)
	case "wifi-restart":
		if err := system.RequireRoot(); err != nil {
			return m.withResult("", err)
		}
		err := wifi.Restart(m.ctx, m.runner, m.paths)
		return m.withResult("Wi-Fi точка доступа перезапущена.", err)
	case "restore-network":
		err := (restore.Service{Paths: m.paths, Runner: m.runner}).Run(m.ctx)
		return m.withResult("Локальные сетевые изменения router-manager отключены.", err)
	case "update":
		var b strings.Builder
		err := (selfupdate.Service{Paths: m.paths, CurrentVersion: m.version, Out: &b}).Run(m.ctx)
		return m.withResult(b.String(), err)
	case "uninstall":
		var b strings.Builder
		err := (uninstall.Service{Paths: m.paths, Runner: m.runner, Out: &b, RemoveDependencies: true}).Run(m.ctx)
		return m.withResult(b.String(), err)
	default:
		return m
	}
}

func (m model) startPrompt(title string, label string, def string, action string) model {
	m.mode = modePrompt
	m.prompt = promptState{title: title, label: label, def: def, action: action}
	return m
}

func (m model) startConfirm(title string, body string, action string, value string, params map[string]string) model {
	m.mode = modeConfirm
	m.message = ""
	m.confirm = confirmState{title: title, body: body, action: action, value: value, params: params}
	return m
}

func (m model) startForeignForm() model {
	m.mode = modeForm
	m.form = formState{
		title:  "Добавить выходной сервер",
		action: "foreign-add",
		fields: []formField{
			{key: "name", label: "Имя выходного сервера", def: "out-1"},
			{key: "ip", label: "IP выходного сервера", def: ""},
			{key: "ssh_user", label: "SSH user выходного сервера", def: "root"},
			{key: "ssh_port", label: "SSH port выходного сервера", def: "22"},
			{key: "tunnel_port", label: "порт подключения выходного сервера", def: "443"},
		},
		values: map[string]string{},
	}
	return m
}

func (m model) withResult(message string, err error) model {
	if err != nil {
		m.message = ux.FriendlyError(err)
		return m
	}
	m.message = strings.TrimSpace(message)
	return m
}

func (m model) ruService() ru.Service {
	return ru.Service{Paths: m.paths, Runner: m.runner, SSH: sshclient.Client{Runner: m.runner}}
}

func (m model) foreignService() foreign.Service {
	return foreign.Service{Paths: m.paths, Runner: m.runner, SSH: sshclient.Client{Runner: m.runner}}
}

func (m model) showConnectInfo() model {
	cfg, err := config.Load(m.paths)
	if err != nil {
		return m.withResult("", err)
	}
	var b strings.Builder
	fmt.Fprintln(&b, "Подключение устройства")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Wi-Fi сеть: %s\n", valueOrNotConfigured(cfg.MiniPC.SSID))
	fmt.Fprintf(&b, "Режим сейчас: %s\n", valueOrNotConfigured(cfg.CurrentMode))
	if cfg.CurrentMode != "tunnel" {
		fmt.Fprintln(&b, "Маршрут через сервер сейчас выключен. Чтобы весь трафик Wi-Fi шёл через него, выберите: Интернет -> Включить маршрут через сервер.")
	}
	if _, err := os.Stat(m.paths.ClientLink); err == nil {
		fmt.Fprintln(&b, "QR-код клиента доступен в пункте: Показать QR-код клиента.")
	} else {
		fmt.Fprintln(&b, "QR-код клиента ещё не создан. Завершите первичную настройку.")
	}
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Если устройство подключилось к Wi-Fi, но интернет не работает, откройте: Проблемы и диагностика -> Краткая диагностика.")
	return m.withResult(b.String(), nil)
}

func (m model) showQR() model {
	data, err := os.ReadFile(m.paths.ClientLink)
	if err != nil {
		return m.withResult("", fmt.Errorf("QR-ссылка не найдена: сначала запустите sudo router-manager"))
	}
	out, err := m.runner.Output(m.ctx, "qrencode", "-t", "ANSIUTF8", strings.TrimSpace(string(data)))
	return m.withResult(out, err)
}

func (m model) showDirectRules() model {
	set, err := rules.Load(m.paths.CustomDirect)
	if err != nil {
		return m.withResult("", err)
	}
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return m.withResult("", err)
	}
	return m.withResult(string(data), nil)
}

func (m model) showDirectRulesHuman() model {
	set, err := rules.Load(m.paths.CustomDirect)
	if err != nil {
		return m.withResult("", err)
	}
	return m.withResult(rules.FormatHuman(set), nil)
}

func (m model) showForeignList() model {
	servers, err := config.LoadForeign(m.paths)
	if err != nil {
		return m.withResult("", err)
	}
	if len(servers.Servers) == 0 {
		return m.withResult("Выходные серверы не добавлены.", nil)
	}
	var b strings.Builder
	for i, server := range servers.Servers {
		fmt.Fprintf(&b, "%d) %s: %s:%d, SSH %s:%d, SNI %s\n", i+1, server.Name, server.IP, server.TunnelPort, server.SSHUser, server.SSHPort, server.Reality.SNI)
	}
	return m.withResult(b.String(), nil)
}

func (m model) foreignSwitchItems() []item {
	return m.foreignServerItems("ru-use", func(server config.ForeignServer) string {
		return fmt.Sprintf("%s -> %s:%d", server.Name, server.IP, server.TunnelPort)
	})
}

func (m model) foreignTestItems() []item {
	return m.foreignServerItems("ru-test", func(server config.ForeignServer) string {
		return fmt.Sprintf("%s SSH %s@%s:%d", server.Name, server.SSHUser, server.IP, server.SSHPort)
	})
}

func (m model) foreignServerItems(actionPrefix string, title func(config.ForeignServer) string) []item {
	servers, err := config.LoadForeign(m.paths)
	if err != nil || len(servers.Servers) == 0 {
		return []item{{"Выходные серверы не добавлены", "foreign-list"}, {"Назад", "back"}}
	}
	items := make([]item, 0, len(servers.Servers)+1)
	for _, server := range servers.Servers {
		items = append(items, item{
			title:  title(server),
			action: actionPrefix + ":" + server.Name,
		})
	}
	items = append(items, item{"Назад", "back"})
	return items
}

func (m model) wifiAdapterItems() []item {
	cfg, _ := config.Load(m.paths)
	infos, err := wifi.InterfaceInfos(m.ctx, m.runner)
	if err != nil || len(infos) == 0 {
		return []item{{"Wi-Fi адаптеры не найдены", "wifi-status"}, {"Назад", "menu:wifi"}}
	}
	items := make([]item, 0, len(infos)+1)
	for _, info := range infos {
		title := info.Name
		if info.Type != "" {
			title += " (" + info.Type + ")"
		}
		if info.Name == cfg.MiniPC.APInterface {
			title += " - текущий"
		}
		items = append(items, item{title: title, action: "wifi-adapter:" + info.Name})
	}
	items = append(items, item{"Назад", "menu:wifi"})
	return items
}

func (m model) wifiBandItems() []item {
	cfg, err := config.Load(m.paths)
	if err != nil {
		return []item{{"Не удалось прочитать Wi-Fi настройки", "wifi-status"}, {"Назад", "menu:wifi"}}
	}
	caps, err := wifi.InspectInterface(m.ctx, m.runner, cfg.MiniPC.APInterface)
	if err != nil {
		return []item{{"Диапазоны недоступны: " + err.Error(), "wifi-status"}, {"Назад", "menu:wifi"}}
	}
	bands := []struct {
		value string
		title string
		ok    bool
	}{
		{value: "2.4", title: "2.4 GHz", ok: caps.Supports24},
		{value: "5", title: "5 GHz", ok: caps.Supports5},
	}
	items := make([]item, 0, len(bands)+1)
	for _, band := range bands {
		if !band.ok {
			continue
		}
		title := band.title
		if cfg.WiFi.Band == band.value {
			title += " - текущий"
		}
		items = append(items, item{title: title, action: "wifi-band:" + band.value})
	}
	if len(items) == 0 {
		items = append(items, item{"Доступные диапазоны не найдены", "wifi-status"})
	}
	items = append(items, item{"Назад", "menu:wifi"})
	return items
}

func supportsWiFiBand(caps wifi.Capabilities, band string) bool {
	switch band {
	case "2.4":
		return caps.Supports24
	case "5":
		return caps.Supports5
	default:
		return false
	}
}

func (m model) showWiFiStatus() model {
	cfg, err := config.Load(m.paths)
	if err != nil {
		return m.withResult("", err)
	}
	text := fmt.Sprintf("SSID: %s\nДиапазон: %s GHz\nКанал: %d\nШирина: %d MHz\nWi-Fi адаптер для раздачи: %s\n",
		cfg.MiniPC.SSID, cfg.WiFi.Band, cfg.WiFi.Channel, cfg.WiFi.ChannelWidth, cfg.MiniPC.APInterface)
	return m.withResult(text, nil)
}

func (m model) showWiFiScan() model {
	cfg, err := config.Load(m.paths)
	if err != nil {
		return m.withResult("", err)
	}
	networks, err := wifi.Scan(m.ctx, m.runner, cfg.MiniPC.APInterface)
	if err != nil {
		return m.withResult("", err)
	}
	count24, count5 := 0, 0
	for _, network := range networks {
		if network.Band == "5" {
			count5++
		} else {
			count24++
		}
	}
	return m.withResult(fmt.Sprintf("Найдено сетей:\n  2.4 GHz: %d\n  5 GHz: %d", count24, count5), nil)
}

func (m model) prepareAutoChannel() model {
	if err := system.RequireRoot(); err != nil {
		return m.withResult("", err)
	}
	cfg, err := config.Load(m.paths)
	if err != nil {
		return m.withResult("", err)
	}
	caps, err := wifi.InspectInterface(m.ctx, m.runner, cfg.MiniPC.APInterface)
	if err != nil {
		return m.withResult("", err)
	}
	networks, _ := wifi.Scan(m.ctx, m.runner, cfg.MiniPC.APInterface)
	rec := wifi.Recommend(caps, networks)
	body := fmt.Sprintf("Рекомендация:\n  Диапазон: %s GHz\n  Канал: %d\n  Ширина: %d MHz", rec.Band, rec.Channel, rec.ChannelWidth)
	return m.startConfirm("Автоподбор Wi-Fi канала", body, "wifi-auto-channel", "", map[string]string{
		"band":    rec.Band,
		"channel": strconv.Itoa(rec.Channel),
		"width":   strconv.Itoa(rec.ChannelWidth),
	})
}

func (m model) prepareSwitchAdapter(iface string) model {
	if err := system.RequireRoot(); err != nil {
		return m.withResult("", err)
	}
	cfg, err := config.Load(m.paths)
	if err != nil {
		return m.withResult("", err)
	}
	caps, rec, err := wifi.RecommendedSettings(m.ctx, m.runner, iface)
	if err != nil {
		return m.withResult("", err)
	}
	body := fmt.Sprintf("Новый адаптер: %s\nТекущий адаптер: %s\n\nВозможности: %s\n\nБудут автоматически применены:\n  Диапазон: %s GHz\n  Канал: %d\n  Ширина: %d MHz\n\nПодключённые клиенты временно потеряют соединение.",
		iface, valueOrNotConfigured(cfg.MiniPC.APInterface), wifi.FormatCapabilities(caps), rec.Band, rec.Channel, rec.ChannelWidth)
	return m.startConfirm("Сменить Wi-Fi адаптер", body, "wifi-switch-adapter", iface, nil)
}

func (m model) showBackups() model {
	entries, err := os.ReadDir(m.paths.BackupsDir)
	if err != nil {
		return m.withResult("", err)
	}
	type backup struct {
		name string
		time string
	}
	var backups []backup
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backup{name: entry.Name(), time: info.ModTime().Format("2006-01-02 15:04:05")})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].time > backups[j].time })
	if len(backups) == 0 {
		return m.withResult("Локальных backups нет.", nil)
	}
	var b strings.Builder
	for _, backup := range backups {
		fmt.Fprintf(&b, "%s  %s\n", backup.time, backup.name)
	}
	return m.withResult(b.String(), nil)
}

func (m model) firstForeignName() string {
	servers, err := config.LoadForeign(m.paths)
	if err != nil || len(servers.Servers) == 0 {
		return ""
	}
	return servers.Servers[0].Name
}

func (m model) applyWiFi(success string, mutate func(*config.Config) error) model {
	if err := m.checkRoot(); err != nil {
		return m.withResult("", err)
	}
	cfg, err := config.Load(m.paths)
	if err != nil {
		return m.withResult("", err)
	}
	if err := mutate(&cfg); err != nil {
		return m.withResult("", err)
	}
	if err := config.Save(m.paths, cfg); err != nil {
		return m.withResult("", err)
	}
	if err := wifi.ApplyAccessPoint(m.ctx, m.runner, m.paths, cfg); err != nil {
		return m.withResult("", err)
	}
	return m.withResult(success+" применены.", nil)
}

func (m model) checkRoot() error {
	if m.requireRoot != nil {
		return m.requireRoot()
	}
	return system.RequireRoot()
}

func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	return string(runes[:len(runes)-1])
}

func valueOrNotConfigured(value string) string {
	if strings.TrimSpace(value) == "" {
		return "не настроено"
	}
	return value
}
