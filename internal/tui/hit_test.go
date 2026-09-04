package tui

import (
	"fmt"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// Every region rectangle answers hit() inside its corners and nowhere one
// cell past its edges, with and without the agent details banner.
func TestHitTest(t *testing.T) {
	for _, size := range [][2]int{{140, 40}, {79, 24}} {
		for _, open := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/details=%v", size[0], size[1], open), func(t *testing.T) {
				m := goldenModel(size[0], size[1])
				if open {
					m.agentOpen = "root-agent:ba06cc4c6983c16d"
					m.layout()
				}
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
				check("side", r.side, regSide)
				if size[0] >= sidebarMinWidth && r.side.Empty() || size[0] < sidebarMinWidth && !r.side.Empty() {
					t.Fatalf("sidebar rect %v at width %d", r.side, size[0])
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := goldenModel(140, 40)
			tc.prep(m)
			m.layout()
			rows := strings.Split(ansi.Strip(viewStr(m)), "\n")
			r := m.frameNow()
			if r.inputText.Empty() {
				t.Fatal("no input rectangle")
			}
			textRow := rows[r.inputText.Min.Y]
			if m.namePrompt == nil {
				if bar := rows[r.inputText.Min.Y-1]; !strings.HasPrefix(bar[r.input.Min.X:], "┃") {
					t.Fatalf("prompt bar not on the row above the input text: %q", bar)
				}
				idx := strings.Index(textRow, "Ask whip")
				if idx < 0 || ansi.StringWidth(textRow[:idx]) != r.inputText.Min.X {
					t.Fatalf("input text does not start at cell %d: %q", r.inputText.Min.X, textRow)
				}
			} else if !strings.HasPrefix(textRow[r.input.Min.X:], "name:") {
				t.Fatalf("name prompt row: %q", textRow)
			}
			if !strings.Contains(rows[r.status.Min.Y], "ctrl+p") || r.status.Min.Y != len(rows)-1 {
				t.Fatalf("status row %d: %q", r.status.Min.Y, rows[r.status.Min.Y])
			}
			if got := rows[r.transcript.Min.Y+m.contentPad()+m.blocks[0].y0-m.vp.YOffset()]; !strings.Contains(got, "find the config loader") {
				t.Fatalf("first block not where the transcript rect says: %q", got)
			}
		})
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

// A press in the sidebar on a row shared with a transcript block never seeds
// a chat selection or opens message actions.
func TestSidebarPressDoesNotSelectChat(t *testing.T) {
	m := goldenModel(140, 40)
	y := blockRowY(m, m.blocks[0].y0)
	x := m.frameNow().side.Min.X + 1
	if handled, _ := m.handleMouseSelect(clickMsg(x, y)); handled || m.sel != nil {
		t.Fatalf("sidebar press seeded a selection: handled=%v sel=%+v", handled, m.sel)
	}
	m.clickAt(x, y)
	if m.msgActions != nil {
		t.Fatal("sidebar press opened message actions")
	}
}
