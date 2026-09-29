package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

type nativeCommandChoice struct {
	name, description string
	instant           bool
}

// Only mounted native commands are advertised. Palette/completion selection
// still routes through each command's ordinary admission and owner checks.
var nativeCommandChoices = []nativeCommandChoice{
	{"/auth", "Connect a host provider or account", true},
	{"/connect", "Connect a host provider or account", true},
	{"/mouse", "Toggle local mouse capture", true},
	{"/agents", "Inspect the current agent tree", true},
	{"/attach", "Attach a client-local image", false},
	{"/browser", "Inspect offered tabs or external Chrome status and controls", true},
	{"/cd", "Change the host working directory", false},
	{"/check", "Read the original uncertain receipt", true},
	{"/clear", "Clear a stopped owner's history and REPL", false},
	{"/compact", "Compact with the captured helper configuration", false},
	{"/computer", "Inspect computer availability", true},
	{"/computer-use", "Submit a computer-use task", false},
	{"/context-doctor", "Inspect captured context and accounting evidence", true},
	{"/copy", "Copy the latest loaded assistant text", true},
	{"/dock", "Toggle the local agent dock", true},
	{"/effort", "Inspect or change reasoning effort", true},
	{"/export", "Export canonical history to a local file", false},
	{"/fork", "Name a fork of captured history", true},
	{"/fork-at", "Fork through an explicit boundary", false},
	{"/goal", "Inspect or set the session goal", true},
	{"/goal-from-context", "Formulate a goal from context", false},
	{"/help", "Show native commands and keys", true},
	{"/latest", "Return to live history", true},
	{"/lsp", "Inspect language-server status", true},
	{"/mcp", "Inspect MCP connections", true},
	{"/me", "Edit explicitly published standing instructions", false},
	{"/memory", "Inspect client-local notes", true},
	{"/model", "Select a model or saved default", true},
	{"/model-for-session", "Select this session's model", true},
	{"/newer", "Read newer canonical history", true},
	{"/older", "Read older canonical history", true},
	{"/panel", "Expand the agents, context, or LSP sidebar", false},
	{"/shell", "Inspect or explicitly focus the current interactive shell", true},
	{"/pending", "Inspect saved local input intents, including deleted owners", true},
	{"/permissions", "Inspect permission policy and grants", true},
	{"/pwd", "Show this owner's host directory", true},
	{"/queue", "Queue a prompt instead of steering", false},
	{"/quit", "Detach; accepted host work continues", true},
	{"/reasoning", "Show or hide live reasoning", false},
	{"/redraft", "Restore or discard a staged original input", false},
	{"/rejected", "Restore or discard the original rejected draft", false},
	{"/rename", "Rename the displayed owner", true},
	{"/repl", "Toggle the execution evidence panel", true},
	{"/report", "Inspect a redacted issue report", true},
	{"/resume", "Open a session by exact ID or root prefix", false},
	{"/retry", "Explicitly retry the original uncertain intent", false},
	{"/rewind", "Inspect history boundaries for a stopped owner", true},
	{"/schedule", "List, create, or cancel schedules", false},
	{"/sessions", "Browse recent roots", true},
	{"/settings", "Edit terminal-local display preferences", true},
	{"/setup", "Configure host providers and accounts", true},
	{"/sidebar", "Toggle the local sidebar", true},
	{"/start", "Start the displayed owner", false},
	{"/status", "Inspect this owner's lifecycle and pending work", true},
	{"/steer", "Steer the explicitly captured active turn", false},
	{"/stop", "Stop the displayed owner", false},
	{"/theme", "Choose a local terminal theme", true},
	{"/tools", "Expand or collapse displayed tool output", false},
}

type nativeCompletion struct {
	owner                      protocol.ID
	revision                   protocol.Counter
	cwd, base, displayed, head string
	line, column               int
	cancel                     context.CancelFunc
	explicit, truncated        bool
	candidates                 []protocol.WorkspaceCompletionCandidate
	selected                   int
}
type nativeCompletionResult struct {
	request    *nativeCompletion
	candidates []protocol.WorkspaceCompletionCandidate
	truncated  bool
	err        error
}

