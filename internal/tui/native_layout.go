package tui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/tui/ui"
)

func (m *nativeModel) sidebarVisible() bool {
	return m.width >= 120 && nativePreferenceLabel(m.preferences.Sidebar, true) == "on" && (!m.replVisible() || m.width >= 150)
}

func (m *nativeModel) replVisible() bool {
	return m.width >= 120 && nativePreferenceLabel(m.preferences.Repl, false) == "on"
}

func (m *nativeModel) replWidth() int {
	if !m.replVisible() {
		return 0
	}
	width := m.width
	if m.sidebarVisible() {
		width -= 44
	}
	return width / 2
}

func (m *nativeModel) transcriptWidth() int {
	width := m.width
	if m.sidebarVisible() {
		width -= 44
	}
	if m.replVisible() {
		width -= m.replWidth() + 1
	}
	return max(width, 1)
}

func (m *nativeModel) dockHeight() int {
	if m.sidebarVisible() || !m.dock {
		return 0
	}
	return min(8, max(m.height-9, 0))
}
func (m *nativeModel) agentsVisible() bool { return m.sidebarVisible() || m.dock || m.agentsFocus }

func (m *nativeModel) agentRows(width, height int) string {
	if m.agents == nil {
		return "Loading agent tree…"
	}
	lo, hi := nativeAgentWindow(m.agents.rows, m.agentSelection, max(height-2, 1))
	rows := make([]string, 0, hi-lo+2)
	th := currentTheme()
	for i := lo; i < hi; i++ {
		row := m.agents.rows[i]
		name := row.name + " · " + string(row.id)
		rows = append(rows, ui.ListRow{Badge: nativeAgentState(row), BadgeColor: th.Muted, Label: nativeDisplayText(name), Depth: min(row.depth, 4), Selected: m.agentsFocus && row.id == m.agentSelection, Open: row.id == m.owner.ID, Width: width}.Render(th, nil))
	}
	if lo > 0 || hi < len(m.agents.rows) {
		rows = append(rows, fmt.Sprintf("Rows %d–%d of %d · arrows scroll", lo+1, hi, len(m.agents.rows)))
	}
	if m.agents.partial {
		rows = append(rows, "Partial tree · /resume <exact ID>")
	}
	return strings.Join(rows, "\n")
}

func (m *nativeModel) layoutFrame(main string) string {
	if m.replVisible() {
		panel := ui.Panel{Title: "REPL", Key: "ctrl+r", Width: m.replWidth(), Height: m.height, Focused: m.replFocused}
		main = lipgloss.JoinHorizontal(lipgloss.Top, main, " ", panel.Render(currentTheme(), m.replVP.View()))
	}
	if m.sidebarVisible() {
		agentsHeight := max(m.height-6, 5)
		panel := ui.Panel{Title: "Agents", Key: "ctrl+t", Width: 42, Height: agentsHeight, Focused: m.agentsFocus, Band: true}
		left := panel.Render(currentTheme(), m.agentRows(panel.Inner(currentTheme())+2, max(agentsHeight-4, 1)))
		contextPanel := ui.Panel{Title: "Context", Width: 42, Height: max(m.height-agentsHeight, 0)}
		left += "\n" + contextPanel.Render(currentTheme(), nativeContextLabel(m.contextUsage))
		main = lipgloss.JoinHorizontal(lipgloss.Top, left, "  ", main)
	}
	rows := strings.Split(main, "\n")
	for i := range rows {
		rows[i] = ansi.Truncate(rows[i], m.width, "")
	}
	if len(rows) > m.height {
		rows = rows[:m.height]
	}
	return strings.Join(rows, "\n")
}

func (m *nativeModel) layoutCommand(name string) tea.Cmd {
	if name == "dock" {
		m.dock = !m.dock
		m.polls = 0
		m.input.Reset()
		m.refresh()
		return nil
	}
	enabled := nativePreferenceLabel(m.preferences.Sidebar, true) != "on"
	value, err := updateNativePreferences(m.preferencesDirectory, func(p *nativePreferences) { p.Sidebar = new(enabled) })
	if err != nil {
		m.status = "Sidebar preference: " + err.Error()
		return nil
	}
	m.preferences = value
	m.polls = 0
	m.input.Reset()
	m.refresh()
	return nil
}

// Draft text stays on its original owner when opening another transcript. This
// client-local cache is bounded and never injected into history or resubmitted.
func (m *nativeModel) switchDraft(owner protocol.ID) (string, error) {
	current := m.input.Value()
	if strings.HasPrefix(strings.TrimSpace(current), "/") {
		current = ""
	}
	bytes := len(current)
	count := 0
	for id, text := range m.drafts {
		if id != m.owner.ID {
			bytes += len(text)
			if text != "" {
				count++
			}
		}
	}
	if current != "" {
		count++
	}
	if len(current) > 256<<10 || bytes > 1<<20 || count > 16 {
		return "", errors.New("local draft capacity reached; save or clear a draft before switching owners")
	}
	next := m.drafts[owner]
	if m.drafts == nil {
		m.drafts = map[protocol.ID]string{}
	}
	if current == "" {
		delete(m.drafts, m.owner.ID)
	} else {
		m.drafts[m.owner.ID] = current
	}
	delete(m.drafts, owner)
	if owner == m.owner.ID {
		return current, nil
	}
	return next, nil
}

func nativeFixedRows(value string, width, height int) string {
	rows := strings.Split(value, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	rows = rows[:height]
	for i := range rows {
		rows[i] = ansi.Truncate(rows[i], width, "")
	}
	return strings.Join(rows, "\n")
}
