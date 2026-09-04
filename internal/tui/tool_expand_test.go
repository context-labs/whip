package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Tool results collapse to a preview; ctrl+e toggles the latest one and
// clicking the block expands it.
func TestToolExpand(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	result := "line1\nline2\nline3\nline4\nline5\nline6\nline7\nline8"
	m.appendRaw(blockTool, result)

	// collapsed: a single "↳ N lines" hint, no preview rows
	out := ansi.Strip(m.blocks[0].render(m.width))
	if strings.Contains(out, "line8") || !strings.Contains(out, "↳ 8 lines") {
		t.Fatalf("collapsed render wrong: %q", out)
	}

	// ctrl+e expands the latest tool block
	tm, _ := m.key(keyMsg(tea.KeyCtrlE))
	m = tm.(*model)
	out = ansi.Strip(m.blocks[0].render(m.width))
	if !strings.Contains(out, "line8") || strings.Contains(out, "…") {
		t.Fatalf("expanded render wrong: %q", out)
	}

	// and collapses back
	tm, _ = m.key(keyMsg(tea.KeyCtrlE))
	m = tm.(*model)
	if m.blocks[0].expanded {
		t.Fatal("second ctrl+e should collapse")
	}

	// click on the block row expands it (press consumed by selection; the
	// release replays it as a click)
	m.refreshVP()
	screenY := blockRowY(m, m.blocks[0].y0) // content starts at screen row 3
	tm, _ = m.Update(clickMsg(5, screenY))
	m = tm.(*model)
	tm, _ = m.Update(releaseMsg(5, screenY))
	m = tm.(*model)
	if !m.blocks[0].expanded {
		t.Fatalf("click at screen Y=%d should expand the tool block", screenY)
	}
}
