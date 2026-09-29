package tui

import tea "charm.land/bubbletea/v2"

func (m *nativeModel) openMenu(kind string) tea.Cmd {
	if !m.navigationAllowed() {
		return nil
	}
	m.closeMenu()
	m.menu = newNativeMenu(&m.work, m.connection, nativeMenuOptions{Kind: kind, Owner: &m.owner, PreferencesDirectory: m.preferencesDirectory})
	m.input.Reset()
	return m.menu.Init()
}

func (m *nativeModel) updateMenu(message tea.Msg) tea.Cmd {
	menu := m.menu
	command := menu.Update(message)
	if owner := menu.Owner(); owner != nil && owner.ID == m.owner.ID && owner.ConfigRevision >= m.owner.ConfigRevision {
		m.owner = *owner
	}
	if menu.options.Kind == "settings" || menu.options.Kind == "theme" {
		m.preferences = menu.Preferences()
		m.showReasoning = nativePreferenceLabel(m.preferences.Thinking, true) == "on"
	}
	if menu.Done() {
		m.menu = nil
	}
	m.refresh() // Includes theme preview or restoration without touching history.
	return command
}

func (m *nativeModel) closeMenu() {
	if m.menu != nil {
		m.menu.Close()
		m.menu = nil
	}
}

func (m *nativeModel) close() {
	m.closeMenu()
	m.work.close()
	if m.recovery != nil {
		_ = m.recovery.root.Close()
	}
}
