package tui

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyRunes builds the key press Bubble Tea produces for typed text s (a
// single printable character, or several for a pasted-looking sequence).
func keyRunes(s string) tea.KeyPressMsg {
	msg := tea.KeyPressMsg{Text: s}
	if r := []rune(s); len(r) == 1 {
		msg.Code = r[0]
	}
	return msg
}

// shift+enter is a newline, not a submit (the textarea binding plus the
// thinKey fast path that lifts MaxHeight so the newline always lands).
func TestShiftEnterInsertsNewline(t *testing.T) {
	m := newGrowModel()
	m.input.SetValue("aaa")
	m.input.CursorEnd()
	tm, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	m = tm.(*model)
	if got := m.input.Value(); got != "aaa\n" {
		t.Fatalf("shift+enter should insert a newline, got %q", got)
	}
}

// With no tool output to expand, ctrl+e falls through to the textarea's
// line-end binding; ctrl+a is never intercepted and goes to line-start.
func TestCtrlECtrlAMoveWithinTextarea(t *testing.T) {
	m := compactCmdModel()
	m.input.SetValue("hello world")
	m.input.SetCursorColumn(5)
	tm, _ := m.Update(ctrlKey('e'))
	m = tm.(*model)
	if got := m.input.Column(); got != len("hello world") {
		t.Fatalf("ctrl+e should move to line end: column %d", got)
	}
	tm, _ = m.Update(ctrlKey('a'))
	m = tm.(*model)
	if got := m.input.Column(); got != 0 {
		t.Fatalf("ctrl+a should move to line start: column %d", got)
	}
}

// Streamed content must not yank a user who scrolled up back to the bottom:
// every append site re-arms follow only when already following.
func TestAppendKeepsScrolledUpViewport(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 20))
	for range 40 {
		m.appendAssistant("line of transcript content that is long enough to matter")
	}
	m.vp.GotoBottom()
	um, _ := m.Update(wheelMsg(40, 10, true))
	m = um.(*model)
	if m.follow || m.vp.AtBottom() {
		t.Fatal("setup: wheel-up should leave the viewport scrolled up with follow off")
	}
	offset := m.vp.YOffset()

	m.appendAssistant("chunk one") // same message: the merge path
	m.inMsg = false
	m.appendAssistant("chunk two") // new block: appendRaw
	m.thinkStart, m.ocThink = m.nowFn(), "thought"
	m.flushThink()
	m.finishTool(toolEndMsg{id: "none", name: "shell", result: "ok"})

	if m.follow || m.vp.YOffset() != offset {
		t.Fatalf("appends must not yank: follow=%v offset %d -> %d", m.follow, offset, m.vp.YOffset())
	}
}

// The startup report warns (warn-only: whip never runs `tmux set`) when
// shift+enter cannot reach the pane: under mosh, or inside tmux with the
// server option extended-keys off.
func TestStartupReportShiftEnterWarnings(t *testing.T) {
	mosh, tmux := moshDetect, tmuxExtKeysCheck
	t.Cleanup(func() { moshDetect, tmuxExtKeysCheck = mosh, tmux })
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv("TMUX", "/tmp/tmux-501/default,1,0")

	moshDetect = func() bool { return false }
	tmuxExtKeysCheck = func() bool { return false }
	m := compactCmdModel()
	m.startupReport()
	if !strings.Contains(m.transcriptText(), "set -s extended-keys on") {
		t.Fatalf("tmux with extended-keys off should warn, got %q", m.transcriptText())
	}

	tmuxExtKeysCheck = func() bool { return true }
	m = compactCmdModel()
	m.startupReport()
	if strings.Contains(m.transcriptText(), "extended-keys") {
		t.Fatalf("tmux with extended-keys on must not warn, got %q", m.transcriptText())
	}

	moshDetect = func() bool { return true }
	m = compactCmdModel()
	m.startupReport()
	if !strings.Contains(m.transcriptText(), "over mosh") {
		t.Fatalf("mosh should warn, got %q", m.transcriptText())
	}
}

func TestProcHasAncestor(t *testing.T) {
	self, err := os.ReadFile("/proc/self/comm")
	if err != nil {
		t.Skip("/proc unavailable")
	}
	name := strings.TrimSpace(string(self))
	if !procHasAncestor(os.Getpid(), name) {
		t.Errorf("own process %q should be found walking its own chain", name)
	}
	if procHasAncestor(os.Getpid(), "no-such-proc-xyzzy") {
		t.Error("a nonexistent process name must not match")
	}
	if procHasAncestor(1, name) { // init's chain contains only init
		t.Error("walking from pid 1 must not find the test binary")
	}
}
