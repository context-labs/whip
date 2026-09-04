package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// viewportPlain returns the visible transcript as plain text lines.
func (m *model) viewportPlain() []string {
	return strings.Split(ansi.Strip(m.vp.View()), "\n")
}

// End-to-end proof: a long bash command on a narrow terminal renders fully
// in the actual viewport — wrapped, never truncated — and stays fully
// visible across a resize.
func TestToolCallFullyVisibleInViewportAcrossResize(t *testing.T) {
	m := compactCmdModel()
	tm, _ := m.Update(mkWinSize(46, 30))
	m = tm.(*model)

	cmd := `{"command":"cd /home/user/project && git log --oneline --graph --decorate=full --all | head -50"}`
	tm, _ = m.Update(toolStartMsg{name: "bash", args: cmd})
	m = tm.(*model)

	assertFullyVisible := func(width int) {
		t.Helper()
		lines := m.viewportPlain()
		joined := strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
		if strings.Contains(joined, "…") {
			t.Fatalf("viewport at %d cols contains an ellipsis:\n%s", width, strings.Join(lines, "\n"))
		}
		// the whole command survives wrapping: whitespace at break points is
		// eaten, so compare with all whitespace removed — nothing may be lost
		compact := strings.Map(func(r rune) rune {
			if r == ' ' || r == '\t' || r == '\n' {
				return -1
			}
			return r
		}, joined)
		want := strings.ReplaceAll(cmd, " ", "")
		if !strings.Contains(compact, want) {
			t.Errorf("viewport at %d cols lost command bytes:\nwant fragment of %q\ngot %q", width, want, compact)
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > width {
				t.Errorf("viewport line at %d cols is %d wide: %q", width, w, l)
			}
		}
	}

	assertFullyVisible(46)

	// shrink and grow: content must reflow, still fully visible
	tm, _ = m.Update(mkWinSize(30, 30))
	m = tm.(*model)
	assertFullyVisible(30)

	tm, _ = m.Update(mkWinSize(120, 30))
	m = tm.(*model)
	assertFullyVisible(120)
}

// End-to-end: the tool *result* block collapses to a preview, expands fully
// with ctrl+e (no ellipsis in the viewport), and re-wraps on resize.
func TestToolResultFullyVisibleWhenExpanded(t *testing.T) {
	m := compactCmdModel()
	tm, _ := m.Update(mkWinSize(46, 40))
	m = tm.(*model)

	var sb strings.Builder
	for i := 1; i <= 12; i++ {
		sb.WriteString("output row " + strings.Repeat("x", i) + "\n")
	}
	tm, _ = m.Update(toolEndMsg{name: "bash", result: sb.String()})
	m = tm.(*model)

	// collapsed: preview + a hint (an ellipsis is fine HERE — it's the
	// collapse affordance, and it must say how to expand)
	joined := strings.Join(m.viewportPlain(), "\n")
	if !strings.Contains(joined, "↳ 12 lines") {
		t.Fatalf("collapsed view should announce hidden lines, got:\n%s", joined)
	}
	if strings.Contains(joined, "output row "+strings.Repeat("x", 12)) {
		t.Fatal("collapsed view must not show the last row")
	}

	// expand: every row visible, no truncation
	tm, _ = m.key(keyMsg(tea.KeyCtrlE))
	m = tm.(*model)
	joined = strings.Join(m.viewportPlain(), "\n")
	for i := 1; i <= 12; i++ {
		row := "output row " + strings.Repeat("x", i)
		if !strings.Contains(joined, row) {
			t.Errorf("expanded viewport missing %q", row)
		}
	}
	if strings.Contains(joined, "… +") {
		t.Errorf("expanded view still hides lines:\n%s", joined)
	}

	// resize narrower: expanded content re-wraps, nothing overflows
	tm, _ = m.Update(mkWinSize(28, 40))
	m = tm.(*model)
	for _, l := range m.viewportPlain() {
		if w := ansi.StringWidth(l); w > 28 {
			t.Errorf("expanded line exceeds 28 cols (%d): %q", w, l)
		}
	}
}
