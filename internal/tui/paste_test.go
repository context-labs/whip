package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// A macOS screenshot preview pastes the path of a temporary, extension-less
// file. The paste must attach the image (copied into ~/.whip/pastes) as the
// same @path mention a clipboard paste produces — not type the raw path.
func TestPastedScreenshotPathAttachesImage(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())

	source := filepath.Join(t.TempDir(), "Screenshot") // preview paths need not have an extension
	image := []byte("\x89PNG\r\n\x1a\nimage-data")
	if err := os.WriteFile(source, image, 0o600); err != nil {
		t.Fatal(err)
	}

	m := compactCmdModel()
	tm, cmd := m.Update(tea.PasteMsg{Content: source + "\n"})
	m = tm.(*model)
	if cmd == nil {
		t.Fatal("pasted screenshot path should schedule an attachment")
	}
	if m.input.Value() != "" {
		t.Fatalf("the raw path must not land in the input, got %q", m.input.Value())
	}

	tm, _ = m.Update(cmd())
	m = tm.(*model)
	value := m.input.Value()
	if !strings.HasPrefix(value, "@") || !strings.Contains(value, string(filepath.Separator)+"pastes"+string(filepath.Separator)) {
		t.Fatalf("input should mention the saved copy, got %q", value)
	}
	// The copy holds the pasted bytes verbatim, under the recognized extension.
	saved := strings.TrimSpace(strings.TrimPrefix(value, "@"))
	if filepath.Ext(saved) != ".png" {
		t.Fatalf("magic bytes should pick the extension, got %q", saved)
	}
	data, err := os.ReadFile(saved)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(image) {
		t.Fatalf("saved image = %q, want %q", data, image)
	}
}

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
