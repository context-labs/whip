package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A Finder drag delivers the path as one KeyRunes burst (msg.Paste is false).
// It must be detected as an image and routed to the chip flow.
func TestFinderDragImagePathDetected(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "Screenshot 2026-09-04 at 3.21.45 PM.png")
	if err := os.WriteFile(img, []byte("\x89PNG\r\n\x1a\nfake"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Simulate the drag: a single multi-rune KeyRunes with the raw path.
	m := tasksModel("http://unused")
	t.Setenv("WHIP_HOME", t.TempDir())
	tm, cmd := m.Update(dragRunes(img))
	m = tm.(*model)
	if cmd == nil {
		t.Fatal("a dragged image path should schedule an attachment command")
	}
	tm, _ = m.Update(cmd())
	m = tm.(*model)
	if len(m.images) != 1 {
		t.Fatalf("Finder drag produced %d images, want 1", len(m.images))
	}
	if !strings.Contains(m.input.Value(), "[Image 1: Screenshot") || !strings.Contains(m.input.Value(), ".png]") {
		t.Errorf("input = %q, want the [Image 1: <name>…png] chip", m.input.Value())
	}
}

// A single-rune KeyRunes (normal typing) must NOT be treated as a drag.
func TestSingleRuneKeyNotADrag(t *testing.T) {
	m := tasksModel("http://unused")
	um, _ := m.Update(dragRunes("a"))
	m = um.(*model)
	if len(m.images) != 0 {
		t.Fatalf("single rune produced %d images, want 0", len(m.images))
	}
}

// dragRunes builds a KeyRunes message the way a Finder drag does: Paste=false,
// the whole path as one burst.
func dragRunes(path string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(path), Paste: false}
}
