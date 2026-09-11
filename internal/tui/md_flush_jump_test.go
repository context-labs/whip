package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Reproduction for the residual "jump" the user still sees after the inline
// frame fix: when the assistant's final markdown render lands, the visible UI
// shifts. This drives a markdown-heavy turn through the real streaming path
// (textMsg per newline, then turnDoneMsg) and records the frame geometry at
// every step so the exact row that jumps is visible in the test output.
func TestMarkdownFlushDoesNotJump(t *testing.T) {
	for _, mode := range []string{"default", "opencode"} {
		t.Run(mode, func(t *testing.T) {
			runMarkdownFlushJump(t, mode == "opencode")
		})
	}
}

func runMarkdownFlushJump(t *testing.T, oc bool) {
	t.Helper()
	m := compactCmdModel()
	if oc {
		m.applyUIMode(opencodeMode)
		t.Cleanup(func() { m.applyUIMode("") })
	}
	m.Update(mkWinSize(80, 24))
	m.busy = true
	m.turnStart = m.nowFn()
	m.layout()
	m.View()

	// A markdown-heavy assistant turn: heading, paragraph, list, code fence.
	chunks := []string{
		"## Fixing the jump\n",
		"\n",
		"Here is a paragraph of assistant text that wraps a bit at width 80.\n",
		"\n",
		"- first item with some words\n",
		"- second item with more words in it\n",
		"- third\n",
		"\n",
		"```go\n",
		"func main() {\n",
		"\tfmt.Println(\"hi\")\n",
		"}\n",
		"```\n",
		"\n",
		"That is the plan.\n",
	}

	type snap struct {
		step             string
		viewTop, viewH   int
		frameTop, frameH int
		inputTop         int
		screenH          int
	}
	var snaps []snap
	snapIt := func(step string) {
		m.layout()
		v := m.View()
		s := snap{step: step, viewTop: m.viewTop, viewH: m.viewH, frameTop: m.frameTop, frameH: m.frameH, inputTop: m.inputTop, screenH: lipgloss.Height(v)}
		snaps = append(snaps, s)
	}

	snapIt("start")
	for _, c := range chunks {
		um, _ := m.Update(textMsg(c))
		m = um.(*model)
		snapIt("stream:" + strings.TrimSpace(c))
	}
	// Partial tail still in m.current (no trailing newline on the last flush):
	um, _ := m.Update(textMsg("final trailing bit"))
	m = um.(*model)
	snapIt("stream:tail")

	// Turn ends: flushCurrent finalizes the merged block into one markdown doc,
	// busy clears, chrome shrinks back to base.
	um, _ = m.Update(turnDoneMsg{final: "done"})
	m = um.(*model)
	snapIt("turnDone")

	// One more idle render (the next View() after busy clears).
	m.layout()
	m.View()
	snapIt("idle")

	// Find the snapshots bracketing the flush (stream:tail -> turnDone) and the
	// tallest streaming frame so a failure explains which row moved and when.
	var pre, post snap
	for i, s := range snaps {
		if s.step == "turnDone" {
			pre, post = snaps[i-1], snaps[i]
			break
		}
	}
	t.Logf("stream:tail inputTop=%d viewTop=%d viewH=%d -> turnDone inputTop=%d viewTop=%d viewH=%d",
		pre.inputTop, pre.viewTop, pre.viewH, post.inputTop, post.viewTop, post.viewH)

	// Invariant: the input box's absolute screen row must not move when the
	// final markdown render lands — that is the visible "jump". The streaming
	// tail and the busy chrome are transient; the committed transcript + input
	// position must be stable across turnDone.
	if pre.inputTop != post.inputTop {
		t.Errorf("input box jumped across final markdown flush: %d -> %d", pre.inputTop, post.inputTop)
	}
	// And the physical screen must stay bottom-anchored (content bottom == terminal bottom).
	if post.viewTop+post.viewH != m.height {
		t.Errorf("after flush, frame not bottom-anchored: bottom %d != terminal %d", post.viewTop+post.viewH, m.height)
	}
	// Frame must never exceed the terminal (overflow scrolls the top away).
	for _, s := range snaps {
		if s.screenH > m.height {
			t.Errorf("step %q: frame %d rows exceeds terminal %d", s.step, s.screenH, m.height)
		}
	}
}
