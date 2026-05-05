package tui

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func TestTUIForeignMenuHasExecutableActions(t *testing.T) {
	m := testModel().run("menu:foreign")
	if m.menu != "foreign" {
		t.Fatalf("menu = %q", m.menu)
	}
	actions := map[string]bool{}
	for _, item := range m.items() {
		actions[item.action] = true
	}
	for _, want := range []string{"foreign-list", "foreign-add", "foreign-test", "foreign-remove", "foreign-cleanup"} {
		if !actions[want] {
			t.Fatalf("foreign menu missing action %q", want)
		}
	}
}

func TestTUIForeignAddStartsForm(t *testing.T) {
	m := testModel().run("foreign-add")
	if m.mode != modeForm {
		t.Fatalf("mode = %q", m.mode)
	}
	if m.form.action != "foreign-add" || len(m.form.fields) != 5 {
		t.Fatalf("unexpected form: %#v", m.form)
	}
}

func TestTUIRUServerUseStartsForeignServerList(t *testing.T) {
	paths := testForeignServerPaths(t)
	m := testModelWithPaths(paths).run("menu:ru")
	m = m.run(menuAction(t, m.items(), "Выбрать выходной сервер вручную"))
	if m.mode != modeMenu || m.menu != "foreign-switch" {
		t.Fatalf("unexpected state: mode=%q menu=%q", m.mode, m.menu)
	}
}

func TestTUIRUServerTestStartsForeignServerList(t *testing.T) {
	paths := testForeignServerPaths(t)
	m := testModelWithPaths(paths).run("menu:ru")
	m = m.run(menuAction(t, m.items(), "Проверить выходной сервер"))
	if m.mode != modeMenu || m.menu != "foreign-test" {
		t.Fatalf("unexpected state: mode=%q menu=%q", m.mode, m.menu)
	}
}

func TestTUIForeignSwitchItemsUseServerActions(t *testing.T) {
	paths := testForeignServerPaths(t)
	items := testModelWithPaths(paths).setMenu("foreign-switch").items()
	assertMenuItem(t, items, "de-1 -> 203.0.113.10:8443", "ru-use:de-1")
	assertMenuItem(t, items, "nl-1 -> 203.0.113.20:9443", "ru-use:nl-1")
}

func TestTUIForeignTestItemsUseServerActions(t *testing.T) {
	paths := testForeignServerPaths(t)
	items := testModelWithPaths(paths).setMenu("foreign-test").items()
	assertMenuItem(t, items, "de-1 SSH root@203.0.113.10:22", "ru-test:de-1")
	assertMenuItem(t, items, "nl-1 SSH admin@203.0.113.20:2222", "ru-test:nl-1")
}

func TestTUIForeignServerItemsShowsEmptyState(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	items := testModelWithPaths(paths).setMenu("foreign-test").items()
	assertMenuItem(t, items, "Выходные серверы не добавлены", "foreign-list")
	assertMenuItem(t, items, "Назад", "back")
}

func TestTUIInternetMenuShowsSingleRouteSwitch(t *testing.T) {
	items := testModel().setMenu("internet").items()

	assertMenuItem(t, items, "Пропускать весь трафик через VPN", "route:vpn")
	assertMenuItem(t, items, "Отключить пропуск всего трафика через VPN", "route:direct")
	assertMenuItem(t, items, "GeoIP Россия", "menu:geoip")
	assertNoMenuTitle(t, items, "Весь трафик через VPN")
	assertNoMenuTitle(t, items, "Прямой интернет, выбранные сайты через VPN")
}

func TestTUIRoutingRulesMenuSeparatesDirectAndProxyLists(t *testing.T) {
	mainItems := testModel().items()
	assertMenuItem(t, mainItems, "Правила маршрутизации", "menu:route-rules")

	items := testModel().setMenu("route-rules").items()
	assertMenuItem(t, items, "Сайты и IP напрямую", "menu:direct-rules")
	assertMenuItem(t, items, "Сайты и IP через VPN", "menu:proxy-rules")
	assertMenuItem(t, items, "Автоправила России", "menu:geoip")

	proxyItems := testModel().setMenu("proxy-rules").items()
	assertMenuItem(t, proxyItems, "Добавить сайт или IP через VPN", "proxy-add:auto")
	assertMenuItem(t, proxyItems, "Показать сайты через VPN", "proxy-list-human")
}

