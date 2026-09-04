package tui

import tea "charm.land/bubbletea/v2"

// dialog is a floating panel drawn over the dimmed session. The open dialogs
// form a z-stack DERIVED from the model's nullable fields each time it is
// asked for (no push/pop, no second source of truth): one fixed order decides
// both what is drawn on top and who owns the keyboard, which used to be two
// disagreeing lists.
type dialog interface {
	key(m *model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd)
	rows(m *model) []string // rendered rows; View centers them in the upper third
}

func (p *palette) key(m *model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m.paletteKey(msg) }
func (p *palette) rows(m *model) []string                                 { return m.ocDialogRows() }

func (a *msgActions) key(m *model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m.msgActionsKey(msg)
}
func (a *msgActions) rows(m *model) []string { return m.ocMsgActionRows() }

func (p *modelPicker) key(m *model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	return m.modelPickerKey(msg)
}
func (p *modelPicker) rows(m *model) []string { return m.ocModelDialogRows() }

func (p *picker) key(m *model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) { return m.pickerKey(msg) }
func (p *picker) rows(m *model) []string                                 { return m.ocSessionDialogRows() }

// dialogs returns the open dialogs bottom→top: the session picker, the model
// picker, Message Actions, then the command palette (the palette can open
// the others, so it stays on top). Inline modes — the permission prompt, the
// rewind picker, the name prompt, an interactive command — are body regions,
// not dialogs; the completion menu is a popup that leaves the keyboard to
// the textarea.
func (m *model) dialogs() []dialog {
	var ds []dialog
	if m.picker != nil {
		ds = append(ds, m.picker)
	}
	if m.mpicker != nil {
		ds = append(ds, m.mpicker)
	}
	if m.msgActions != nil {
		ds = append(ds, m.msgActions)
	}
	if m.palette != nil {
		ds = append(ds, m.palette)
	}
	return ds
}

// topDialog is the dialog that owns the keyboard, nil when none is open.
func (m *model) topDialog() dialog {
	ds := m.dialogs()
	if len(ds) == 0 {
		return nil
	}
	return ds[len(ds)-1]
}

// floatingOpen reports whether a dialog is drawn over the transcript: presses
// and the wheel must not reach what it covers. (The completion menu is not a
// dialog: keys fall through to the textarea, but presses are swallowed by the
// callers that check m.menu.) The inline modes — permission prompt, rewind
// picker — leave the transcript fully visible, so it stays scrollable and
// clickable under them.
func (m *model) floatingOpen() bool { return len(m.dialogs()) > 0 }

// dialogOpen reports whether something other than the textarea owns the
// keyboard: a floating dialog or an inline mode. The terminal cursor hides.
func (m *model) dialogOpen() bool {
	return m.floatingOpen() || m.rew != nil || (m.permDialog != nil && m.permDialog.daemon != nil)
}
