package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A macOS screenshot preview pastes its temporary file path into the
// terminal. Treat that path as an attachment instead of a slash command.
func TestPastedScreenshotPathAttachesImage(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())

	source := filepath.Join(t.TempDir(), "Screenshot") // preview paths need not have an extension
	image := []byte("\x89PNG\r\n\x1a\nimage-data")
	if err := os.WriteFile(source, image, 0o600); err != nil {
		t.Fatal(err)
	}

	m := compactCmdModel()
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(source + "\n"), Paste: true})
	m = tm.(*model)
	if cmd == nil {
		t.Fatal("pasted screenshot path should schedule an attachment")
	}

	tm, _ = m.Update(cmd())
	m = tm.(*model)
	got := strings.TrimSpace(strings.TrimPrefix(m.input.Value(), "@"))
	if filepath.Ext(got) != ".png" {
		t.Fatalf("attachment extension: %q", got)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(image) {
		t.Fatalf("saved image = %q, want %q", data, image)
	}
}

func TestSaveClipboardImageStaysWithinPasteDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	if _, err := saveClipboardImage("../../escaped", []byte("image")); err == nil {
		t.Fatal("path-traversing extension should be rejected")
	}
	if _, err := os.Stat(filepath.Join(home, "escaped")); !os.IsNotExist(err) {
		t.Fatalf("write escaped the paste directory: %v", err)
	}
}

// Paste collapse is opt-in (config collapsePaste): off by default a paste
// lands verbatim; on, a ≥3-line paste becomes a placeholder whose real text
// swaps back in at submit.
func TestPasteCollapseOptIn(t *testing.T) {
	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("line1\nline2\nline3"), Paste: true}

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
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("just one line"), Paste: true})
	if strings.Contains(m.input.Value(), "[Pasted") {
		t.Fatal("a one-line paste should not collapse")
	}
}
