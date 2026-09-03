package tui

import (
	"fmt"
	"testing"
)

// unknownCSI mimics bubbletea's unexported unknownCSISequenceMsg: a named
// []byte type whose String() renders "?CSI[<bytes after ESC[]>?". whip's Update
// must catch it (it's NOT a tea.KeyMsg, so it never reaches key()).
type unknownCSI []byte

func (u unknownCSI) String() string { return fmt.Sprintf("?CSI%+v?", []byte(u)[2:]) }

// The production path: shift+enter arrives as an unknownCSISequenceMsg through
// Update (not key()), and must insert a newline — not submit, not be dropped.
// Regression test for "shift+enter still does not work": the isShiftEnterSeq
// branch in key() was dead code because bubbletea never sends a KeyMsg for it.
func TestShiftEnterViaUnknownCSIInsertsNewline(t *testing.T) {
	m := compactCmdModel()
	m.input.SetValue("aaa")

	for _, seq := range []string{
		"\x1b[13;2u",    // kitty CSI-u
		"\x1b[27;2;13~", // xterm/modifyOtherKeys (tmux extended-keys on, xterm fmt)
		"\x1b[57441u",   // kitty shifted CR
	} {
		m.input.SetValue("aaa")
		m.input.CursorEnd()
		tm, _ := m.Update(unknownCSI(seq))
		m = tm.(*model)
		if got := m.input.Value(); got != "aaa\n" {
			t.Errorf("Update(%q): want input %q, got %q", seq, "aaa\n", got)
		}
	}

	// a non-shift+enter unknown CSI (shift+up = ESC[1;2A) must NOT newline
	m.input.SetValue("aaa")
	tm, _ := m.Update(unknownCSI("\x1b[1;2A"))
	m = tm.(*model)
	if got := m.input.Value(); got != "aaa" {
		t.Errorf("shift+up must not insert a newline, got %q", got)
	}
}
