package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/llm"
)

type rewindEntry struct {
	cut  int
	text string
	when *time.Time
}

type rewindState struct {
	entries []rewindEntry
	sel     int
	savedVP int
}

type escArmMsg struct{}

func (m *model) rewindEntries() []rewindEntry {
	var entries []rewindEntry
	for i, message := range m.displayMessages() {
		if message.Role == "user" && message.Authored {
			entries = append(entries, rewindEntry{cut: i, text: oneLine(message.TextContent()), when: message.SentAt})
		}
	}
	return entries
}

func oneLine(value string) string { return truncLine(strings.Join(strings.Fields(value), " "), 100) }

func firstLine(value string) string {
	for line := range strings.Lines(value) {
		if line = strings.TrimSpace(line); line != "" {
			return truncLine(strings.Join(strings.Fields(line), " "), 120)
		}
	}
	return "(no output)"
}

func lastLines(value string, count int) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	var kept []string
	for i := len(lines) - 1; i >= 0 && len(kept) < count; i-- {
		if line := strings.TrimRight(lines[i], "\r \t"); line != "" {
			kept = append([]string{truncLine(line, 200)}, kept...)
		}
	}
	return strings.Join(kept, "\n  ")
}

func toolVerb(name string) string {
	switch name {
	case "read":
		return "Reading"
	case "write":
		return "Writing"
	case "edit":
		return "Editing"
	case "bash":
		return "Running"
	case "agents.spawn":
		return "Delegating"
	default:
		return name
	}
}

func (m *model) batchSuffix(name, self string) string {
	var ids []string
	for _, value := range m.blocks {
		if (value.kind == blockToolQueued || value.kind == blockToolRun) && value.toolName == name && value.toolID != "" {
			ids = append(ids, value.toolID)
		}
	}
	if !slices.Contains(ids, self) {
		ids = append(ids, self)
	}
	if len(ids) < 2 {
		return ""
	}
	slices.Sort(ids)
	return " " + strconv.Itoa(slices.Index(ids, self)+1) + "/" + strconv.Itoa(len(ids))
}

func (m *model) scrollToMsg(index int) {
	if index < 0 || index >= len(m.msgBlock) {
		return
	}
	blockIndex := m.msgBlock[index]
	if blockIndex < 0 || blockIndex >= len(m.blocks) {
		return
	}
	m.follow = false
	m.vp.SetYOffset(max(m.blocks[blockIndex].y0-1, 0))
}

func (m *model) openRewind() {
	entries := m.rewindEntries()
	if len(entries) == 0 {
		m.append(dimStyle.Render("(nothing to rewind to yet)"))
		return
	}
	m.rew = &rewindState{entries: entries, sel: len(entries) - 1, savedVP: m.vp.YOffset()}
	m.scrollToMsg(entries[len(entries)-1].cut)
}

func (m *model) rewindKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	state := m.rew
	selected := func() rewindEntry { return state.entries[state.sel] }
	switch msg.String() {
	case "esc", "ctrl+c":
		m.vp.SetYOffset(state.savedVP)
		m.rew = nil
	case "up":
		state.sel = max(state.sel-1, 0)
		m.scrollToMsg(selected().cut)
	case "down":
		state.sel = min(state.sel+1, len(state.entries)-1)
		m.scrollToMsg(selected().cut)
	case "enter":
		entry := selected()
		m.rew = nil
		messages := m.displayMessages()
		if entry.cut >= 0 && entry.cut < len(messages) {
			m.input.SetValue(messages[entry.cut].TextContent())
			m.input.CursorEnd()
		}
		return m.submitClientAction("history.rewind", map[string]string{"args": strconv.Itoa(entry.cut)}, "")
	case "f":
		{
			entry := selected()
			m.rew = nil
			name := strings.TrimSpace(m.sessTitle)
			if name == "" {
				name = "session"
			}
			m.openClientNamePrompt("⑂ fork name:", name+" (fork)", "session.fork", entry.cut)
		}
	}
	return m, nil
}

func (m *model) rewindView() string {
	state := m.rew
	const maxRows = 8
	start := max(0, min(state.sel-maxRows/2, len(state.entries)-maxRows))
	end := min(start+maxRows, len(state.entries))
	var out strings.Builder
	out.WriteString(dimStyle.Render("⏪ rewind — enter: rewind here · f: fork from here · esc: cancel"))
	for row := start; row < end; row++ {
		entry := state.entries[row]
		out.WriteString("\n")
		if row == state.sel {
			out.WriteString(youStyle.Render(glyphUser + entry.text))
		} else {
			out.WriteString("  " + entry.text)
		}
		out.WriteString("\n    " + dimStyle.Render(rewindWhen(entry.when)))
	}
	fmt.Fprintf(&out, "\n%s", dimStyle.Render(fmt.Sprintf("  (%d/%d) ↑ older · ↓ newer", state.sel+1, len(state.entries))))
	return out.String()
}

func fmtTurn(usage llm.Usage) string {
	in := fmtTok(usage.PromptTokens) + " in"
	if cached := usage.Cached(); cached > 0 {
		in += fmt.Sprintf(" (%s cached)", fmtTok(cached))
	}
	return fmt.Sprintf("%s / %s out", in, fmtTok(usage.CompletionTokens))
}

func rewindWhen(value *time.Time) string {
	if value == nil {
		return "—"
	}
	return value.Local().Format("2006-01-02 15:04") + " · " + ago(*value)
}
