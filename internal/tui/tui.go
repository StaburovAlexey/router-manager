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

	"vpn-router/internal/config"
	"vpn-router/internal/diagnostics"
	"vpn-router/internal/foreign"
	"vpn-router/internal/modes"
	"vpn-router/internal/restore"
	"vpn-router/internal/ru"
	"vpn-router/internal/rules"
	"vpn-router/internal/shell"
	"vpn-router/internal/sshclient"
	"vpn-router/internal/summary"
	"vpn-router/internal/system"
	"vpn-router/internal/wifi"
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
}

type model struct {
	ctx     context.Context
	paths   config.Paths
	runner  shell.Runner
	cursor  int
	menu    string
	mode    viewMode
	message string
	prompt  promptState
	form    formState
	confirm confirmState
}

func Run(ctx context.Context, paths config.Paths, runner shell.Runner) error {
	m := model{
		ctx:    ctx,
		paths:  paths,
		runner: runner,
		menu:   "main",
		mode:   modeMenu,
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
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.items())-1 {
			m.cursor++
		}
	case "enter":
		items := m.items()
		if len(items) == 0 {
			return m
		}
		selected := items[m.cursor]
		if selected.action == "quit" {
			m.menu = "quit"
			return m
		}
		return m.run(selected.action)
	}
	return m
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
	switch strings.ToLower(key.String()) {
	case "esc", "n":
		m.mode = modeMenu
		m.message = "Операция отменена."
		m.confirm = confirmState{}
	case "enter", "y":
		confirm := m.confirm
		m.mode = modeMenu
		m.confirm = confirmState{}
		return m.runConfirm(confirm)
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
	fmt.Fprintf(&b, "VPN Router Manager / %s\n\n", m.menuTitle())
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
	return fmt.Sprintf("VPN Router Manager / %s\n\n%s: %s\n\nEnter - применить, Esc - отмена\n", m.prompt.title, m.prompt.label, value)
}

func (m model) viewForm() string {
	var b strings.Builder
	fmt.Fprintf(&b, "VPN Router Manager / %s\n\n", m.form.title)
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
	return fmt.Sprintf("VPN Router Manager / %s\n\n%s\n\nY/Enter - подтвердить, N/Esc - отмена\n", m.confirm.title, m.confirm.body)
}

func (m model) setMenu(menu string) model {
	m.menu = menu
	m.cursor = 0
	m.mode = modeMenu
	return m
}

func (m model) items() []item {
	switch m.menu {
	case "direct-rules":
		return []item{
			{"Показать правила", "direct-list"},
			{"Добавить suffix", "direct-add:suffix"},
			{"Добавить domain", "direct-add:domain"},
			{"Добавить IP", "direct-add:ip"},
			{"Добавить CIDR", "direct-add:cidr"},
			{"Удалить правило", "direct-remove"},
			{"Назад", "back"},
		}
	case "ru":
		return []item{
			{"Статус RU", "ru-status"},
			{"Включить auto-режим", "ru-auto"},
			{"Выбрать foreign вручную", "ru-use"},
			{"Проверить foreign", "ru-test"},
			{"Показать foreign-серверы", "foreign-list"},
			{"Логи RU", "ru-logs"},
			{"Откатить RU-конфиг", "ru-rollback"},
			{"Назад", "back"},
		}
	case "foreign":
		return []item{
			{"Показать foreign-серверы", "foreign-list"},
			{"Переключить RU на foreign", "menu:foreign-switch"},
			{"Добавить foreign-сервер", "foreign-add"},
			{"Проверить SSH foreign", "foreign-test"},
			{"Удалить foreign из схемы", "foreign-remove"},
			{"Очистить foreign VPS", "foreign-cleanup"},
			{"Назад", "back"},
		}
	case "foreign-switch":
		return m.foreignSwitchItems()
	case "wifi":
		return []item{
			{"Показать Wi-Fi настройки", "wifi-status"},
			{"Сканировать соседние сети", "wifi-scan"},
			{"Установить диапазон 2.4 GHz", "wifi-band:2.4"},
			{"Установить диапазон 5 GHz", "wifi-band:5"},
			{"Изменить канал", "wifi-channel"},
			{"Ширина канала 20 MHz", "wifi-width:20"},
			{"Ширина канала 40 MHz", "wifi-width:40"},
			{"Ширина канала 80 MHz", "wifi-width:80"},
			{"Автоподбор канала", "wifi-auto-channel"},
			{"Перезапустить Wi-Fi", "wifi-restart"},
			{"Назад", "back"},
		}
	case "backup":
		return []item{
			{"Показать локальные backups", "backup-list"},
			{"Откатить RU-конфиг", "ru-rollback"},
			{"Назад", "back"},
		}
	default:
		return []item{
			{"Статус системы", "status"},
			{"Включить VPN-режим", "vpn"},
			{"Отключить VPN / включить прямой интернет", "direct"},
			{"Логи", "logs"},
			{"Информация и VLESS-ссылка", "info"},
			{"QR-код", "qr"},
			{"Правила сайтов без VPN", "menu:direct-rules"},
			{"Управление RU-сервером", "menu:ru"},
			{"Foreign-серверы", "menu:foreign"},
			{"Настройки Wi-Fi роутера", "menu:wifi"},
			{"Резервные копии и откат", "menu:backup"},
			{"Откатить локальную сеть до состояния без приложения", "restore-network"},
			{"Диагностика", "diagnostics"},
			{"Выход", "quit"},
		}
	}
}

