package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// keyRunes builds the KeyMsg bubbletea would produce for an unknown sequence
// whose String() renders as s.
func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func TestIsShiftEnterSeq(t *testing.T) {
	for in, want := range map[string]bool{
		// rendered forms of unknownCSISequenceMsg (see bubbletea key.go)
		"unknown csi sequence: 0x1b, '[', '1', '3', ';', '2', 'u'":                     true,  // CSI u
		"unknown csi sequence: 0x1b, '[', '2', '7', ';', '2', ';', '1', '3', '~'":      true,  // modifyOtherKeys
		"unknown csi sequence: 0x1b, '[', 'five', 'seven', 'four', 'four', 'one', 'u'": true,  // kitty 57441u
		"unknown csi sequence: 0x1b, '[', '1', ';', '2', 'A'":                          false, // shift+up
		// current bubbletea v1.3.10 form: ?CSI[decimal byte values]?
		"?CSI[49 51 59 50 117]?":          true,  // CSI 13;2u
		"?CSI[50 55 59 50 59 49 51 126]?": true,  // CSI 27;2;13~
		"?CSI[53 55 52 52 49 117]?":       true,  // CSI 57441u
		"?CSI[49 59 50 65]?":              false, // shift+up (CSI 1;2A)
		"a":                               false,
		"enter":                           false,
	} {
		if got := isShiftEnterSeq(keyRunes(in)); got != want {
			t.Errorf("isShiftEnterSeq(%q) = %v, want %v", in, got, want)
		}
	}
}

// ctrl+e with no tool result blocks falls through to the textarea, where
// bubbles binds it to cursor-to-line-end — readline behavior users expect
// while typing (Ruslan: "ctrl-e/a"). With a tool block present, ctrl+e keeps
// its whip binding (expand/collapse the most recent tool result).
func TestCtrlEFallsThroughToTextarea(t *testing.T) {
	m := compactCmdModel()
	m.input.SetValue("hello world")
	m.input.CursorStart()

	// no tool blocks: ctrl+e is line-end, not a no-op
	tm, _ := m.key(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = tm.(*model)
	if got := m.input.LineInfo().CharOffset; got != len("hello world") {
		t.Fatalf("ctrl+e with no tool blocks should go to line end, char offset = %d", got)
	}

	// a tool result block reclaims ctrl+e for expand/collapse
	m.blocks = append(m.blocks, block{kind: blockTool, text: "ran a thing"})
	m.input.CursorStart()
	tm, _ = m.key(tea.KeyMsg{Type: tea.KeyCtrlE})
	m = tm.(*model)
	if !m.blocks[len(m.blocks)-1].expanded {
		t.Fatal("ctrl+e with a tool block should expand it")
	}
	if got := m.input.LineInfo().CharOffset; got != 0 {
		t.Fatalf("ctrl+e consumed by the block toggle must not move the cursor, offset = %d", got)
	}
}

// ctrl+a was never intercepted: it reaches the textarea's LineStart binding.
func TestCtrlAGoesToLineStart(t *testing.T) {
	m := compactCmdModel()
	m.input.SetValue("hello world")
	m.input.CursorEnd()
	tm, _ := m.key(tea.KeyMsg{Type: tea.KeyCtrlA})
	m = tm.(*model)
	if got := m.input.LineInfo().CharOffset; got != 0 {
		t.Fatalf("ctrl+a should go to line start, char offset = %d", got)
	}
}

// Keyboard enhancement is what makes shift+enter distinguishable at all:
// without the kitty flags push, the terminal (or tmux with extended-keys
// off) reports shift+enter as a plain CR and whip can never see it. Pin the
// escape sequence and that Run pushes/pops it.
func TestKeyboardEnhancementEscapes(t *testing.T) {
	t.Setenv("TMUX", "") // exercise the non-tmux path deterministically
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	enableKeyboardEnhancement(w)
	disableKeyboardEnhancement(w)
	w.Close()
	buf := make([]byte, 64)
	_ = r.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := r.Read(buf)
	got := string(buf[:n])
	if !strings.Contains(got, "\x1b[>1u") {
		t.Errorf("must push kitty disambiguate flag \\x1b[>1u, got %q", got)
	}
	if !strings.Contains(got, "\x1b[<u") {
		t.Errorf("must pop the keyboard stack \\x1b[<u, got %q", got)
	}
}

// Inside tmux the kitty push/pop must be DCS-passthrough-wrapped so it reaches
// the outer terminal (a pane's bare escape is interpreted by tmux itself).
func TestTmuxPassthrough(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	got := tmuxPassthrough("\x1b[>1u")
	want := "\x1bPtmux;\x1b\x1b[>1u\x1b\\"
	if got != want {
		t.Errorf("tmux passthrough = %q, want %q", got, want)
	}
}

// Outside tmux (no TMUX, no tmux/screen TERM) the sequence is unchanged.
func TestTmuxPassthroughOutsideTmux(t *testing.T) {
	t.Setenv("TMUX", "")
	t.Setenv("TERM", "xterm-256color")
	if got := tmuxPassthrough("\x1b[>1u"); got != "\x1b[>1u" {
		t.Errorf("outside tmux the sequence passes through unchanged, got %q", got)
	}
}

// Run must push the enhancement before p.Run() in inline mode (no ?1049h, so
// it survives) and pop after — and opencode mode must route the push through
// the post-altscreen Init handler instead (its per-screen kitty stack means a
// pre-Run push never lands) — a source-level pin guarding the wiring.
func TestKeyboardEnhancementWiredIntoRun(t *testing.T) {
	src, err := os.ReadFile("tui.go")
	if err != nil {
		t.Fatal(err)
	}
	// inline-mode push before p.Run(), pop after.
	if !strings.Contains(string(src), "enableKeyboardEnhancement(os.Stdout)") {
		t.Error("Run must push keyboard enhancement at startup (inline mode)")
	}
	if !strings.Contains(string(src), "disableKeyboardEnhancement(os.Stdout)") {
		t.Error("Run must pop keyboard enhancement on exit (inline mode)")
	}
	// opencode mode: the push is unconditionally scheduled via the Init/msg
	// path (post-altscreen), not just when the mouse is on.
	if !strings.Contains(string(src), "terminalInitMsg{}") {
		t.Error("opencode mode must schedule the post-altscreen terminal init push")
	}
}
