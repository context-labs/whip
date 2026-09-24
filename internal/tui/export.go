package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/context-labs/whip/internal/llm"
)

// /export [path] writes the current session's transcript to a markdown file
// (default ./whip-transcript-<session>.md in cwd) and confirms with the
// absolute path.
func (m *model) exportCommand(arg string) tea.Cmd {
	path := strings.TrimSpace(arg)
	if path == "" {
		path = "whip-transcript-" + m.sessionID + ".md"
	}
	if m.client != nil {
		client, rootID, agentID := m.client, m.sessionID, m.visibleAgentID()
		m.append(dimStyle.Render("Exporting the full host transcript…"))
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			err := exportHostTranscript(ctx, client, path, rootID, agentID)
			return clientExportMsg{path: path, err: err}
		}
	}
	messages := m.displayMessages()
	if len(messages) == 0 {
		m.append(dimStyle.Render("(nothing to export yet)"))
		return nil
	}
	if err := exportTranscript(path, messages); err != nil {
		m.append(errStyle.Render("export failed: " + err.Error()))
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	m.append(dimStyle.Render("⤓ transcript exported → " + abs))
	return nil
}

// exportTranscript flattens a conversation into a readable markdown log:
// a ## heading per message, tool calls listed under the assistant message
// that issued them, and tool results under their own sub-heading.
func exportTranscript(path string, msgs []llm.Message) error {
	var b strings.Builder
	b.WriteString("# Session transcript\n\n")
	for _, msg := range msgs {
		if err := writeTranscriptMessage(&b, msg); err != nil {
			return err
		}
	}
	// 0o600: the transcript can hold secrets the user pasted; don't leave it
	// world-readable (gosec G306).
	return os.WriteFile(path, []byte(strings.TrimRight(b.String(), "\n")+"\n"), 0o600)
}

// displayRole maps a wire role to a reader-facing heading label.
func displayRole(role string) string {
	switch role {
	case "user":
		return "User"
	case "assistant":
		return "Assistant"
	case "system":
		return "System"
	default:
		// Title-case the first letter without strings.Title (deprecated).
		if role == "" {
			return role
		}
		return strings.ToUpper(role[:1]) + role[1:]
	}
}
