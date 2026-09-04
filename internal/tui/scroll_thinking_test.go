package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Reproduction for the small-terminal scroll bug: reasoning streams, then a
// response longer than the screen streams. On a small terminal the user sees
// the response cut off at the top with reasoning traces above it, and
// scrolling up goes "straight to the reasoning" — the response content is
// only visible again after enlarging the terminal.
//
// What MUST hold instead: every completed reasoning line and every response
// line is reachable by scrolling the transcript viewport, and the rendered
// frame never exceeds the terminal height (overflow pushes the top rows into
// terminal scrollback, which is the "cut off at the top" symptom).
func TestThinkingAndResponseScrollFullyOnSmallTerminal(t *testing.T) {
	m := compactCmdModel()
	m.showThinking = true
	m.busy = true               // streaming turn in flight: the spinner rows are live chrome
	m.Update(mkWinSize(80, 12)) // small terminal

	// Stream reasoning: thinkMsg accumulates; complete lines flush to blocks.
	for range 15 {
		um, _ := m.Update(thinkMsg("reasoning trace line about the plan\n"))
		m = um.(*model)
	}
	// Stream the response: complete lines append as assistant blocks.
	for i := range 10 {
		um, _ := m.Update(textMsg("response line " + string(rune('a'+i)) + " of the actual answer\n"))
		m = um.(*model)
	}
	// End the turn: partial lines flush, busy clears (chrome back to base).
	m.flushThink()
	m.flushCurrent()
	m.busy = false
	m.layout()

	// 1. The frame must never be taller than the terminal — overflow pushes
	//    top rows into terminal scrollback, which is exactly the "cut off at
	//    the top" the user sees.
	if h := lipgloss.Height(m.View()); h > m.height {
		t.Errorf("frame height %d exceeds terminal height %d — top rows scrolled off-screen", h, m.height)
	}

	// 2. Every response line must be reachable by scrolling: scroll to the
	//    very top and walk down; every "response line x" must appear in some
	//    viewport position.
	m.vp.GotoTop()
	seen := map[string]bool{}
	for i := range 10 {
		seen[string(rune('a'+i))] = false
	}
	for {
		v := m.viewportView()
		for i := range 10 {
			if strings.Contains(v, "response line "+string(rune('a'+i))) {
				seen[string(rune('a'+i))] = true
			}
		}
		if m.vp.AtBottom() {
			break
		}
		m.vp.LineDown(1)
	}
	var missing []string
	for i := range 10 {
		k := string(rune('a' + i))
		if !seen[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		t.Errorf("response lines never visible at any scroll position: %v", missing)
	}
}

// The streaming phase must hold the same invariants: with curThink/current
// live below the viewport, the frame still fits and prior reasoning stays
// scrollable.
func TestFrameFitsWhileReasoningStreams(t *testing.T) {
	m := compactCmdModel()
	m.showThinking = true
	m.busy = true
	m.Update(mkWinSize(80, 12))

	for range 30 {
		um, _ := m.Update(thinkMsg("a long reasoning trace line that keeps coming\n"))
		m = um.(*model)
	}
	um, _ := m.Update(textMsg("the response begins"))
	m = um.(*model)
	m.layout()

	if h := lipgloss.Height(m.View()); h > m.height {
		t.Errorf("streaming frame height %d exceeds terminal height %d", h, m.height)
	}

	// Scrolled up, the earliest reasoning blocks must be reachable.
	m.vp.GotoTop()
	top := m.viewportView()
	if !strings.Contains(top, "reasoning trace") {
		t.Errorf("top of transcript doesn't show reasoning:\n%s", top)
	}
}

// A long in-flight response chunk must never render a frame taller than the
// terminal (overflow pushes the transcript's top into scrollback — the "cut
// off at the top" symptom). The live area is capped to its tail so the
// transcript keeps a usable window.
func TestFrameHeightDuringLongInflightResponse(t *testing.T) {
	newStreamingModel := func(height int) *model {
		m := compactCmdModel()
		m.showThinking = true
		m.busy = true
		m.Update(mkWinSize(80, height))
		for i := range 5 {
			um, _ := m.Update(thinkMsg("thinking " + string(rune('0'+i)) + "\n"))
			m = um.(*model)
		}
		// An in-flight response with embedded newlines that never flushed.
		m.current = "answer\nanswer\nanswer\nanswer\nanswer\nanswer\nanswer\nanswer"
		m.layout()
		return m
	}

	// Realistic small terminal: the transcript floor holds — the live area is
	// what gets trimmed, never the transcript below minTranscriptRows.
	t.Run("small terminal keeps the transcript floor", func(t *testing.T) {
		m := newStreamingModel(20)
		if h := lipgloss.Height(m.View()); h > m.height {
			t.Errorf("frame %d rows > terminal %d rows:\n%s", h, m.height, m.View())
		}
		if m.vp.Height < minTranscriptRows {
			t.Errorf("vp.Height=%d < minTranscriptRows=%d — the live area ate the transcript floor", m.vp.Height, minTranscriptRows)
		}
		// And the live area still shows its growing tail.
		if cv := m.currentViewCapped(); !strings.Contains(cv, "answer") {
			t.Errorf("live area lost its tail entirely: %q", cv)
		}
	})

	// Degenerate tiny terminal: a 5-row floor is mathematically impossible, so
	// the invariant is the frame fits and the split stays fair (live area never
	// takes more than the transcript gets).
	t.Run("tiny terminal fits with a fair split", func(t *testing.T) {
		m := newStreamingModel(12)
		if h := lipgloss.Height(m.View()); h > m.height {
			t.Errorf("frame %d rows > terminal %d rows:\n%s", h, m.height, m.View())
		}
		live := lipgloss.Height(m.currentViewCapped())
		if live > m.vp.Height {
			t.Errorf("live area %d rows > transcript %d rows on a %d-row terminal", live, m.vp.Height, m.height)
		}
	})
}

// While the user is scrolled up reading reasoning, streamed response lines
// must not yank them to the bottom: follow is only re-engaged by scrolling
// back down, never by new content arriving.
func TestScrollPositionStableWhileResponseStreams(t *testing.T) {
	m := compactCmdModel()
	m.showThinking = true
	m.busy = true
	m.Update(mkWinSize(80, 12))

	for i := range 10 {
		um, _ := m.Update(thinkMsg("reasoning line " + string(rune('0'+i)) + "\n"))
		m = um.(*model)
	}
	m.layout()
	m.vp.GotoTop()
	m.follow = false
	before := m.viewportView()
	if !strings.Contains(before, "reasoning line 0") {
		t.Fatalf("setup: top should show first reasoning line:\n%s", before)
	}

	// Response streams below the viewport.
	um, _ := m.Update(textMsg("response begins\n"))
	m = um.(*model)
	m.layout()

	after := m.viewportView()
	if m.follow {
		t.Error("streamed response re-engaged follow mode while scrolled up")
	}
	if !strings.Contains(after, "reasoning line 0") {
		t.Errorf("response streaming shifted the scrolled view:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// PgUp through a thinking+response transcript must move through ALL content,
// not jump past the response to the reasoning.
func TestPgUpMovesThroughWholeTranscript(t *testing.T) {
	m := compactCmdModel()
	m.showThinking = true
	m.Update(mkWinSize(80, 12))

	for range 8 {
		um, _ := m.Update(thinkMsg("thinking block line\n"))
		m = um.(*model)
	}
	for i := range 20 {
		um, _ := m.Update(textMsg("answer part " + string(rune('a'+i)) + "\n"))
		m = um.(*model)
	}
	m.flushThink()
	m.flushCurrent()
	m.layout()
	m.vp.GotoBottom()

	// PgUp once: the visible window should still contain answer content
	// (the lines just above the fold), not skip straight to thinking.
	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	m = um.(*model)
	v := m.viewportView()
	if !strings.Contains(v, "answer part") {
		t.Errorf("after one PgUp the answer is gone — jumped straight past it:\n%s", v)
	}
}
