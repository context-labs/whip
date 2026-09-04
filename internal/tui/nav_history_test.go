package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// fakeClock is a deterministic time source for key-repeat timing tests.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// navModel builds a model with a width-wide-enough input and a couple of
// submitted inputs in the history buffer, the way the live session does. The
// returned clock lets tests control the key-repeat window deterministically.
func navModel(history ...string) (*model, *fakeClock) {
	m := newGrowModel()
	m.hist = append([]string{}, history...)
	m.histIdx = len(m.hist) // not navigating
	clk := &fakeClock{t: time.Now()}
	m.now = clk.now
	return m, clk
}

func TestUpDownMovesWithinMultilineInput(t *testing.T) {
	m, clk := navModel("older", "newer")
	m.input.SetValue("first\nsecond")
	m.input.CursorEnd()

	if got := m.input.LineCount(); got != 2 {
		t.Fatalf("setup: want 2 logical lines, got %d (%q)", got, m.input.Value())
	}

	// cursor is on the last line; ↑ should move up within the input, not history
	startIdx := m.histIdx
	tm, _ := m.key(keyMsg(tea.KeyUp))
	m = tm.(*model)
	if m.input.Line() != 0 {
		t.Fatalf("↑ from the last line should move up within the input, got line %d (%q)", m.input.Line(), m.input.Value())
	}
	if m.histIdx != startIdx {
		t.Fatalf("↑ within the input must not walk history, histIdx %d→%d", startIdx, m.histIdx)
	}

	// now on the first line; a DELIBERATE ↑ (after a pause) rolls over to history
	clk.advance(500 * time.Millisecond)
	tm, _ = m.key(keyMsg(tea.KeyUp))
	m = tm.(*model)
	if m.histIdx != 1 {
		t.Fatalf("↑ on the first line should recall history, want histIdx 1, got %d (value=%q)", m.histIdx, m.input.Value())
	}
	if m.input.Value() != "newer" {
		t.Fatalf("expected history recall to load 'newer', got %q", m.input.Value())
	}
}

func TestDownOnLastLineRecallsNewerHistory(t *testing.T) {
	m, _ := navModel("older", "newer")
	// sitting on a recalled single-line history entry; ↓ should walk forward,
	// loading the next entry ("newer"), since the cursor is on its last row
	m.input.SetValue("older")
	m.histIdx = 0
	m.input.CursorEnd()

	tm, _ := m.key(keyMsg(tea.KeyDown))
	m = tm.(*model)
	if m.histIdx != 1 {
		t.Fatalf("↓ should recall newer history, want histIdx 1, got %d (value=%q)", m.histIdx, m.input.Value())
	}
	if m.input.Value() != "newer" {
		t.Fatalf("expected history recall to load 'newer', got %q", m.input.Value())
	}
}

func TestUpOnFirstLineOfSingleLineInputRecallsHistory(t *testing.T) {
	m, _ := navModel("solo")
	m.input.SetValue("editing")
	m.input.CursorEnd()

	tm, _ := m.key(keyMsg(tea.KeyUp))
	m = tm.(*model)
	if m.input.Value() != "solo" {
		t.Fatalf("↑ on a single-line input should recall history, got %q", m.input.Value())
	}
}

func TestDownOnLastLineOfSingleLineInputOutsideHistoryIsNoop(t *testing.T) {
	m, _ := navModel("solo")
	m.histIdx = len(m.hist) // at the newest edge, nothing newer to recall
	m.input.SetValue("editing")
	m.input.CursorEnd()

	startVal := m.input.Value()
	tm, _ := m.key(keyMsg(tea.KeyDown))
	m = tm.(*model)
	if m.input.Value() != startVal {
		t.Fatalf("↓ past the newest history entry should leave input unchanged, got %q", m.input.Value())
	}
}

