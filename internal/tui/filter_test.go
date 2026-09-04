package tui

import (
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// Only mouse motion inside the 16 ms window is dropped; every other message
// (keys, pastes, resizes, ticks, clicks, releases, wheel) comes back as is.
func TestInputFilter(t *testing.T) {
	clock := time.Unix(1000, 0)
	f := &inputFilter{now: func() time.Time { return clock }}
	for _, msg := range []tea.Msg{
		tea.KeyPressMsg{Code: 'a', Text: "a"},
		tea.PasteMsg{Content: "pasted"},
		tea.WindowSizeMsg{Width: 80, Height: 24},
		spinner.TickMsg{},
		tea.MouseClickMsg(mouseAt(1, 2, tea.MouseLeft)),
		tea.MouseReleaseMsg(mouseAt(1, 2, tea.MouseLeft)),
		tea.MouseWheelMsg(mouseAt(1, 2, tea.MouseWheelUp)),
	} {
		if got := f.Filter(nil, msg); got != msg {
			t.Fatalf("%T must pass through unchanged, got %#v", msg, got)
		}
	}
	motion := tea.MouseMotionMsg(mouseAt(3, 4, tea.MouseLeft))
	if f.Filter(nil, motion) == nil {
		t.Fatal("the first motion must pass")
	}
	clock = clock.Add(5 * time.Millisecond)
	if f.Filter(nil, motion) != nil {
		t.Fatal("motion 5 ms later must be dropped")
	}
	clock = clock.Add(12 * time.Millisecond)
	if f.Filter(nil, motion) == nil {
		t.Fatal("motion past the window must pass")
	}
}

// The release re-samples the pointer: a drag whose last motion was thinned
// by the filter still ends at the cell the button came up on.
func TestReleaseEndsSelectionAtPointer(t *testing.T) {
	m := selTestModel()
	y := blockRowY(m, m.blocks[1].y0) // "second block here"
	x0 := m.frameNow().transcript.Min.X
	m.handleMouseSelect(clickMsg(x0, y))
	m.handleMouseSelect(dragMsg(x0+2, y))
	m.handleMouseSelect(releaseMsg(x0+6, y))
	if m.sel == nil || !m.sel.done || m.sel.cur.col != 6 {
		t.Fatalf("selection should end at the release column: %+v", m.sel)
	}
	if got := m.selText(*m.sel); got != "second" {
		t.Fatalf("copied %q, want %q", got, "second")
	}
}