func (m *nativeModel) closeCompletion(restore bool) {
	if c := m.completion; c != nil {
		if c.cancel != nil {
			c.cancel()
		}
		if restore {
			m.input.SetValue(c.base)
			m.sizeInput()
		}
	}
	m.completion = nil
	m.sizeInput()
}

func (m *nativeModel) completeInput(explicit bool) tea.Cmd {
	m.closeCompletion(false)
	text := m.input.Value()
	if len(text) > nativeDraftLimit || m.input.Line() != m.input.LineCount()-1 {
		return nil
	}
	index := strings.LastIndexAny(text, " \n")
	head, token := text[:index+1], text[index+1:]
	lastLine := text[strings.LastIndex(text, "\n")+1:]
	if m.input.Column() != utf8.RuneCountInString(lastLine) {
		return nil
	}
	kind, prefix := "", ""
	switch {
	case head == "" && strings.HasPrefix(token, "/"):
		kind, prefix = "command", token
	case strings.HasPrefix(token, "@"):
		kind, prefix = "mention", token[1:]
	case strings.HasPrefix(token, "$"):
		kind, prefix = "skill", token[1:]
	case explicit && !strings.HasPrefix(text, "/"):
		kind, prefix = "path", token
	default:
		return nil
	}
	if len(prefix) > 4096 || kind == "skill" && len(prefix) > 64 {
		m.status = "Completion prefix exceeds its host bound (4096 bytes for paths, 64 for skills)."
		return nil
	}
	c := &nativeCompletion{owner: m.owner.ID, revision: m.owner.ConfigRevision, cwd: m.owner.WorkingDirectory, base: text, displayed: text, head: head, line: m.input.Line(), column: m.input.Column(), explicit: explicit, selected: -1}
	m.completion = c
	if kind == "command" {
		for _, choice := range nativeCommandChoices {
			if strings.HasPrefix(choice.name, prefix) {
				c.candidates = append(c.candidates, protocol.WorkspaceCompletionCandidate{Text: choice.name, Description: choice.description})
			}
		}
		if explicit {
			m.previewCompletion(0)
		}
		m.refresh()
		return nil
	}
	read, cancel := context.WithCancel(m.work.ctx)
	c.cancel = cancel
	return func() tea.Msg {
		defer cancel()
		result := nativeCompletionResult{request: c}
		ctx, done, err := m.work.beginFor(3 * time.Second)
		if err != nil {
			result.err = err
			return result
		}
		defer done()
		ctx, stop := context.WithCancel(ctx)
		defer stop()
		unhook := context.AfterFunc(read, stop)
		defer unhook()
		if err := read.Err(); err != nil {
			result.err = err
			return result
		}
		if !explicit {
			timer := time.NewTimer(80 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				result.err = ctx.Err()
				return result
			case <-timer.C:
			}
		}
		if kind == "skill" {
			var page protocol.ListSkillsResult
			result.err = m.connection.Call(ctx, "skills.list", protocol.ListSkillsParams{SessionID: c.owner, Prefix: prefix, Limit: 64}, &page)
			result.truncated = page.NextAfter != nil
			for _, skill := range page.Items {
				if !skill.Disabled {
					result.candidates = append(result.candidates, protocol.WorkspaceCompletionCandidate{Text: "$" + skill.Name, Description: skill.Description})
				}
			}
		} else {
			var page protocol.WorkspaceCompletionResult
			result.err = m.connection.Call(ctx, "workspace.complete", protocol.WorkspaceCompletionParams{SessionID: c.owner, Kind: kind, Prefix: prefix, Limit: 64}, &page)
			result.candidates, result.truncated = page.Candidates, page.Truncated
			if result.err == nil && page.WorkingDirectory != c.cwd {
				result.err = errors.New("working directory changed during completion")
			}
		}
		return result
	}
}