func (m model) menuTitle() string {
	switch m.menu {
	case "direct-rules":
		return "правила без VPN"
	case "ru":
		return "RU-сервер"
	case "foreign":
		return "foreign-серверы"
	case "foreign-switch":
		return "выбор foreign"
	case "wifi":
		return "Wi-Fi"
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
		return m.startPrompt("Добавить правило", "Значение "+kind, "", "direct-add:"+kind)
	}
	if strings.HasPrefix(action, "wifi-band:") {
		return m.applyWiFi("Wi-Fi диапазон", func(cfg *config.Config) error {
			band := strings.TrimPrefix(action, "wifi-band:")
			caps, err := wifi.InspectInterface(m.ctx, m.runner, cfg.MiniPC.APInterface)
			if err != nil {
				return err
			}
			if band == "5" && !caps.Supports5 {
				return fmt.Errorf("выбранный AP-адаптер %s не поддерживает 5 GHz; доступно: %s", cfg.MiniPC.APInterface, wifi.FormatCapabilities(caps))
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
	if strings.HasPrefix(action, "ru-use:") {
		name := strings.TrimPrefix(action, "ru-use:")
		err := m.ruService().Use(m.ctx, name)
		return m.withResult(fmt.Sprintf("RU переключён на %s.", name), err).setMenu("foreign")
	}

	switch action {
	case "status", "diagnostics":
		status, err := diagnostics.Collect(m.ctx, m.runner, m.paths)
		return m.withResult(diagnostics.Format(status), err)
	case "vpn":
		err := modes.EnableVPN(m.ctx, m.runner, m.paths)
		return m.withResult("VPN-режим включён.", err)
	case "direct":
		err := modes.EnableDirect(m.ctx, m.runner, m.paths)
		return m.withResult("Прямой интернет включён.", err)
	case "logs":
		logs, err := diagnostics.Logs(m.ctx, m.runner)
		return m.withResult(logs, err)
	case "info":
		text, err := summary.Generate(m.paths)
		return m.withResult(text, err)
	case "qr":
		return m.showQR()
	case "direct-list":
		return m.showDirectRules()
	case "direct-remove":
		return m.startPrompt("Удалить правило", "Домен/IP/CIDR", "", "direct-remove")
	case "ru-status":
		out, err := m.ruService().Status(m.ctx)
		return m.withResult(out, err)
	case "ru-auto":
		err := m.ruService().Auto(m.ctx)
		return m.withResult("RU auto-режим включён.", err)
	case "ru-use":
		return m.startPrompt("Выбрать foreign", "Имя foreign-сервера", m.firstForeignName(), "ru-use")
	case "ru-test":
		return m.startPrompt("Проверить foreign", "Имя foreign-сервера", m.firstForeignName(), "ru-test")
	case "ru-logs":
		out, err := m.ruService().Logs(m.ctx)
		return m.withResult(out, err)
	case "ru-rollback":
		return m.startConfirm("Откат RU", "Будет восстановлен последний backup /etc/sing-box/config.json на RU-сервере.", "ru-rollback", "", nil)
	case "foreign-list":
		return m.showForeignList()
	case "foreign-add":
		return m.startForeignForm()
	case "foreign-test":
		return m.startPrompt("Проверить foreign", "Имя foreign-сервера", m.firstForeignName(), "foreign-test")
	case "foreign-remove":
		return m.startPrompt("Удалить foreign", "Имя foreign-сервера", m.firstForeignName(), "foreign-remove")
	case "foreign-cleanup":
		return m.startPrompt("Очистить foreign VPS", "Имя foreign-сервера", m.firstForeignName(), "foreign-cleanup")
	case "wifi-status":
		return m.showWiFiStatus()
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
		return m.startConfirm("Откат сети", "Будут остановлены hostapd/dnsmasq/sing-box, удалены nftables правила vpn-router и Wi-Fi будет возвращён в NetworkManager. Продолжить?", "restore-network", "", nil)
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
		added, err := rules.Add(m.paths.CustomDirect, kind, value)
		if err == nil {
			err = rules.AfterChange(m.ctx, m.runner, m.paths)
		}
		return m.withResult(fmt.Sprintf("Правило добавлено: %s", added), err)
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
		return m.withResult(fmt.Sprintf("RU переключён на %s.", value), err)
	case "ru-test":
		err := m.ruService().Test(m.ctx, value)
		return m.withResult("Проверка успешна.", err)
	case "foreign-test":
		err := m.foreignService().Test(m.ctx, value)
		return m.withResult("Проверка успешна.", err)
	case "foreign-remove":
		return m.startConfirm("Удалить foreign", "Сервер будет удалён из схемы. Если он выбран на RU вручную, RU будет переключён в auto.", "foreign-remove", value, nil)
	case "foreign-cleanup":
		return m.startConfirm("Очистить foreign VPS", "На foreign будет остановлен sing-box и активный config будет перенесён в backup. Продолжить?", "foreign-cleanup", value, nil)
	case "wifi-channel":
		channel, err := strconv.Atoi(value)
		if err != nil {
			return m.withResult("", fmt.Errorf("канал должен быть числом"))
		}
		return m.applyWiFi("Wi-Fi канал", func(cfg *config.Config) error {
			cfg.WiFi.Channel = channel
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
		vpnPort, err := strconv.Atoi(form.values["vpn_port"])
		if err != nil {
			return m.withResult("", fmt.Errorf("VPN port должен быть числом"))
		}
		added, err := m.foreignService().Add(m.ctx, config.ForeignServer{
			Name:    form.values["name"],
			IP:      form.values["ip"],
			SSHUser: form.values["ssh_user"],
			SSHPort: sshPort,
			VPNPort: vpnPort,
		})
		return m.withResult(fmt.Sprintf("Foreign-сервер добавлен: %s, SNI: %s", added.Name, added.Reality.SNI), err)
	default:
		return m
	}
}

func (m model) runConfirm(confirm confirmState) model {
	switch confirm.action {
	case "ru-rollback":
		err := m.ruService().Rollback(m.ctx)
		return m.withResult("Откат RU выполнен.", err)
	case "foreign-remove":
		err := m.foreignService().Remove(m.ctx, confirm.value, true)
		return m.withResult("Foreign-сервер удалён.", err)
	case "foreign-cleanup":
		err := m.foreignService().Cleanup(m.ctx, confirm.value)
		return m.withResult("Очистка foreign завершена.", err)
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
	case "wifi-restart":
		if err := system.RequireRoot(); err != nil {
			return m.withResult("", err)
		}
		err := wifi.Restart(m.ctx, m.runner, m.paths)
		return m.withResult("Wi-Fi точка доступа перезапущена.", err)
	case "restore-network":
		err := (restore.Service{Paths: m.paths, Runner: m.runner}).Run(m.ctx)
		return m.withResult("Локальные сетевые изменения vpn-router отключены.", err)
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
	m.confirm = confirmState{title: title, body: body, action: action, value: value, params: params}
	return m
}

func (m model) startForeignForm() model {
	m.mode = modeForm
	m.form = formState{
		title:  "Добавить foreign-сервер",
		action: "foreign-add",
		fields: []formField{
			{key: "name", label: "Имя", def: "de-1"},
			{key: "ip", label: "IP", def: ""},
			{key: "ssh_user", label: "SSH user", def: "root"},
			{key: "ssh_port", label: "SSH port", def: "22"},
			{key: "vpn_port", label: "VPN port", def: "443"},
		},
		values: map[string]string{},
	}
	return m
}

func (m model) withResult(message string, err error) model {
	if err != nil {
		m.message = err.Error()
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

func (m model) showQR() model {
	data, err := os.ReadFile(m.paths.ClientLink)
	if err != nil {
		return m.withResult("", fmt.Errorf("VLESS-ссылка не найдена: сначала выполните setup"))
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

func (m model) showForeignList() model {
	servers, err := config.LoadForeign(m.paths)
	if err != nil {
		return m.withResult("", err)
	}
	if len(servers.Servers) == 0 {
		return m.withResult("Foreign-серверы не добавлены.", nil)
	}
	var b strings.Builder
	for i, server := range servers.Servers {
		fmt.Fprintf(&b, "%d) %s: %s:%d, SSH %s:%d, SNI %s\n", i+1, server.Name, server.IP, server.VPNPort, server.SSHUser, server.SSHPort, server.Reality.SNI)
	}
	return m.withResult(b.String(), nil)
}

func (m model) foreignSwitchItems() []item {
	servers, err := config.LoadForeign(m.paths)
	if err != nil || len(servers.Servers) == 0 {
		return []item{{"Foreign-серверы не добавлены", "foreign-list"}, {"Назад", "back"}}
	}
	items := make([]item, 0, len(servers.Servers)+1)
	for _, server := range servers.Servers {
		items = append(items, item{
			title:  fmt.Sprintf("%s -> %s:%d", server.Name, server.IP, server.VPNPort),
			action: "ru-use:" + server.Name,
		})
	}
	items = append(items, item{"Назад", "back"})
	return items
}

func (m model) showWiFiStatus() model {
	cfg, err := config.Load(m.paths)
	if err != nil {
		return m.withResult("", err)
	}
	text := fmt.Sprintf("SSID: %s\nДиапазон: %s GHz\nКанал: %d\nШирина: %d MHz\nAP interface: %s\n",
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
	if err := system.RequireRoot(); err != nil {
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

func trimLastRune(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	return string(runes[:len(runes)-1])
}