func TestTUIWiFiMenuHasSSIDAndPasswordActions(t *testing.T) {
	m := testModel().run("menu:wifi")
	actions := map[string]bool{}
	for _, item := range m.items() {
		actions[item.action] = true
	}
	for _, want := range []string{"wifi-ssid", "wifi-password"} {
		if !actions[want] {
			t.Fatalf("wifi menu missing action %q", want)
		}
	}
}

func TestTUIWiFiMenuUsesSingleBandPicker(t *testing.T) {
	items := testModel().run("menu:wifi").items()

	assertMenuItem(t, items, "Выбрать диапазон", "menu:wifi-band")
	assertNoMenuTitle(t, items, "Установить диапазон 2.4 GHz")
	assertNoMenuTitle(t, items, "Установить диапазон 5 GHz")
}

func TestTUIWiFiBandMenuShowsOnlySupportedBands(t *testing.T) {
	paths := testWiFiPaths(t)
	m := testModelWithPaths(paths)
	m.runner = testWiFi24OnlyRunner()

	items := m.setMenu("wifi-band").items()

	assertMenuItem(t, items, "2.4 GHz", "wifi-band:2.4")
	assertNoMenuTitle(t, items, "5 GHz")
	assertNoMenuTitle(t, items, "5 GHz - текущий")
	assertMenuItem(t, items, "Назад", "menu:wifi")
}

func TestTUIWiFiSSIDStartsPrompt(t *testing.T) {
	m := testModel().run("wifi-ssid")
	if m.mode != modePrompt || m.prompt.action != "wifi-ssid" {
		t.Fatalf("unexpected prompt state: %#v", m.prompt)
	}
}

func TestTUIQuitReturnsCommand(t *testing.T) {
	m := testModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
}