// A long line that soft-wraps to two rows should let ↑/↓ move between the
// rows before rolling over to history, just like explicit newlines do.
func TestUpDownSoftWrapRowsCountAsLines(t *testing.T) {
	m, _ := navModel("hist")
	// one logical line, but wide enough to wrap to ≥2 visual rows
	m.input.SetValue(wrapString(m.input.Width() - 2))
	m.input.CursorEnd()

	// cursor is on the last wrapped row; ↑ should move up visually,
	// not recall history
	startIdx := m.histIdx
	tm, _ := m.key(keyMsg(tea.KeyUp))
	m = tm.(*model)
	if m.histIdx != startIdx {
		t.Fatalf("↑ within a soft-wrapped line must not walk history, histIdx %d→%d", startIdx, m.histIdx)
	}
	li := m.input.LineInfo()
	if li.RowOffset >= li.Height-1 {
		t.Fatalf("↑ should have moved off the last visual row (RowOffset=%d Height=%d)", li.RowOffset, li.Height)
	}
}

// Cross-session recall: a fresh session seeded with global user history (from
// every folder) must let ↑ walk back through all of it, newest first, exactly
// as session-local recall does.
func TestUpCyclesGlobalCrossSessionHistory(t *testing.T) {
	// hist holds the global seed oldest→newest (the TUI reverses the store's
	// newest-first UserHistory into this order at startup)
	m, clk := navModel("oldest across sessions", "from another folder", "most recent")
	m.input.SetValue("")
	m.input.CursorEnd()

	var got []string
	for range 3 {
		tm, _ := m.key(keyMsg(tea.KeyUp))
		m = tm.(*model)
		got = append(got, m.input.Value())
		clk.advance(500 * time.Millisecond) // deliberate presses, not a held key
	}
	want := []string{"most recent", "from another folder", "oldest across sessions"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("↑ press %d: got %q, want %q (full walk: %v)", i+1, got[i], want[i], got)
		}
	}
	// a 4th ↑ at the oldest entry is a no-op (stays put)
	tm, _ := m.key(keyMsg(tea.KeyUp))
	m = tm.(*model)
	if m.input.Value() != "oldest across sessions" {
		t.Fatalf("↑ past the oldest entry should stay, got %q", m.input.Value())
	}
}

// Regression: holding ↑ (key auto-repeat) past the top of a multi-line message
// must NOT walk back through history — the user is just trying to reach the
// start of the current message. Only a deliberate ↑ after a pause recalls.
func TestHeldUpStaysOnCurrentMessage(t *testing.T) {
	m, clk := navModel("older", "newer")
	m.input.SetValue("line one\nline two\nline three")
	m.input.CursorEnd()

	// hold ↑: repeats arrive 40ms apart. The cursor climbs to the top line…
	for range 2 {
		tm, _ := m.key(keyMsg(tea.KeyUp))
		m = tm.(*model)
		clk.advance(40 * time.Millisecond)
	}
	if m.input.Line() != 0 {
		t.Fatalf("held ↑ should have climbed to the first line, got line %d", m.input.Line())
	}
	// …and keeps going: every repeated press must be swallowed, never recall
	for range 10 {
		tm, _ := m.key(keyMsg(tea.KeyUp))
		m = tm.(*model)
		clk.advance(40 * time.Millisecond)
	}
	if m.histIdx != len(m.hist) {
		t.Fatalf("held ↑ must not walk history, histIdx=%d (value=%q)", m.histIdx, m.input.Value())
	}
	if m.input.Value() != "line one\nline two\nline three" {
		t.Fatalf("held ↑ must keep the current message, got %q", m.input.Value())
	}

	// releasing and deliberately pressing again DOES recall history
	clk.advance(500 * time.Millisecond)
	tm, _ := m.key(keyMsg(tea.KeyUp))
	m = tm.(*model)
	if m.input.Value() != "newer" {
		t.Fatalf("deliberate ↑ after a pause should recall history, got %q", m.input.Value())
	}
}

// wrapString builds a single string of spaces-separated words long enough to
// wrap to at least two rows at the given content width.
func wrapString(width int) string {
	if width < 4 {
		width = 4
	}
	w := []byte{}
	for len(w) < width*2+4 {
		w = append(w, 'w', 'o', 'r', 'd', ' ')
	}
	return string(w)
}
