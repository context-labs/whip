package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Drag on the exact screen row of a block must copy that block's text.
func TestSelectionRowAccuracy(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.append("hello world")                 // block 0
	m.appendAssistantBlock("MARKER-ANSWER") // block 1
	tm, _ := m.Update(keyRunes(" "))        // settle
	m = tm.(*model)
	m.input.SetValue("")

	// find MARKER on the rendered screen: its row AND its start column (the
	// assistant "● " prefix width depends on render state, so don't hardcode it)
	v := viewStr(m)
	screenRow, screenCol := -1, -1
	for i, l := range strings.Split(v, "\n") {
		if j := strings.Index(ansi.Strip(l), "MARKER-ANSWER"); j >= 0 {
			// view row i renders at absolute screen row i (the view starts at
			// row 0 in the alternate screen); mouse events are absolute
			screenRow, screenCol = i, ansi.StringWidth(ansi.Strip(l)[:j])
		}
	}
	if screenRow < 0 {
		t.Fatal("MARKER not rendered")
	}
	t.Logf("MARKER at screen (%d,%d); block[1] y0=%d", screenRow, screenCol, m.blocks[1].y0)

	// drag across that screen row, starting exactly on the M
	tm, _ = m.Update(clickMsg(screenCol, screenRow))
	m = tm.(*model)
	tm, _ = m.Update(dragMsg(screenCol+13, screenRow))
	m = tm.(*model)
	tm, _ = m.Update(releaseMsg(screenCol+13, screenRow))
	m = tm.(*model)
	if m.sel == nil {
		t.Fatal("no selection")
	}
	got := m.selText(*m.sel)
	if !strings.Contains(got, "MARKER") {
		t.Fatalf("drag on row %d copied %q, want MARKER text", screenRow, got)
	}
	t.Logf("copied %q", got)
}
