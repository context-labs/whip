// toolrow.go: rendering for completed tool calls — a bold "Verb(subject)"
// header row, and for file mutations a "⎿ Added N lines, removed M lines"
// summary over a colored, line-numbered diff. The shape follows claude-code's
// tool output: the header keeps the call's subject (path, command) visible
// after completion, and a write's diff is the collapsed view, not something
// hidden behind an expand.
package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	toolHeadStyle = lipgloss.NewStyle().Bold(true)
	// diff bands: colored background across the full row, terminal-default
	// foreground on top (legible on both themes).
	diffAddStyle = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "194", Dark: "22"})
	diffDelStyle = lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "224", Dark: "52"})
)

// toolHeaderName maps a tool to its header verb ("Update" over "edit" — the
// row reads as what happened, not which function ran).
func toolHeaderName(name string) string {
	switch name {
	case "edit":
		return "Update"
	case "write":
		return "Write"
	case "read":
		return "Read"
	case "bash":
		return "Bash"
	case "subagent":
		return "Subagent"
	case "subagent_steer":
		return "Steer"
	case "todowrite":
		return "Plan"
	case "remember":
		return "Remember"
	case "forget":
		return "Forget"
	case "browser_exec":
		return "Browser"
	case "computer_exec":
		return "Computer"
	}
	return name
}

// toolSubject extracts the human subject from a call's raw JSON args: the
// path for file tools, the command for bash, the description for subagents.
// Unknown shapes fall back to the compacted args.
func toolSubject(name, args string) string {
	var m map[string]any
	_ = json.Unmarshal([]byte(args), &m)
	get := func(k string) string { s, _ := m[k].(string); return s }
	s := ""
	switch name {
	case "bash":
		// the whole command, whitespace-collapsed but NEVER truncated — whip's
		// no-truncation rule for commands; the row wraps or truncates at
		// render time depending on where it appears
		s = strings.Join(strings.Fields(get("command")), " ")
	case "read", "write", "edit":
		s = get("path")
	case "subagent":
		if s = get("description"); s == "" {
			s = firstLine(get("prompt"))
		}
	case "subagent_steer":
		s = get("id")
	case "browser_exec", "computer_exec":
		s = browserStepLabel(args)
	}
	if s == "" {
		s = truncLine(oneLine(args), 60)
	}
	return s
}

// queuedSubject is the text a still-streaming tool row leads with: the
// subagent's task description when there is one (raw JSON for a subagent call
// is an unreadable blob), else the call's first line.
func queuedSubject(name, args string) string {
	if name == "subagent" {
		return toolSubject("subagent", args)
	}
	return firstLine(args)
}

// toolHeaderRow renders the completed call's header: "● Update(path)"
// (opencode mode: an indent-3 icon row, see ocToolRow).
func toolHeaderRow(name, args string, failed bool) string {
	if ocActive {
		return ocToolRow(name, args, failed)
	}
	head := toolHeaderName(name) + "(" + toolSubject(name, args) + ")"
	if failed {
		return errStyle.Render(glyphAssistant + head)
	}
	return toolStyle.Render(glyphAssistant) + toolHeadStyle.Render(toolHeaderName(name)) + "(" + toolSubject(name, args) + ")"
}

// extractDiff splits a tool result into its fenced ```diff block and the
// text around it. diff is "" when the result carries none.
func extractDiff(result string) (diff, rest string) {
	before, tail, found := strings.Cut(result, "\n```diff\n")
	if !found {
		return "", result
	}
	body, after, found := strings.Cut(tail, "\n```")
	if !found {
		return "", result
	}
	rest = before
	if after = strings.TrimPrefix(after, "\n"); after != "" {
		rest += "\n" + after
	}
	return body, rest
}

// diffLineKind classifies an editDiff row as added '+', removed '-', or
// context ' ', tolerating the optional leading line number.
func diffLineKind(line string) byte {
	rest := strings.TrimLeft(line, "0123456789")
	if rest != line {
		rest = strings.TrimPrefix(rest, " ")
	}
	switch {
	case strings.HasPrefix(rest, "+ ") || rest == "+":
		return '+'
	case strings.HasPrefix(rest, "- ") || rest == "-":
		return '-'
	}
	return ' '
}

// diffCounts tallies the added/removed rows of an editDiff body.
func diffCounts(diff string) (added, removed int) {
	for l := range strings.SplitSeq(diff, "\n") {
		switch diffLineKind(l) {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return added, removed
}

// diffSummary words the counts: "Added 7 lines, removed 7 lines".
func diffSummary(added, removed int) string {
	plural := func(n int) string {
		if n == 1 {
			return "line"
		}
		return "lines"
	}
	switch {
	case added > 0 && removed > 0:
		return fmt.Sprintf("Added %d %s, removed %d %s", added, plural(added), removed, plural(removed))
	case added > 0:
		return fmt.Sprintf("Added %d %s", added, plural(added))
	case removed > 0:
		return fmt.Sprintf("Removed %d %s", removed, plural(removed))
	}
	return "No lines changed"
}

// diffPreviewRows caps the collapsed diff body; expand shows everything.
const diffPreviewRows = 30

// renderDiffResult renders a diff-carrying tool result: the ⎿ summary, the
// colored diff (capped unless expanded), then any trailing result text (LSP
// diagnostics ride there — they must stay visible).
func renderDiffResult(diff, rest string, expanded bool, width int) string {
	added, removed := diffCounts(diff)
	var b strings.Builder
	b.WriteString(dimStyle.Render("  ⎿ ") + diffSummary(added, removed))
	rows := strings.Split(diff, "\n")
	shown := rows
	if !expanded && len(rows) > diffPreviewRows {
		shown = rows[:diffPreviewRows]
	}
	for _, l := range shown {
		b.WriteString("\n" + renderDiffLine(l, width))
	}
	if n := len(rows) - len(shown); n > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("\n    … +%d more (ctrl+e or click to expand)", n)))
	}
	// the text after the fence (e.g. LSP diagnostics); the first line before
	// it ("Replaced 1 occurrence…") is bookkeeping the summary already covers
	if trail := strings.TrimSpace(strings.Join(strings.Split(rest, "\n")[1:], "\n")); trail != "" {
		b.WriteString("\n" + wrap(dimStyle.Render("  "+strings.ReplaceAll(trail, "\n", "\n  ")), width))
	}
	return b.String()
}

// renderDiffLine paints one editDiff row: added rows on a green band, removed
// on red, context dim — the band runs the full width, the row truncates
// rather than wraps (the raw result behind expand keeps the full text).
func renderDiffLine(l string, width int) string {
	row := ansi.Truncate("    "+l, max(width, 8), "…")
	switch diffLineKind(l) {
	case '+':
		return diffAddStyle.Render(row + strings.Repeat(" ", max(width-lipgloss.Width(row), 0)))
	case '-':
		return diffDelStyle.Render(row + strings.Repeat(" ", max(width-lipgloss.Width(row), 0)))
	}
	return dimStyle.Render(row)
}
