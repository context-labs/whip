package tui

import (
	"context"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/config"
)

// /me — open ~/.whip/me.md in $EDITOR. The file is appended to every
// session's system prompt (the built-in operating rules stay — they carry
// the safety rails), so this is the user's standing-instructions surface.
// tea.ExecProcess suspends the renderer for the edit, then resumes.
func (m *model) openMe() tea.Cmd {
	path := config.MePath()
	if path == "" {
		m.append(errStyle.Render("/me: cannot locate ~/.whip"))
		return nil
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	m.append(dimStyle.Render("editing " + path + " — save and quit to apply (next turn picks it up)"))
	//nolint:gosec // G702: $VISUAL/$EDITOR is the user's own editor choice; running it is the command's purpose
	c := exec.CommandContext(context.Background(), editor, path)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return meEditedMsg{path, err}
	})
}

type meEditedMsg struct {
	path string
	err  error
}
