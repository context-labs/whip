package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// selTestModel builds a headless model with a known transcript: two plain
// blocks, wide enough that neither wraps, and settles the layout (deferred
// layout() runs on the first Update) so contentPad/screen rows are stable.
func selTestModel() *model {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.append("hello world")
	m.append("second block here")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")}) // settle layout
	m = tm.(*model)
	m.input.SetValue("")
	return m
}

// blockRowY maps content row r to the ABSOLUTE screen row where it renders:
// viewTop + 3 (header + hint + blank) + (r + contentPad - YOffset) - vpLead,
// matching the trimmed view viewportView produces. Mouse events carry
// absolute screen coordinates, so tests must aim there too.
func blockRowY(m *model, r int) int {
	m.View() // ensure viewTop/vpLead are current (View/viewportView record them)
	return m.viewTop + 3 + (r + m.contentPad() - m.vp.YOffset) - m.vpLead
}

// Full Update-path drag: press, motion, release must select + copy the
// dragged text, and View must paint the highlight on the dragged range
// WITHOUT shifting the transcript (no jump to the bottom).
func TestDragSelectsHighlightsCopies(t *testing.T) {
	m := selTestModel()
	y := blockRowY(m, m.blocks[1].y0) // "second block here"
	before := m.View()

	tm, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: y})
	m = tm.(*model)
	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 6, Y: y})
	m = tm.(*model)
	if m.sel == nil {
		t.Fatal("motion did not start a selection")
	}
	during := m.View()
	if !strings.Contains(during, "\x1b[7msecond\x1b[27m") {
		t.Fatalf("View must highlight the dragged range:\n%q", during)
	}
	// the highlight must not move the text: "second block here" stays on the
	// same screen row before and during the drag (no jump to the bottom).
	rowOf := func(v string) int {
		for i, l := range strings.Split(v, "\n") {
			if strings.Contains(ansi.Strip(l), "second block here") {
				return i
			}
		}
		return -1
	}
	if rowOf(before) != rowOf(during) {
		t.Fatalf("text shifted during drag: before row %d, during row %d", rowOf(before), rowOf(during))
	}

	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 6, Y: y})
	m = tm.(*model)
	if m.sel == nil || !m.sel.done {
		t.Fatal("release must keep a done selection for the highlight")
	}
	if got := m.selText(*m.sel); got != "second" {
		t.Fatalf("copied %q, want %q", got, "second")
	}
	// after release the highlight is gone but the text still doesn't move
	after := m.View()
	if rowOf(before) != rowOf(after) {
		t.Fatalf("text shifted after release: before row %d, after row %d", rowOf(before), rowOf(after))
	}
	// a keypress clears the highlight
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = tm.(*model)
	if m.sel != nil {
		t.Fatal("keypress must clear the selection highlight")
	}
}

// A press+release with no drag is a click: it replays (tool expand) and
// leaves no selection behind.
func TestClickIsNotASelection(t *testing.T) {
	m := selTestModel()
	y := blockRowY(m, m.blocks[0].y0)
	if handled, _ := m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: y}); !handled {
		t.Fatal("press inside the block range is consumed (the viewport must not scroll on it)")
	}
	if handled, _ := m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 2, Y: y}); !handled {
		t.Fatal("release is consumed (it replays the click)")
	}
	if m.sel != nil {
		t.Fatal("a click must leave no selection behind")
	}
}

// A plain click on a tool block still expands it.
func TestClickExpandsToolBlock(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.appendRaw(blockTool, "one\ntwo\nthree\nfour\nfive\nsix\nseven")
	m.refreshVP()
	y := blockRowY(m, m.blocks[0].y0)
	tm, _ := m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 3, Y: y})
	m = tm.(*model)
	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 3, Y: y})
	m = tm.(*model)
	if !m.blocks[0].expanded {
		t.Fatal("click must still expand the tool block")
	}
	if m.sel != nil {
		t.Fatal("a click must leave no selection")
	}
}