func TestTUINumberSelectsMenuItem(t *testing.T) {
	m := testModel()
	next := m.updateMenu(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if next.menu != "internet" {
		t.Fatalf("menu = %q", next.menu)
	}
}

func TestTUIZeroGoesBackOrQuits(t *testing.T) {
	m := testModel().setMenu("wifi")
	next := m.updateMenu(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	if next.menu != "main" {
		t.Fatalf("menu = %q", next.menu)
	}
	next = next.updateMenu(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("0")})
	if next.menu != "quit" {
		t.Fatalf("menu = %q", next.menu)
	}
}

func TestTUIConfirmRequiresExplicitYesOrNo(t *testing.T) {
	m := testModel().startConfirm("Проверка", "Опасное действие.", "foreign-remove", "de-1", nil)
	m = m.updateConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeConfirm {
		t.Fatalf("empty enter must keep confirm mode, got %q", m.mode)
	}
	if !strings.Contains(m.message, "да/нет") {
		t.Fatalf("unexpected message: %q", m.message)
	}
}

func TestTUIConfirmNoCancelsInRussian(t *testing.T) {
	m := typeConfirmInput(testModel().startConfirm("Проверка", "Опасное действие.", "foreign-remove", "de-1", nil), "нет")
	m = m.updateConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenu {
		t.Fatalf("mode = %q", m.mode)
	}
	if !strings.Contains(m.message, "отменена") {
		t.Fatalf("unexpected message: %q", m.message)
	}
}

func TestTUIConfirmYesRunsActionInRussian(t *testing.T) {
	paths := config.NewPaths(t.TempDir())
	if err := config.SaveForeign(paths, config.ForeignServers{Servers: []config.ForeignServer{{Name: "de-1"}}}); err != nil {
		t.Fatal(err)
	}
	m := typeConfirmInput(testModelWithPaths(paths).startConfirm("Удалить", "Удалить сервер.", "foreign-remove", "de-1", nil), "да")
	m = m.updateConfirm(tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeMenu {
		t.Fatalf("mode = %q", m.mode)
	}
	if !strings.Contains(m.message, "удалён") {
		t.Fatalf("unexpected message: %q", m.message)
	}
	servers, err := config.LoadForeign(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(servers.Servers) != 0 {
		t.Fatalf("server was not removed: %#v", servers.Servers)
	}
}

func TestTUIWiFiSSIDPromptUpdatesConfig(t *testing.T) {
	paths := testWiFiPaths(t)
	m := testModelWithPaths(paths)
	m.runner = testWiFiRunner()
	m.requireRoot = func() error { return nil }

	m = m.runPrompt("wifi-ssid", "NewAP")

	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MiniPC.SSID != "NewAP" {
		t.Fatalf("SSID = %q, want NewAP", cfg.MiniPC.SSID)
	}
	if !strings.Contains(m.message, "Wi-Fi настройки применены") {
		t.Fatalf("unexpected message: %q", m.message)
	}
}

func TestTUIWiFiPasswordPromptUpdatesConfig(t *testing.T) {
	paths := testWiFiPaths(t)
	m := testModelWithPaths(paths)
	m.runner = testWiFiRunner()
	m.requireRoot = func() error { return nil }

	m = m.runPrompt("wifi-password", "newpassword")

	cfg, err := config.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WiFi.Password != "newpassword" {
		t.Fatalf("password = %q, want newpassword", cfg.WiFi.Password)
	}
	if strings.Contains(m.message, "newpassword") {
		t.Fatalf("password leaked to message: %q", m.message)
	}
}

func testModel() model {
	return testModelWithPaths(config.NewPaths("/tmp/router-manager-tui-test"))
}

func testModelWithPaths(paths config.Paths) model {
	return model{
		ctx:    context.Background(),
		paths:  paths,
		runner: &shell.DryRunner{},
		menu:   "main",
		mode:   modeMenu,
	}
}

func typeConfirmInput(m model, value string) model {
	for _, r := range value {
		m = m.updateConfirm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func testForeignServerPaths(t *testing.T) config.Paths {
	t.Helper()
	paths := config.NewPaths(t.TempDir())
	if err := config.SaveForeign(paths, config.ForeignServers{Servers: []config.ForeignServer{
		{Name: "de-1", IP: "203.0.113.10", SSHUser: "root", SSHPort: 22, TunnelPort: 8443},
		{Name: "nl-1", IP: "203.0.113.20", SSHUser: "admin", SSHPort: 2222, TunnelPort: 9443},
	}}); err != nil {
		t.Fatal(err)
	}
	return paths
}

func assertMenuItem(t *testing.T, items []item, title string, action string) {
	t.Helper()
	for _, item := range items {
		if item.title == title && item.action == action {
			return
		}
	}
	t.Fatalf("menu item %q with action %q not found in %#v", title, action, items)
}

func assertNoMenuTitle(t *testing.T, items []item, title string) {
	t.Helper()
	for _, item := range items {
		if item.title == title {
			t.Fatalf("unexpected menu item %q in %#v", title, items)
		}
	}
}

func menuAction(t *testing.T, items []item, title string) string {
	t.Helper()
	for _, item := range items {
		if item.title == title {
			return item.action
		}
	}
	t.Fatalf("menu item %q not found in %#v", title, items)
	return ""
}

func testWiFiPaths(t *testing.T) config.Paths {
	t.Helper()
	dir := t.TempDir()
	paths := config.NewPaths(dir)
	paths.HostapdConf = filepath.Join(dir, "hostapd.conf")
	paths.DnsmasqConf = filepath.Join(dir, "dnsmasq.conf")
	paths.NftablesMainConf = filepath.Join(dir, "nftables.conf")
	paths.NftablesConf = filepath.Join(dir, "router-manager.nft")
	paths.SingBoxLocalConf = filepath.Join(dir, "sing-box.json")
	cfg := config.DefaultConfig()
	cfg.MiniPC.APInterface = "wlan1"
	cfg.MiniPC.SSID = "OldAP"
	cfg.WiFi.Password = "oldpassword"
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths
}

func testWiFiRunner() *shell.DryRunner {
	return &shell.DryRunner{Outputs: map[string]string{
		"iw dev": `
phy#1
	Interface wlan1
		type managed
`,
		"iw list": `
Wiphy phy1
	Supported interface modes:
		 * managed
		 * AP
	Band 1:
		Capabilities: 0x19ef
			HT20/HT40
		Frequencies:
			* 2412 MHz [1] (20.0 dBm)
			* 2437 MHz [6] (20.0 dBm)
			* 2462 MHz [11] (20.0 dBm)
	Band 2:
		VHT Capabilities (0x0)
		Frequencies:
			* 5180 MHz [36] (20.0 dBm)
`,
		"systemctl is-active hostapd": "active",
		"systemctl is-active dnsmasq": "active",
	}}
}

func testWiFi24OnlyRunner() *shell.DryRunner {
	return &shell.DryRunner{Outputs: map[string]string{
		"iw dev": `
phy#1
	Interface wlan1
		type managed
`,
		"iw list": `
Wiphy phy1
	Supported interface modes:
		 * managed
		 * AP
	Band 1:
		Capabilities: 0x19ef
			HT20/HT40
		Frequencies:
			* 2412 MHz [1] (20.0 dBm)
			* 2437 MHz [6] (20.0 dBm)
			* 2462 MHz [11] (20.0 dBm)
`,
		"systemctl is-active hostapd": "active",
		"systemctl is-active dnsmasq": "active",
	}}
}
