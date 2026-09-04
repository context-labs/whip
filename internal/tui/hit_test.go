package tui

import (
	"fmt"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/session"
)

// Every region rectangle answers hit() inside its corners and nowhere one
// cell past its edges, with and without the agent details banner.
func TestHitTest(t *testing.T) {
	for _, size := range []struct {
		w, h int
		repl bool
	}{{140, 40, false}, {79, 24, false}, {160, 40, true}, {120, 40, true}} {
		for _, open := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/repl=%v/details=%v", size.w, size.h, size.repl, open), func(t *testing.T) {
				m := goldenModel(size.w, size.h)
				m.replPanel = size.repl
				m.recalcWidth()
				if open {
					m.agentOpen = "root-agent:ba06cc4c6983c16d"
				}
				m.layout()
				viewStr(m)
				r := m.frameNow()
				if open && r.details.Empty() {
					t.Fatal("details rectangle missing while an agent is open")
				}
				if r.transcript.Min.Y != r.details.Max.Y+boolInt(open) {
					t.Fatalf("transcript should start under the details banner: %v vs %v", r.transcript, r.details)
				}
				check := func(name string, rc uv.Rectangle, want region) {
					if rc.Empty() {
						return
					}
					for _, pt := range [][2]int{{rc.Min.X, rc.Min.Y}, {rc.Max.X - 1, rc.Min.Y}, {rc.Min.X, rc.Max.Y - 1}, {rc.Max.X - 1, rc.Max.Y - 1}} {
						if reg, _, _ := m.hit(pt[0], pt[1]); reg != want {
							t.Fatalf("%s: corner %v → region %d, want %d (%v)", name, pt, reg, want, rc)
						}
					}
					outside := [][2]int{{rc.Max.X, rc.Min.Y}, {rc.Min.X, rc.Min.Y - 1}, {rc.Min.X, rc.Max.Y}}
					if want != regTranscript { // the left margin counts as transcript (edge drags)
						outside = append(outside, [2]int{rc.Min.X - 1, rc.Min.Y})
					}
					for _, pt := range outside {
						if reg, _, _ := m.hit(pt[0], pt[1]); reg == want {
							t.Fatalf("%s: one past the edge %v still hits region %d (%v)", name, pt, reg, rc)
						}
					}
				}
				check("inputText", r.inputText, regInput)
				check("transcript", r.transcript, regTranscript)
				check("left", r.left, regLeft)
				check("side", r.side, regSide)
				wantLeft := size.w >= sidebarMinWidth && !(size.repl && size.w < replMinWide)
				if wantLeft == r.left.Empty() || (size.repl && size.w >= sidebarMinWidth) == r.side.Empty() {
					t.Fatalf("columns at %dx%d repl=%v: left %v side %v", size.w, size.h, size.repl, r.left, r.side)
				}
			})
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// The rectangles self-locate in the rendered frame: the prompt bar sits on
// the row above the input text, the input text row shows the placeholder, and
// the status line is the last row.
func TestLayoutFrameOracle(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(m *model)
	}{
		{"idle", func(*model) {}},
		{"details", func(m *model) { m.agentOpen = "root-agent:ba06cc4c6983c16d" }},
		{"quit-hint", func(m *model) { m.quit1 = true }},
		{"esc-hint", func(m *model) { m.esc1 = true }},
		{"rewind", func(m *model) { m.rew = &rewindState{entries: []rewindEntry{{cut: 0, text: "x"}}} }},
		{"name-prompt", func(m *model) { m.openNamePrompt("name:", "", func(string) {}) }},
		{"thinking", func(m *model) { m.busy = true; m.thinkStart = m.now() }},
		{"thought", func(m *model) { m.curThink = "considering the options" }},
		{"current", func(m *model) { m.current = "streaming partial answer" }},
		{"plan", func(m *model) {
			m.plan = []daemon.PlanItem{{Content: "one", Status: "pending"}, {Content: "two", Status: "done"}}
		}},
		{"permission", func(m *model) { m.permDialog = &permDialog{daemon: &session.PermissionSnapshot{ID: "p1", Rule: "x"}} }},
		{"interactive", func(m *model) { m.iactive = &interactive{output: "$ ls\nfoo\n"} }},
	} {
		for _, w := range []int{140, 79} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, w), func(t *testing.T) {
				m := goldenModel(w, 40)
				tc.prep(m)
				m.layout()
				rows := strings.Split(ansi.Strip(viewStr(m)), "\n")
				r := m.frameNow()
				if len(rows) != 40 {
					t.Fatalf("frame has %d rows", len(rows))
				}
				if !strings.Contains(rows[r.footer.Min.Y], "ctrl+x") || r.footer.Min.Y != len(rows)-1 {
					t.Fatalf("footer row %d: %q", r.footer.Min.Y, rows[r.footer.Min.Y])
				}
				if got := rows[r.transcript.Min.Y+m.contentPad()+m.blocks[0].y0-m.vp.YOffset()]; !strings.Contains(got, "find the config loader") {
					t.Fatalf("first block not where the transcript rect says: %q", got)
				}
				if m.iactive != nil {
					if !r.inputText.Empty() {
						t.Fatal("an interactive command hides the input")
					}
					return
				}
				if r.inputText.Empty() {
					t.Fatal("no input rectangle")
				}
				textRow := rows[r.inputText.Min.Y]
				if m.namePrompt == nil {
					if bar := rows[r.inputText.Min.Y-1]; !strings.HasPrefix(bar[r.input.Min.X:], "┃") {
						t.Fatalf("prompt bar not on the row above the input text: %q", bar)
					}
					idx := strings.Index(textRow, m.input.Placeholder[:8]) // the placeholder changes while busy
					if idx < 0 || ansi.StringWidth(textRow[:idx]) != r.inputText.Min.X {
						t.Fatalf("input text does not start at cell %d: %q", r.inputText.Min.X, textRow)
					}
				} else if !strings.HasPrefix(textRow[r.input.Min.X:], "name:") {
					t.Fatalf("name prompt row: %q", textRow)
				}
			})
		}
	}
}

