package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// shift+mouse must pass through unconsumed so the terminal's native
// selection (drag-to-copy) works while capture is on.
func TestShiftMousePassesThrough(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.appendRaw(blockTool, "line1\nline2")
	m.refreshVP()
	before := m.blocks[0].expanded
	rowY := blockRowY(m, m.blocks[0].y0)
	// shift+click on the tool block must NOT expand it (native selection owns it)
	tm, _ := m.Update(tea.MouseClickMsg{X: 5, Y: rowY, Button: tea.MouseLeft, Mod: tea.ModShift})
	m = tm.(*model)
	if m.blocks[0].expanded != before {
		t.Fatal("shift+click must not toggle the block — it belongs to native selection")
	}
	// plain click still toggles (release-without-drag replays it)
	tm, _ = m.Update(clickMsg(5, rowY))
	m = tm.(*model)
	tm, _ = m.Update(releaseMsg(5, rowY))
	m = tm.(*model)
	if m.blocks[0].expanded == before {
		t.Fatal("plain click should toggle the block")
	}
}

// /theme auto reports the detected scheme AND the source of the decision.
func TestThemeAutoReportsSource(t *testing.T) {
	m := compactCmdModel()
	m.setTheme("light")
	m.command("/theme auto")
	var note string
	for _, b := range m.blocks {
		if strings.Contains(b.text, "◐ theme:") {
			note = b.text
		}
	}
	if !strings.Contains(note, "(auto:") {
		t.Fatalf("auto should report the detection source, got %q", note)
	}
}