// A press on the header/status rows must NOT start a selection —
// handleMouseSelect runs before the dock/effort click handlers, so consuming
// those presses would break their clicks. The input box IS selectable now (a
// press there starts an input-region selection). A drag that overshoots past
// the last block still clamps (motion only).
func TestPressOutsideTranscriptNotConsumed(t *testing.T) {
	m := selTestModel()
	m.View()
	for _, y := range []int{0, 1, m.height - 1} { // header rows + status line
		if m.inInputRow(y) {
			continue // the input box is its own selectable region now
		}
		if handled, _ := m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: y}); handled {
			t.Fatalf("press on non-selectable row %d must not be consumed", y)
		}
		if m.sel != nil {
			t.Fatalf("press on non-selectable row %d must not start a selection", y)
		}
	}
	// but a drag that starts on the transcript and overshoots below clamps
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: blockRowY(m, m.blocks[0].y0)})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 80, Y: m.height - 1})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 80, Y: m.height - 1})
	if got := m.selText(*m.sel); got != "hello world\n\nsecond block here" {
		t.Fatalf("overshooting drag selected %q", got)
	}
}

// Backward drags (right-to-left) select the same text.
func TestDragBackward(t *testing.T) {
	m := selTestModel()
	y := blockRowY(m, m.blocks[1].y0) // "second block here"
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 17, Y: y})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 7, Y: y})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 7, Y: y})
	if got := m.selText(*m.sel); got != "block here" {
		t.Fatalf("backward drag selected %q, want %q", got, "block here")
	}
}

// A drag over the input box selects its text and copies it on release, with a
// reverse-video highlight that does not move the box.
func TestInputDragSelectsHighlightsCopies(t *testing.T) {
	m := selTestModel()
	m.input.SetValue("copy me from input")
	tm, _ := m.Update(mkWinSize(80, 30))
	m = tm.(*model)
	m.View()
	iy := m.inputTop
	if iy < 0 {
		t.Fatal("inputTop must be set after View")
	}
	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: iy})
	m = tm.(*model)
	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 15, Y: iy})
	m = tm.(*model)
	if m.sel == nil || !m.sel.anchor.input {
		t.Fatal("a drag over the input box must start an input-region selection")
	}
	if !strings.Contains(m.View(), "\x1b[7m") {
		t.Fatalf("input selection must paint a highlight:\n%q", m.View())
	}
	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 15, Y: iy})
	m = tm.(*model)
	if m.sel == nil || !m.sel.done {
		t.Fatal("release must keep a done selection for the highlight")
	}
	if got := m.selText(*m.sel); !strings.Contains(got, "copy me") {
		t.Fatalf("input drag copied %q, want it to contain %q", got, "copy me")
	}
}

// A no-drag click in the input box is just focus: no selection, and typing
// afterward still reaches the textarea.
func TestInputClickThenType(t *testing.T) {
	m := selTestModel()
	m.input.SetValue("")
	tm, _ := m.Update(mkWinSize(80, 30))
	m = tm.(*model)
	m.View()
	iy := m.inputTop
	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: iy})
	m = tm.(*model)
	tm, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 2, Y: iy})
	m = tm.(*model)
	if m.sel != nil {
		t.Fatal("a no-drag input click must leave no selection")
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")})
	m = tm.(*model)
	if m.input.Value() != "hello" {
		t.Fatalf("typing after an input click broke: input=%q", m.input.Value())
	}
}

// A multi-row drag copies the rows AS RENDERED: the blank separator between
// blocks pastes as a blank line, exactly like a terminal's native copy.
func TestDragAcrossBlocks(t *testing.T) {
	m := selTestModel()
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 6, Y: blockRowY(m, m.blocks[0].y0)})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 6, Y: blockRowY(m, m.blocks[1].y0)})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 6, Y: blockRowY(m, m.blocks[1].y0)})
	if got := m.selText(*m.sel); got != "world\n\nsecond" {
		t.Fatalf("cross-block drag selected %q, want %q", got, "world\n\nsecond")
	}
}

// Blank rows INSIDE a block (paragraph breaks in rendered markdown) must
// survive the copy — dropping them glued multi-paragraph output into one
// run-on text (the missing-newlines bug).
func TestCopyKeepsParagraphBreaks(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.append("para one\n\npara two")
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")}) // settle layout
	m = tm.(*model)
	m.input.SetValue("")
	b := m.blocks[0]
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 0, Y: blockRowY(m, b.y0)})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 8, Y: blockRowY(m, b.y1)})
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 8, Y: blockRowY(m, b.y1)})
	if got := m.selText(*m.sel); got != "para one\n\npara two" {
		t.Fatalf("paragraph break lost: copied %q", got)
	}
}