// With an agent's details open above the transcript, clicks still land on
// the block under the pointer (the row math used to be off by the banner).
func TestMouseWithAgentDetailsOpen(t *testing.T) {
	m := goldenModel(140, 40)
	m.agentOpen = "root-agent:ba06cc4c6983c16d"
	m.layout()
	y := blockRowY(m, m.blocks[len(m.blocks)-1].y0) // the assistant block
	m.clickAt(m.frameNow().transcript.Min.X+2, y)
	if m.msgActions == nil || m.msgActions.block != len(m.blocks)-1 {
		t.Fatalf("click did not hit the assistant block: %+v", m.msgActions)
	}
}

// A press in the name prompt's text starts an input selection at its own
// origin (the label pushes the text right; the boxed prompt's +3 gutter does
// not apply).
func TestNamePromptDragSelect(t *testing.T) {
	m := goldenModel(140, 40)
	m.openNamePrompt("name:", "", func(string) {})
	m.input.SetValue("hello")
	m.layout()
	viewStr(m)
	it := m.frameNow().inputText
	if handled, _ := m.handleMouseSelect(clickMsg(it.Min.X+1, it.Min.Y)); !handled || m.sel == nil || !m.sel.anchor.input || m.sel.anchor.col != 1 {
		t.Fatalf("press in the name prompt: handled=%v sel=%+v (rect %v)", handled, m.sel, it)
	}
}

// A press in either column on a row shared with a transcript block never
// seeds a chat selection or opens message actions; a press in the margin left
// of the chat (the screen edge, or the gap after the left column) still does.
func TestSidebarPressDoesNotSelectChat(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    int
		repl bool
		x    func(r frameRects) int
	}{
		{"left column", 140, false, func(r frameRects) int { return r.left.Min.X + 1 }},
		{"repl panel", 160, true, func(r frameRects) int { return r.side.Min.X + 1 }},
	} {
		m := goldenModel(tc.w, 40)
		m.replPanel = tc.repl
		m.recalcWidth()
		m.layout()
		y := blockRowY(m, m.blocks[0].y0)
		x := tc.x(m.frameNow())
		if handled, _ := m.handleMouseSelect(clickMsg(x, y)); handled || m.sel != nil {
			t.Fatalf("%s: press seeded a selection: handled=%v sel=%+v", tc.name, handled, m.sel)
		}
		m.clickAt(x, y)
		if m.msgActions != nil {
			t.Fatalf("%s: press opened message actions", tc.name)
		}
		if handled, _ := m.handleMouseSelect(clickMsg(m.frameNow().main.Min.X-1, y)); !handled || m.sel == nil {
			t.Fatalf("%s: a press in the chat's margin should start a selection", tc.name)
		}
	}
	m := goldenModel(79, 24)
	if handled, _ := m.handleMouseSelect(clickMsg(0, blockRowY(m, m.blocks[0].y0))); !handled || m.sel == nil {
		t.Fatal("narrow: a press at the screen edge should start a selection")
	}
}

