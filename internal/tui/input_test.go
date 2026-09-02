package tui

import (
	"testing"

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
		"a":     false,
		"enter": false,
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