func (m *nativeModel) applyCompletion(value nativeCompletionResult) {
	c := value.request
	if m.completion != c || c == nil {
		return
	}
	if c.owner != m.owner.ID || c.revision != m.owner.ConfigRevision || c.cwd != m.owner.WorkingDirectory || c.displayed != m.input.Value() || c.line != m.input.Line() || c.column != m.input.Column() {
		m.closeCompletion(false)
		return
	}
	if value.err != nil {
		m.closeCompletion(false)
		m.status = "Host completion: " + value.err.Error()
		m.refresh()
		return
	}
	bytes := 0
	for _, candidate := range value.candidates {
		bytes += len(candidate.Text) + len(candidate.Description)
		if len(candidate.Text) > 8192 || strings.IndexFunc(candidate.Text, unicode.IsControl) >= 0 || !utf8.ValidString(candidate.Text+candidate.Description) {
			value.err = errors.New("invalid completion candidate")
			break
		}
	}
	if len(value.candidates) > 64 || bytes > 256<<10 || value.err != nil {
		m.closeCompletion(false)
		m.status = "Host completion exceeded its bounded candidate contract."
		m.refresh()
		return
	}
	c.candidates, c.truncated = value.candidates, value.truncated
	if c.explicit {
		m.previewCompletion(0)
	}
	m.refresh()
}

func (m *nativeModel) previewCompletion(delta int) {
	c := m.completion
	if c == nil || len(c.candidates) == 0 {
		return
	}
	if c.selected < 0 {
		c.selected = 0
		if delta < 0 {
			c.selected = len(c.candidates) - 1
		}
	} else {
		c.selected = (c.selected + delta + len(c.candidates)) % len(c.candidates)
	}
	c.displayed = c.head + c.candidates[c.selected].Text
	if len(c.displayed) > nativeDraftLimit {
		m.status = "Completion would exceed the draft text bound."
		m.closeCompletion(false)
		return
	}
	m.input.SetValue(c.displayed)
	c.line, c.column = m.input.Line(), m.input.Column()
	m.sizeInput()
}

func (m *nativeModel) completionKey(key tea.KeyPressMsg) (tea.Cmd, bool) {
	c := m.completion
	if c == nil {
		return nil, false
	}
	switch key.String() {
	case "esc":
		m.closeCompletion(true)
		m.refresh()
		return nil, true
	case "tab", "down":
		m.previewCompletion(1)
		m.refresh()
		return nil, true
	case "shift+tab", "up":
		m.previewCompletion(-1)
		m.refresh()
		return nil, true
	case "enter":
		if len(c.candidates) == 0 {
			m.closeCompletion(false)
			return nil, false
		}
		m.previewCompletion(0)
		m.closeCompletion(false)
		m.refresh()
		return nil, true
	}
	m.closeCompletion(false)
	return nil, false
}

func (m *nativeModel) completionHeight() int {
	if m.completion == nil || len(m.completion.candidates) == 0 {
		return 0
	}
	return min(len(m.completion.candidates)+1, 9, max(m.height/3, 1))
}

func (m *nativeModel) completionView() string {
	c := m.completion
	height := m.completionHeight()
	if height == 0 {
		return ""
	}
	label := "Completions · Tab/↑/↓ chooses · Enter inserts · Esc restores"
	if c.truncated {
		label = "Partial completion list · refine the prefix"
	}
	lines := []string{label}
	start := max(min(max(c.selected, 0)-(height-2)/2, len(c.candidates)-height+1), 0)
	for i := start; i < len(c.candidates) && len(lines) < height; i++ {
		marker := strings.Repeat(" ", ansi.StringWidth("› "))
		if i == c.selected {
			marker = "› "
		}
		lines = append(lines, fmt.Sprintf("%s%s · %s", marker, c.candidates[i].Text, c.candidates[i].Description))
	}
	for i := range lines {
		lines[i] = ansi.Truncate(nativeDisplayText(lines[i]), m.transcriptWidth(), "…")
	}
	return strings.Join(lines, "\n")
}
