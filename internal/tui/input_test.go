package tui

import tea "charm.land/bubbletea/v2"

// keyRunes builds the key press Bubble Tea produces for typed text s (a
// single printable character, or several for a pasted-looking sequence).
func keyRunes(s string) tea.KeyPressMsg {
	msg := tea.KeyPressMsg{Text: s}
	if r := []rune(s); len(r) == 1 {
		msg.Code = r[0]
	}
	return msg
}
