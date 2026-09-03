package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// interactive is a rendered view of a daemon-owned terminal process.
type interactive struct {
	output  string
	await   bool
	awaitcd int
}

func arrowBytes(key tea.KeyType) string {
	switch key {
	case tea.KeyUp:
		return "\x1b[A"
	case tea.KeyDown:
		return "\x1b[B"
	case tea.KeyRight:
		return "\x1b[C"
	case tea.KeyLeft:
		return "\x1b[D"
	default:
		return ""
	}
}

func (m *model) interactiveView() string {
	if m.iactive == nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(m.iactive.output, "\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	header := toolStyle.Render("⚒ bash (interactive)")
	if m.iactive.await {
		header += errStyle.Render(fmt.Sprintf("  ⏳ waiting for input — cancels in %ds", m.iactive.awaitcd))
	} else {
		header += dimStyle.Render("  (type to respond; ctrl+c ctrl+c to cancel)")
	}
	return header + "\n" + dimStyle.Render("  "+strings.Join(lines, "\n  "))
}