// The columns partition the screen: pad, left column, gap, chat, scrollbar
// column, (REPL panel, pad); every column stops above the blank row and the
// footer. Narrow terminals keep the bare chat geometry.
func TestColumnGeometry(t *testing.T) {
	for _, tc := range []struct {
		w, h        int
		repl        bool
		left, side  uv.Rectangle
		mainX, chat int
	}{
		{140, 40, false, uv.Rect(1, 0, 42, 38), uv.Rectangle{}, 44, 95},
		{160, 40, true, uv.Rect(1, 0, 42, 38), uv.Rect(117, 0, 42, 38), 44, 72},
		{200, 40, true, uv.Rect(1, 0, 42, 38), uv.Rect(135, 0, 64, 38), 44, 90},
		{120, 40, true, uv.Rectangle{}, uv.Rect(77, 0, 42, 38), 2, 74},
		{79, 24, false, uv.Rectangle{}, uv.Rectangle{}, 2, 76},
		{79, 24, true, uv.Rectangle{}, uv.Rectangle{}, 2, 76},
	} {
		m := goldenModel(tc.w, tc.h)
		m.replPanel = tc.repl
		m.recalcWidth()
		m.layout()
		r := m.frameNow()
		if r.left != tc.left || r.side != tc.side || r.main.Min.X != tc.mainX || r.main.Dx() != tc.chat || m.width != tc.chat {
			t.Fatalf("%dx%d repl=%v: left %v side %v main %v width %d", tc.w, tc.h, tc.repl, r.left, r.side, r.main, m.width)
		}
		if r.gap != uv.Rect(r.main.Max.X, 0, 1, tc.h) {
			t.Fatalf("%dx%d: scrollbar column %v, want the column after the chat", tc.w, tc.h, r.gap)
		}
		if edge := r.side.Max.X; !r.side.Empty() && edge != tc.w-1 {
			t.Fatalf("%dx%d: REPL panel ends at %d, want a one-cell pad", tc.w, tc.h, edge)
		}
	}
}

// The region rectangles partition the main column top to bottom and the
// status line lands on the last row in every layout state: the budget layout()
// gives the viewport and the geometry View draws come from one measurement.
func TestLayoutRectsPartitionMainColumn(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(m *model)
	}{
		{"idle", func(*model) {}},
		{"details", func(m *model) { m.agentOpen = "root-agent:ba06cc4c6983c16d" }},
		{"thinking", func(m *model) { m.busy = true; m.thinkStart = m.now() }},
		{"thought", func(m *model) { m.curThink = "considering the options" }},
		{"current", func(m *model) { m.current = "streaming partial answer" }},
		{"quit-hint", func(m *model) { m.quit1 = true }},
		{"esc-hint", func(m *model) { m.escClr = true }},
		{"rewind", func(m *model) { m.rew = &rewindState{entries: []rewindEntry{{cut: 0, text: "x"}}} }},
		{"name-prompt", func(m *model) { m.openNamePrompt("name:", "", func(string) {}) }},
		{"tall-input", func(m *model) { m.input.SetValue("one\ntwo\nthree\nfour") }},
		{"plan", func(m *model) { m.plan = []daemon.PlanItem{{Content: "one", Status: "pending"}} }},
		{"permission", func(m *model) { m.permDialog = &permDialog{daemon: &session.PermissionSnapshot{ID: "p1", Rule: "x"}} }},
		{"interactive", func(m *model) { m.iactive = &interactive{output: "$ ls\nfoo\n"} }},
	} {
		for _, w := range []int{140, 79} {
			t.Run(fmt.Sprintf("%s/%d", tc.name, w), func(t *testing.T) {
				m := goldenModel(w, 40)
				tc.prep(m)
				m.layout()
				frame := viewStr(m)
				r := m.frameNow()
				if h := lipgloss.Height(frame); h != 40 {
					t.Fatalf("frame is %d rows", h)
				}
				y := 0
				for _, rc := range []uv.Rectangle{r.details, r.transcript, r.input, r.footer} {
					if rc.Empty() {
						continue
					}
					if rc.Min.Y < y {
						t.Fatalf("%s: %v overlaps the region above (y=%d)", tc.name, rc, y)
					}
					y = rc.Max.Y
				}
				if r.footer.Min.Y != 39 || r.footer.Min.X != 0 || r.footer.Dx() != w {
					t.Fatalf("%s: footer %v, want the full last row", tc.name, r.footer)
				}
				if r.transcript.Dy() != m.vp.Height() || r.transcript.Dy() < 1 {
					t.Fatalf("%s: transcript rect %v vs viewport height %d", tc.name, r.transcript, m.vp.Height())
				}
			})
		}
	}
}
