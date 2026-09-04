package tui

import tea "charm.land/bubbletea/v2"

// pickerKey navigates daemon-returned session metadata and requests a bounded
// preview as the selection changes. Opening still switches to the daemon's
// complete authoritative snapshot.
func (m *model) pickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	picker := m.picker
	if picker == nil || len(picker.metas) == 0 {
		m.picker = nil
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.picker = nil
	case "up", "ctrl+p", "shift+tab":
		picker.idx = min(picker.idx+1, len(picker.metas)-1)
		if _, ok := picker.previews[picker.metas[picker.idx].ID]; !ok {
			return m.submitClientAction("session.preview", map[string]string{"id": picker.metas[picker.idx].ID}, "")
		}
	case "down", "ctrl+n", "tab":
		picker.idx = max(picker.idx-1, 0)
		if _, ok := picker.previews[picker.metas[picker.idx].ID]; !ok {
			return m.submitClientAction("session.preview", map[string]string{"id": picker.metas[picker.idx].ID}, "")
		}
	case "enter":
		id := picker.metas[picker.idx].ID
		m.picker = nil
		return m.submitClientAction("session.open", map[string]string{"args": id}, "")
	}
	return m, nil
}
