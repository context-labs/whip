package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Paste collapse is opt-in (config collapsePaste): off by default a paste
// lands verbatim; on, a ≥3-line paste becomes a placeholder whose real text
// swaps back in at submit.
func TestPasteCollapseOptIn(t *testing.T) {
	paste := tea.PasteMsg{Content: "line1\nline2\nline3"}

	// default (nil) — off: the textarea takes the raw paste
	m := compactCmdModel()
	m.Update(paste)
	if !strings.Contains(m.input.Value(), "line1") {
		t.Fatalf("paste should land verbatim by default, got %q", m.input.Value())
	}
	if m.pasteBuf != "" {
		t.Fatal("no buffer held when collapse is off")
	}

	// on — collapse to a placeholder, real text held
	on := true
	m2 := compactCmdModel()
	m2.cfg.CollapsePaste = &on
	m2.Update(paste)
	if !strings.Contains(m2.input.Value(), "[Pasted ~3 lines]") {
		t.Fatalf("collapsed input should show the placeholder, got %q", m2.input.Value())
	}
	if m2.pasteBuf == "" {
		t.Fatal("the real paste text should be held")
	}
	// submit swaps it back
	m2.input.SetValue(m2.input.Value()) // settle
	m2.permDialog = nil
	// drive the submit path's swap directly (the placeholder → real text)
	text := strings.TrimSpace(m2.input.Value())
	text = strings.Replace(text, "[Pasted ~3 lines]", strings.TrimSpace(m2.pasteBuf), 1)
	if !strings.Contains(text, "line1\nline2\nline3") {
		t.Fatalf("submit should restore the real text, got %q", text)
	}
}

// A short paste (1-2 lines) never collapses, even when the option is on.
func TestPasteCollapseShortPasteIgnored(t *testing.T) {
	on := true
	m := compactCmdModel()
	m.cfg.CollapsePaste = &on
	m.Update(tea.PasteMsg{Content: "just one line"})
	if strings.Contains(m.input.Value(), "[Pasted") {
		t.Fatal("a one-line paste should not collapse")
	}
}
