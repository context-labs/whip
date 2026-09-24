package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// The left column is three panels: the open one takes what two collapsed
// headers and the gap rows leave, at every height; the headers sit where the
// rectangles say.
func TestLeftColumnPartition(t *testing.T) {
	pinDarkTheme(t)
	for _, tc := range []struct {
		h      int
		open   int
		panels [3]uv.Rectangle
	}{
		{40, paneAgents, [3]uv.Rectangle{uv.Rect(1, 1, 42, 29), uv.Rect(1, 31, 42, 3), uv.Rect(1, 35, 42, 3)}},
		{40, paneContext, [3]uv.Rectangle{uv.Rect(1, 1, 42, 3), uv.Rect(1, 5, 42, 29), uv.Rect(1, 35, 42, 3)}},
		{24, paneLSP, [3]uv.Rectangle{uv.Rect(1, 1, 42, 3), uv.Rect(1, 5, 42, 3), uv.Rect(1, 9, 42, 13)}},
	} {
		m := goldenModel(140, tc.h)
		m.leftPane = tc.open
		rows := strings.Split(ansi.Strip(viewStr(m)), "\n")
		r := m.frameNow()
		if r.panels != tc.panels {
			t.Fatalf("h=%d open=%d: panels %v, want %v", tc.h, tc.open, r.panels, tc.panels)
		}
		for pane, title := range []string{"[1] Agents", "[2] Context", "[3] LSP"} {
			rc := tc.panels[pane]
			if head := rows[rc.Min.Y+1]; !strings.Contains(head, title) {
				t.Fatalf("h=%d open=%d: panel %d header row %d = %q", tc.h, tc.open, pane, rc.Min.Y+1, head)
			}
			if gap := rows[rc.Max.Y]; rc.Max.Y < tc.h-1 && strings.TrimSpace(gap[:44]) != "" {
				t.Fatalf("h=%d open=%d: row %d under panel %d is not a gap: %q", tc.h, tc.open, rc.Max.Y, pane, gap)
			}
		}
		if body := rows[tc.panels[tc.open].Min.Y+3]; strings.TrimSpace(body[:44]) == "" {
			t.Fatalf("h=%d open=%d: the open panel has no body row: %q", tc.h, tc.open, body)
		}
	}
}

// ctrl+x 1/2/3 pick the expanded panel; the tree's focus shows Agents while
// it lasts, and ctrl+t makes Agents the pick.
func TestLeftPaneChordsAndFocus(t *testing.T) {
	m := goldenModel(140, 40)
	m.thinKey(ctrlKey('x'))
	next, _ := m.thinKey(keyRunes("3"))
	m = next.(*model)
	if m.leftPane != paneLSP || m.openPane() != paneLSP {
		t.Fatalf("ctrl+x 3: leftPane=%d open=%d", m.leftPane, m.openPane())
	}
	if hints := ansi.Strip(strings.Split(viewStr(m), "\n")[39]); strings.Contains(hints, "panels") {
		t.Fatalf("panel hint shown without the leader armed: %q", hints)
	}
	m.thinKey(ctrlKey('x'))
	if hints := ansi.Strip(strings.Split(viewStr(m), "\n")[39]); !strings.Contains(hints, "1·2·3 panels") {
		t.Fatalf("leader hints lack the panel chord: %q", hints)
	}
	next, _ = m.thinKey(keyMsg(tea.KeyEsc)) // cancels the leader
	m = next.(*model)
	next, _ = m.thinKey(keyMsg(tea.KeyDown)) // ↓ on an empty input focuses the tree
	m = next.(*model)
	if !m.agentsFocus || m.openPane() != paneAgents || m.leftPane != paneLSP {
		t.Fatalf("focused tree: focus=%v open=%d leftPane=%d", m.agentsFocus, m.openPane(), m.leftPane)
	}
	if frame := ansi.Strip(viewStr(m)); !strings.HasPrefix(strings.Split(frame, "\n")[1][1:], "┃") {
		t.Fatalf("focused Agents panel lacks its bar: %q", strings.Split(frame, "\n")[1][:44])
	}
	next, _ = m.thinKey(keyMsg(tea.KeyEsc))
	m = next.(*model)
	if m.agentsFocus || m.openPane() != paneLSP {
		t.Fatalf("esc: focus=%v open=%d", m.agentsFocus, m.openPane())
	}
	next, _ = m.thinKey(ctrlKey('t'))
	m = next.(*model)
	if !m.agentsFocus || m.leftPane != paneAgents {
		t.Fatalf("ctrl+t: focus=%v leftPane=%d", m.agentsFocus, m.leftPane)
	}
}