// contentLine reconstructs wrapped rows from the block's raw text, matching
// the renderer's word-aware wrap (a space consumed by the break is gone).
func TestContentLineWrapped(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(10, 30))
	m.append("abcdefghij klmnop") // wraps at width 10: "abcdefghij" / "klmnop"
	b := m.blocks[0]
	if got := m.contentLine(b.y0); got != "abcdefghij" {
		t.Fatalf("row 0: got %q", got)
	}
	if got := m.contentLine(b.y0 + 1); got != "klmnop" {
		t.Fatalf("row 1: got %q", got)
	}
}

// reverseRange wraps the requested cells in reverse video without disturbing
// text outside the range or ANSI sequences inside it.
func TestReverseRange(t *testing.T) {
	if got := reverseRange("hello world", 0, 5); got != "\x1b[7mhello\x1b[27m world" {
		t.Fatalf("got %q", got)
	}
	styled := "\x1b[31mhello\x1b[0m world"
	got := reverseRange(styled, 6, 11)
	if !strings.Contains(got, "\x1b[31mhello\x1b[0m") || !strings.Contains(got, "\x1b[7mworld\x1b[27m") {
		t.Fatalf("styled line mangled: %q", got)
	}
	// an SGR reset INSIDE the range cancels reverse video — it must be
	// re-asserted or the highlight visibly dies at the reset (glamour-styled
	// lines reset after every chunk, cutting the highlight mid-row)
	got = reverseRange("ab\x1b[0mcd", 0, 4)
	if !strings.Contains(got, "\x1b[0m\x1b[7mcd") {
		t.Fatalf("reverse video must be re-asserted after a reset: %q", got)
	}
}

// Dragging past the viewport's top/bottom edge scrolls it a line, extends the
// selection to the row now under the pointer, and arms a tick that repeats
// the scroll while the pointer stays parked there — so a drag can select more
// than one screenful.
func TestDragEdgeAutoScroll(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	for i := range 60 {
		m.append(fmt.Sprintf("line-%02d", i))
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	m = tm.(*model)
	m.input.SetValue("")
	m.View()
	if m.vp.YOffset == 0 {
		t.Fatal("test setup: viewport must start scrolled to the bottom")
	}
	start := m.vp.YOffset

	// press inside the transcript, then drag up past the header
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 2, Y: m.viewTop + 5})
	handled, cmd := m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: 2, Y: m.viewTop})
	if !handled || cmd == nil {
		t.Fatalf("edge drag must be handled and arm the scroll tick (handled=%v cmd=%v)", handled, cmd != nil)
	}
	if m.vp.YOffset != start-1 {
		t.Fatalf("edge drag must scroll up one line: %d -> %d", start, m.vp.YOffset)
	}

	// parked pointer: each tick keeps scrolling until the top, then disarms
	for i := 0; i < 200 && m.selEdgeScroll() != nil; i++ {
	}
	if m.vp.YOffset != 0 {
		t.Fatalf("ticks must scroll to the top, YOffset=%d", m.vp.YOffset)
	}
	if m.selEdgeScroll() != nil {
		t.Fatal("at the top the tick must disarm")
	}
	// the selection followed the scroll all the way to the first row
	if lo, _ := selOrder(*m.sel); lo.row != m.blocks[0].y0 {
		t.Fatalf("selection must extend to the first content row, got %d", lo.row)
	}

	// and the bottom edge scrolls back down
	m.selDragY = m.height - 1
	if m.selEdgeScroll() == nil {
		t.Fatal("drag below the viewport must scroll down")
	}
	if m.vp.YOffset != 1 {
		t.Fatalf("bottom edge must scroll down one line, YOffset=%d", m.vp.YOffset)
	}

	// release ends the drag: the tick no-ops from then on
	m.handleMouseSelect(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: 2, Y: m.viewTop})
	if m.selEdgeScroll() != nil {
		t.Fatal("after release the tick must disarm")
	}
}
