package tui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Regression for the broken-markdown-streaming bug: while a response streamed,
// layout() reserved the FULL streamCap for the in-flight live area whenever
// m.current != "", clamping the transcript viewport to minTranscriptRows (5)
// even though the partial line was ~1 row — then dropped the reservation to 0
// the instant a newline folded the line out of m.current. vpH oscillated
// between the clamped floor and the full height (a ~16-row jump) several times
// per turn, so the transcript visibly thrashed between "full chat" and "tail
// only" on every newline. The fix budgets the live area's ACTUAL rendered
// height (capped at streamCap), so vpH tracks the 1–2 row partial instead of
// the 16-row max and stays stable while text streams.
func TestStreamViewportHeightStable(t *testing.T) {
	for _, mode := range []string{"default", "opencode"} {
		t.Run(mode, func(t *testing.T) {
			runStreamViewportStable(t, mode == "opencode")
		})
	}
}

func runStreamViewportStable(t *testing.T, oc bool) {
	t.Helper()
	m := compactCmdModel()
	if oc {
		m.applyUIMode(opencodeMode)
		t.Cleanup(func() { m.applyUIMode("") })
	}
	m.Update(mkWinSize(80, 30))
	m.busy = true
	m.turnStart = m.nowFn()
	// Pre-fill the transcript so "the full chat" is taller than the viewport.
	for i := 0; i < 15; i++ {
		m.appendAssistant("prior turn line " + itoa(i) + " with some words to be real")
	}
	m.layout()
	m.View()

	// Stream a multi-line response token by token (the real SSE cadence: small
	// deltas that do NOT align to line boundaries, so m.current is non-empty
	// between newlines and empty right after a line folds).
	full := "## Root cause\n\n- item one with words here\n- item two with words\n- item three\n- item four\n\nThat is the plan.\n"
	tokens := tokenize(full)

	type snap struct {
		step string
		vpH  int
	}
	var snaps []snap
	snapIt := func(step string) {
		m.layout()
		m.View() // View mutates viewTop/frameH; call so the inline path is exercised
		snaps = append(snaps, snap{step: step, vpH: m.vp.Height})
	}

	snapIt("start")
	for i, tok := range tokens {
		um, _ := m.Update(textMsg(tok))
		m = um.(*model)
		snapIt("tok:" + itoa(i))
	}
	um, _ := m.Update(turnDoneMsg{final: full})
	m = um.(*model)
	snapIt("turnDone")

	for _, s := range snaps {
		t.Logf("%-14s vpH=%d", s.step, s.vpH)
	}

	// During streaming the viewport height must NOT oscillate by more than a
	// couple rows (the partial line's own height). The old bug swung vpH
	// between minTranscriptRows (5) and the full height (21) — a 16-row jump.
	streamSnaps := snaps[1 : len(snaps)-1]
	minH, maxH := streamSnaps[0].vpH, streamSnaps[0].vpH
	for _, s := range streamSnaps {
		if s.vpH < minH {
			minH = s.vpH
		}
		if s.vpH > maxH {
			maxH = s.vpH
		}
	}
	swing := maxH - minH
	if swing > 3 {
		t.Errorf("viewport height oscillated %d rows during streaming (maxH=%d minH=%d); want ≤3 (the partial line's height, not the full streamCap)", swing, maxH, minH)
	}

	// And the clamped floor must never collapse the transcript to the
	// minTranscriptRows tail — the full chat must stay visible. The viewport is
	// bottom-anchored; with a tall transcript it shows the newest window. The
	// old bug reserved the full cap and dropped vpH to 5.
	if minH <= minTranscriptRows {
		t.Errorf("viewport collapsed to %d rows (≤ minTranscriptRows=%d) during streaming; the full chat was replaced by the tail", minH, minTranscriptRows)
	}

	// Sanity: the whole frame fits the terminal at every step (no overflow that
	// would scroll the top into scrollback).
	for _, s := range snaps {
		if s.vpH > m.height {
			t.Errorf("%s: viewport %d rows exceeds terminal %d", s.step, s.vpH, m.height)
		}
	}
	_ = ansi.Strip // keep ansi import even if assertions evolve
}

// tokenize splits s into small streaming deltas (~2–5 chars), the way SSE
// chunks arrive — crucially NOT aligned to line boundaries.
func tokenize(s string) []string {
	var toks []string
	for i := 0; i < len(s); {
		n := 2 + (i % 4) // 2..5 chars
		if i+n > len(s) {
			n = len(s) - i
		}
		toks = append(toks, s[i:i+n])
		i += n
	}
	return toks
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
