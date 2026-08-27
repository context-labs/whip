package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/context-labs/whip/internal/llm"
)

// /export [path] writes the current session's transcript to a markdown file
// (default ./whip-transcript-<session>.md in cwd) and confirms with the
// absolute path.
//
// ponytail: the opencode include-options dialog (toggle images / tool output,
// ui/dialog-export-options.tsx) is deliberately skipped for v1 — we export
// the full transcript verbatim. Upgrade path: render each llm.Message through
// per-part filters driven by the flags the dialog would set, so "exclude tool
// output" becomes one if-branch here.
func (m *model) exportCommand(arg string) {
	path := strings.TrimSpace(arg)
	if path == "" {
		path = "whip-transcript-" + m.sessionID + ".md"
	}
	if m.agent == nil || len(m.agent.Messages) == 0 {
		m.append(dimStyle.Render("(nothing to export yet)"))
		return
	}
	if err := exportTranscript(path, m.agent.Messages); err != nil {
		m.append(errStyle.Render("export failed: " + err.Error()))
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	m.append(dimStyle.Render("⤓ transcript exported → " + abs))
}

// exportTranscript flattens a conversation into a readable markdown log:
// a ## heading per message, tool calls listed under the assistant message
// that issued them, and tool results under their own sub-heading.
func exportTranscript(path string, msgs []llm.Message) error {
	var b strings.Builder
	b.WriteString("# Session transcript\n\n")
	for _, msg := range msgs {
		switch msg.Role {
		case "tool":
			b.WriteString("#### Tool result\n\n" + msg.TextContent() + "\n\n")
			continue
		}
		fmt.Fprintf(&b, "## %s\n\n", displayRole(msg.Role))
		if c := msg.TextContent(); c != "" {
			b.WriteString(c + "\n\n")
		}
		for _, tc := range msg.ToolCalls {
			fmt.Fprintf(&b, "`%s`\n\n", tc.Function.Name)
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
