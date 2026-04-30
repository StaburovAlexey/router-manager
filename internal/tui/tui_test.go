package tui

import (
	"context"
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

func testModel() model {
	return model{
		ctx:    context.Background(),
		paths:  config.NewPaths("/tmp/vpn-router-tui-test"),
		runner: &shell.DryRunner{},
		menu:   "main",
		mode:   modeMenu,
	}
}
