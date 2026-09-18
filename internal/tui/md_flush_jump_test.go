package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// Overlays (toast, completion popup) must stay anchored to terminal geometry
// while a response streams — the toast pinned top-right, the completion popup
// above the input box — instead of riding the transcript content as it grows.
// (An earlier bottom-anchor fix prepended the lead AFTER splicing overlays,
// which let them ride the content down; that is still guarded here.) The
// streaming view now fills the terminal (the old streamCap clamp that
// collapsed the viewport to minTranscriptRows was the broken-streaming bug
// fixed by liveAreaRows), so we verify anchoring under the corrected,
// full-height streaming view rather than the obsolete short-view condition.
func TestOpencodeOverlaysAnchoredDuringStreaming(t *testing.T) {
	m := compactCmdModel()
	m.applyUIMode(opencodeMode)
	t.Cleanup(func() { m.applyUIMode("") })
	m.Update(mkWinSize(80, 24))

	// A streaming turn: committed lines plus an in-flight partial. The view
	// fills the terminal (viewH == height, lead 0) — the realistic state while
	// markdown streams, and the one where overlays must not ride the content.
	m.busy = true
	m.turnStart = m.nowFn()
	m.appendAssistant("a short committed line")
	m.current = "streaming tail text still in flight"
	m.layout()
	m.View()
	lead := max(m.height-m.viewH, 0)
	if m.viewTop != lead {
		t.Fatalf("viewTop should be lead=%d, got %d", lead, m.viewTop)
	}

	// Toast: ocSpliceToast paints at frame row y=2 (pad row 2, text row 3).
	// It must stay at terminal row 2-3, not ride the content.
	m.toast = "Copied to clipboard"
	m.layout()
	v := m.View()
	lines := strings.Split(v, "\n")
	toastRow := -1
	for i, l := range lines {
		if strings.Contains(ansi.Strip(l), "Copied to clipboard") {
			toastRow = i
			break
		}
	}
	if toastRow < 0 {
		t.Fatalf("toast not rendered:\n%s", v)
	}
	// text row is y+1 = 3 in ocSpliceToast's [pad, mid, pad] slice.
	if toastRow != 3 {
		t.Errorf("toast not at terminal top-right: row %d, want 3; lead=%d\n%s", toastRow, lead, v)
	}

	// Completion popup: bottom-anchored to the row just above the input box.
	// Its screen row is viewTop + inputBodyOff - len(rows); it must sit above
	// the INPUT, not float elsewhere.
	m.toast = ""
	m.menu = &menu{cands: []cand{{Text: "/cd", Desc: "change dir"}}}
	m.layout()
	v = m.View()
	menuRows := strings.Split(m.menuView(), "\n")
	wantTop := max(m.viewTop+m.inputBodyOff-len(menuRows), 0)
	lines = strings.Split(v, "\n")
	found := false
	for i := wantTop; i < wantTop+len(menuRows) && i < len(lines); i++ {
		if strings.Contains(ansi.Strip(lines[i]), "/cd") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("completion popup not anchored above input: want at row %d (viewTop=%d inputBodyOff=%d), lead=%d\n%s", wantTop, m.viewTop, m.inputBodyOff, lead, v)
	}
}
