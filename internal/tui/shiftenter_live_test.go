package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

// The kitty disambiguate push makes terminals report modified ASCII keys as
// CSI u, which bubbletea v1 drops as unknown CSI — so ctrl+a/e/c were dead
// outside tmux. csiUKey must turn them back into the legacy KeyMsgs.
func TestCSIUKeysDecodeToLegacyKeys(t *testing.T) {
	cases := map[string]tea.KeyMsg{
		"\x1b[97;5u":    {Type: tea.KeyCtrlA},
		"\x1b[101;5u":   {Type: tea.KeyCtrlE},
		"\x1b[99;5u":    {Type: tea.KeyCtrlC},
		"\x1b[27u":      {Type: tea.KeyEsc},
		"\x1b[120;3u":   {Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true},
		"\x1b[97:65;2u": {Type: tea.KeyRunes, Runes: []rune{'A'}},
		"\x1b[99;7u":    {Type: tea.KeyCtrlC, Alt: true},
		"\x1b[32;2u":    {Type: tea.KeySpace},
	}
	for seq, want := range cases {
		got, ok := csiUKey(unknownCSI(seq).String())
		if !ok || got.String() != want.String() || got.Alt != want.Alt {
			t.Errorf("%q: got %v (ok=%v), want %v", seq, got, ok, want)
		}
	}
	for _, seq := range []string{"\x1b[1;2A", "\x1b[57441u", "\x1b[200~"} {
		if _, ok := csiUKey(unknownCSI(seq).String()); ok {
			t.Errorf("%q must not decode as a CSI-u ASCII key", seq)
		}
	}

	// end to end through Update: ctrl+a then ctrl+e move the cursor
	m := compactCmdModel()
	m.input.SetValue("abc")
	m.input.CursorEnd()
	tm, _ := m.Update(unknownCSI("\x1b[97;5u"))
	m = tm.(*model)
	m.input.InsertString("X")
	tm, _ = m.Update(unknownCSI("\x1b[101;5u"))
	m = tm.(*model)
	m.input.InsertString("Y")
	if got := m.input.Value(); got != "XabcY" {
		t.Errorf("ctrl+a/ctrl+e via CSI u: want %q, got %q", "XabcY", got)
	}
}
