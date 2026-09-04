package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// A transcript longer than the window shows a scrollbar in the margin column
// and, once scrolled up, a "↓ N more lines" chip that a click dismisses by
// jumping back to the newest rows.
func TestScrollbarAndMoreLinesPill(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(100, 30))
	for i := range 40 {
		m.append(strings.Repeat("row ", 3) + string(rune('a'+i%26)))
	}
	m.layout()
	frame := strings.Split(ansi.Strip(viewStr(m)), "\n")
	r := m.frameNow()
	if !r.pill.Empty() {
		t.Fatalf("at the bottom there must be no pill: %v", r.pill)
	}
	col := func(rows []string, x, y int) string { return string([]rune(rows[y])[x]) }
	sawTrack, sawThumb := false, false
	for y := r.transcript.Min.Y; y < r.transcript.Max.Y; y++ {
		if col(frame, r.gap.Min.X, y) == "█" { // the solid thumb; the track is a filled column (invisible once stripped)
			sawThumb = true
		} else {
			sawTrack = true
		}
	}
	if !sawTrack || !sawThumb {
		t.Fatalf("scrollbar missing (track %v thumb %v)", sawTrack, sawThumb)
	}
	if c := m.scr.CellAt(r.gap.Min.X, r.transcript.Min.Y+r.transcript.Dy()/2); c == nil || (c.Content != "█" && c.Style.Bg == nil) {
		t.Fatal("track cells must carry the element surface")
	}
	m.vp.ScrollUp(7)
	m.follow = false
	frame = strings.Split(ansi.Strip(viewStr(m)), "\n")
	r = m.frameNow()
	if r.pill.Empty() || !strings.Contains(frame[r.pill.Min.Y], "↓ 7 more lines") {
		t.Fatalf("pill missing after scrolling up: %v %q", r.pill, frame[r.transcript.Max.Y-1])
	}
	if handled, _ := m.handleMouseSelect(clickMsg(r.pill.Min.X+1, r.pill.Min.Y)); !handled || !m.vp.AtBottom() || !m.follow {
		t.Fatalf("clicking the pill should return to the bottom: handled=%v atBottom=%v follow=%v", handled, m.vp.AtBottom(), m.follow)
	}
	if !m.frameNow().pill.Empty() {
		t.Fatal("pill should vanish at the bottom")
	}
}

// A failed local command reports in the toast, not in the transcript.
func TestLocalCommandErrorsToast(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(100, 30))
	before := len(m.blocks)
	m.command("/theme nope")
	if m.toast == "" || !strings.Contains(m.toast, "usage: /theme") || m.toastKind != 3 {
		t.Fatalf("expected an error toast, got %q (kind %d)", m.toast, m.toastKind)
	}
	m.command("/definitely-not-a-command")
	if !strings.Contains(m.toast, "unknown command") || len(m.blocks) != before {
		t.Fatalf("unknown command should toast, not append: toast=%q blocks %d→%d", m.toast, before, len(m.blocks))
	}
}

// Double-click selects the word under the pointer, triple-click the row; both
// copy immediately.
func TestDoubleAndTripleClickSelect(t *testing.T) {
	m := selTestModel()
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { return clock }
	y := blockRowY(m, m.blocks[1].y0)      // "second block here"
	x := m.frameNow().transcript.Min.X + 8 // inside "block"
	click := func() {
		m.handleMouseSelect(clickMsg(x, y))
		m.handleMouseSelect(releaseMsg(x, y))
		clock = clock.Add(100 * time.Millisecond)
	}
	click()
	click()
	if m.sel == nil || !m.sel.done || m.selText(*m.sel) != "block" {
		t.Fatalf("double-click should select the word: %+v", m.sel)
	}
	click()
	if m.sel == nil || m.selText(*m.sel) != "second block here" {
		t.Fatalf("triple-click should select the row: %+v", m.sel)
	}
	clock = clock.Add(time.Second)
	click()
	if m.sel != nil && m.sel.done {
		t.Fatal("a click after the window is a plain click again")
	}
}
