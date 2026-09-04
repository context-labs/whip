package tui

import (
	"strings"
	"testing"
)

// The terminal cursor sits on the textarea caret inside the input rectangle,
// follows typing and wrapping, keeps the user's cursor colour, and hides
// while a dialog, the rewind picker or a masked prompt owns the keyboard.
func TestRealCursorInsideInputRect(t *testing.T) {
	m := goldenModel(140, 40)
	view := m.View()
	it := m.frameNow().inputText
	if view.Cursor == nil || view.Cursor.X != it.Min.X || view.Cursor.Y != it.Min.Y {
		t.Fatalf("empty input: cursor %+v, want top-left of %v", view.Cursor, it)
	}
	if view.Cursor.Color != nil {
		t.Fatalf("cursor colour must stay the terminal's own, got %v", view.Cursor.Color)
	}
	m.input.SetValue("hello")
	m.input.CursorEnd()
	if c := m.View().Cursor; c == nil || c.X != it.Min.X+5 || c.Y != it.Min.Y {
		t.Fatalf("after typing: cursor %+v", c)
	}
	m.input.SetValue(strings.Repeat("word ", 40)) // wraps onto several rows
	m.input.CursorEnd()
	m.layout()
	view = m.View()
	it = m.frameNow().inputText
	if view.Cursor == nil || view.Cursor.Y <= it.Min.Y || view.Cursor.Y >= it.Max.Y {
		t.Fatalf("wrapped input: cursor %+v outside rows of %v", view.Cursor, it)
	}
	m.input.SetValue("")
	for name, open := range map[string]func(){
		"palette":   func() { m.openThinThemePalette() },
		"rewind":    func() { m.rew = &rewindState{entries: []rewindEntry{{cut: 0}}} },
		"mask":      func() { m.openNamePrompt("key:", "", func(string) {}); m.namePrompt.mask = true },
		"msgAction": func() { m.msgActions = &msgActions{block: 0} },
	} {
		m.palette, m.rew, m.namePrompt, m.msgActions = nil, nil, nil, nil
		open()
		m.layout()
		if c := m.View().Cursor; c != nil {
			t.Fatalf("%s open: cursor should hide, got %+v", name, c)
		}
	}
	m.palette, m.rew, m.namePrompt, m.msgActions = nil, nil, nil, nil
	m.openNamePrompt("name:", "", func(string) {})
	m.layout()
	it = m.frameNow().inputText
	if c := m.View().Cursor; c == nil || c.X != it.Min.X || c.Y != it.Min.Y {
		t.Fatalf("name prompt: cursor %+v, want %v origin", c, it)
	}
	SetLightTheme(true)
	defer SetLightTheme(false)
	m.applyOpencodeStyles()
	if c := m.View().Cursor; c == nil || c.Color != nil {
		t.Fatalf("light theme: cursor %+v", c)
	}
}
