package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"vpn-router/internal/config"
	"vpn-router/internal/shell"
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

func testModel() model {
	return testModelWithPaths(config.NewPaths("/tmp/vpn-router-tui-test"))
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
