package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
		if fields[1] != "light" && fields[1] != "dark" && fields[1] != "auto" {
			m.append(errStyle.Render("usage: /theme light|dark|auto"))
			return m, nil
		}
		m.setTheme(fields[1])
	case "/mouse":
		m.mouseOn = !m.mouseOn
		enabled := m.mouseOn
		m.cfg.Mouse = &enabled
		if err := m.cfg.Save(); err != nil {
			m.append(errStyle.Render("config save failed: " + err.Error()))
			return m, nil
		}
		if enabled {
			enableClickWheelMouse(os.Stdout)
		} else {
			disableClickWheelMouse(os.Stdout)
		}
		m.append(dimStyle.Render("mouse capture: " + onOff(enabled)))
	case "/export":
		m.exportCommand(args)
	case "/report":
		m.append(m.reportBlock())
	default:
		m.append(errStyle.Render(fmt.Sprintf("unknown command: %s", fields[0])))
	}
	return m, nil
}
