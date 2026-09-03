package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fullModel builds a model whose transcript FILLS the viewport (the resumed-
// session shape), so the rendered view is at its maximum height.
func fullModel() *model {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	for i := range 60 {
		m.append(strings.Repeat("x", 10) + "-" + string(rune('a'+i%26)))
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")}) // settle layout
	m = tm.(*model)
	m.input.SetValue("")
	return m
}

// The rendered view must NEVER be taller than the terminal. If layout()'s
// chrome budget undercounts viewBody's rows, a full transcript overflows the
// screen: every frame scrolls the header off the top and ALL mouse math lands
// that many rows above the pointer. (The off-by-two drag-select bug: the
// status line and its blank line above weren't budgeted, so the full view was
// height+2.) A full transcript must render EXACTLY the terminal height.
func TestViewNeverTallerThanTerminal(t *testing.T) {
	m := fullModel()
	if got := lipgloss.Height(m.View()); got != m.height {
		t.Fatalf("full view renders %d rows on a %d-row terminal", got, m.height)
	}
	// header and tips must therefore still be the top rows on screen
	lines := strings.Split(m.View(), "\n")
	if !strings.Contains(lines[0], "whip") || !strings.Contains(lines[1], "ctrl+p") {
		t.Fatalf("header/tips scrolled off: top rows %q / %q", lines[0], lines[1])
	}

	// the invariant holds across the chrome-changing states too (layout()
	// re-budgets after every Update in production; run it like Update does)
	check := func(name string, mut func()) {
		t.Helper()
		mut()
		m.layout()
		if got := lipgloss.Height(m.View()); got != m.height {
			t.Fatalf("%s view renders %d rows on a %d-row terminal", name, got, m.height)
		}
	}
	check("busy", func() { m.busy = true })
	check("quit-hint", func() { m.busy, m.quit1 = false, true })
	check("esc-hint", func() { m.quit1, m.escClr = false, true })
}

// Drag-select on a FULL transcript (the --resume shape: viewport scrolled,
// YOffset > 0, no top padding) must copy the exact row under the pointer.
func TestDragSelectOnFullTranscript(t *testing.T) {
	m := fullModel()
	m.appendAssistantBlock("FULL-MARKER")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = tm.(*model)
	m.input.SetValue("")
	if m.contentPad() != 0 || m.vp.YOffset == 0 {
		t.Fatalf("test setup: expected a scrolled full viewport (pad=%d yoff=%d)", m.contentPad(), m.vp.YOffset)
	}

	last := len(m.blocks) - 1
	y := blockRowY(m, m.blocks[last].y0)
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: y})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 79, Y: y})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 79, Y: y})
	if got := m.selText(*m.sel); !strings.Contains(got, "FULL-MARKER") {
		t.Fatalf("drag on the marker row copied %q, want FULL-MARKER", got)
	}
}
