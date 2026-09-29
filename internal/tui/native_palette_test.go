package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativePaletteRoutesMountedCommandsWithoutLosingDraft(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.preferencesDirectory = t.TempDir()
	m.preferences.CollapsePaste = new(true)
	m.pasteText("draft\nkept\noriginal")
	m.addImage(protocol.ContentReference{ID: "draft-image", SessionID: m.owner.ID}, "draft.png")
	draft := m.input.Value()
	m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if m.palette == nil {
		t.Fatal("Ctrl+P did not mount palette")
	}
	m.Update(tea.PasteMsg{Content: "model-for-session"})
	if len(m.palette.items) != 1 || m.input.Value() != draft {
		t.Fatal("palette search changed draft")
	}
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil || m.menu == nil || m.menu.options.Kind != "model-for-session" || m.palette != nil || m.input.Value() != draft || len(m.images) != 1 || len(m.pastes) != 1 {
		t.Fatal("palette bypassed native menu or lost draft", m.status)
	}
	m.Update(command())
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.menu != nil || m.input.Value() != draft {
		t.Fatal("menu close lost original draft")
	}
	m.openNativePalette()
	m.palette.query = "/fork-at"
	m.palette.filter()
	m.paletteKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != draft || !strings.Contains(m.status, "Draft kept") {
		t.Fatal("argument command overwrote draft", m.status)
	}
	m.input.Reset()
	m.openNativePalette()
	m.palette.query = "/fork-at"
	m.palette.filter()
	m.paletteKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != "/fork-at " || m.controlling || m.sending {
		t.Fatal("argument command executed prematurely", m.status)
	}
}

func TestNativePaletteBoundsAndShortcutScopes(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.preferencesDirectory = t.TempDir()
	m.input.SetValue("unsent prompt")
	m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if !m.showReasoning || m.input.Value() != "unsent prompt" {
		t.Fatal("reasoning shortcut changed draft")
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if m.controlling || !strings.Contains(m.status, "child") {
		t.Fatal("stop shortcut admitted root stop", m.status)
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if nativePreferenceLabel(m.preferences.Sidebar, true) != "off" || m.input.Value() != "unsent prompt" {
		t.Fatal("leader sidebar did not use native settings")
	}
	m.Update(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	m.leaderAt = time.Now().Add(-3 * time.Second)
	m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	if m.input.Value() != "unsent promptb" || nativePreferenceLabel(m.preferences.Sidebar, true) != "off" {
		t.Fatal("expired chord performed a command", m.input.Value())
	}
	m.closeCompletion(false)
	m.openNativePalette()
	for _, size := range [][2]int{{8, 4}, {80, 24}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		rows := strings.Split(m.View().Content, "\n")
		if len(rows) > size[1] {
			t.Fatal("palette exceeded height")
		}
		for _, row := range rows {
			if ansi.StringWidth(row) > size[0] {
				t.Fatal("palette exceeded width")
			}
		}
	}
	m.Update(tea.PasteMsg{Content: strings.Repeat("x", 513)})
	if m.palette.query != "" {
		t.Fatal("palette accepted unbounded search")
	}
	m.Update(tea.PasteMsg{Content: "é"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.palette.query != "" {
		t.Fatal("palette backspace split UTF-8")
	}
}
