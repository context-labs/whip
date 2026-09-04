package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// command handles UI-local commands only. Everything that can affect an
// agent, transcript, workspace, tool, or process is sent to the daemon by
// thinCommand.
func (m *model) command(text string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return m, nil
	}
	args := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	switch fields[0] {
	case "/quit", "/exit", "/q":
		return m, tea.Quit
	case "/help":
		m.append(dimStyle.Render(helpText()))
	case "/theme":
		if len(fields) == 1 {
			m.openThinThemePalette()
			return m, nil
		}
		var problems []string
		for _, err := range loadUserThemes() {
			problems = append(problems, err.Error())
		}
		if !knownThemeName(fields[1]) {
			problems = append(problems, "usage: /theme "+strings.Join(themeNames(), "|")+" (user themes: ~/.whip/themes/<name>.json)")
		}
		if len(problems) > 0 {
			return m, m.toastError(strings.Join(problems, " · "))
		}
		m.setTheme(fields[1])
	case "/mouse":
		m.mouseOn = !m.mouseOn
		enabled := m.mouseOn
		m.cfg.Mouse = &enabled
		if err := m.cfg.Save(); err != nil {
			return m, m.toastError("config save failed: " + err.Error())
		}
		m.append(dimStyle.Render("mouse capture: " + onOff(enabled))) // View() reflects m.mouseOn
	case "/export":
		m.exportCommand(args)
	case "/report":
		m.append(m.reportBlock())
	default:
		return m, m.toastError(fmt.Sprintf("unknown command: %s", fields[0]))
	}
	return m, nil
}
