package tui

import "strings"

type namePrompt struct {
	label string
	draft string
	mask  bool
	onOK  func(string)
}

func (m *model) openNamePrompt(label, value string, onOK func(string)) {
	m.namePrompt = &namePrompt{label: label, draft: m.input.Value(), onOK: onOK}
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.growInput()
}

func (m *model) closeNamePrompt() {
	m.input.SetValue(m.namePrompt.draft)
	m.input.CursorEnd()
	m.namePrompt = nil
	m.growInput()
}

func (prompt *namePrompt) maskedValue(value string) string {
	if !prompt.mask {
		return value
	}
	return strings.Repeat("•", len([]rune(value)))
}
