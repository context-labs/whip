package tui

import (
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type nativeCommandPalette struct {
	query    string
	items    []nativeCommandChoice
	selected int
}

func (m *nativeModel) openNativePalette() {
	m.closeCompletion(false)
	m.palette = &nativeCommandPalette{}
	m.palette.filter()
}

func (p *nativeCommandPalette) filter() {
	p.items = nil
	query := strings.ToLower(strings.TrimSpace(p.query))
	for _, choice := range nativeCommandChoices {
		if query == "" || strings.Contains(strings.ToLower(choice.name+" "+choice.description), query) {
			p.items = append(p.items, choice)
		}
	}
	p.selected = min(p.selected, max(len(p.items)-1, 0))
}

func (m *nativeModel) paletteKey(key tea.KeyPressMsg) tea.Cmd {
	p := m.palette
	switch key.String() {
	case "esc", "ctrl+p":
		m.palette = nil
	case "up":
		if len(p.items) > 0 {
			p.selected = (p.selected + len(p.items) - 1) % len(p.items)
		}
	case "down":
		if len(p.items) > 0 {
			p.selected = (p.selected + 1) % len(p.items)
		}
	case "enter":
		if len(p.items) == 0 {
			return nil
		}
		choice := p.items[p.selected]
		m.palette = nil
		if choice.instant {
			return m.commandKeepingDraft(choice.name)
		}
		if m.input.Value() != "" && !strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
			m.status = "Draft kept. " + choice.name + " needs arguments; save or clear the composer before inserting it."
			return nil
		}
		m.input.SetValue(choice.name + " ")
		m.sizeInput()
	case "backspace":
		if p.query != "" {
			_, size := utf8.DecodeLastRuneInString(p.query)
			p.query = p.query[:len(p.query)-size]
			p.filter()
		}
	default:
		if key.Text != "" && len(p.query)+len(key.Text) <= 512 {
			p.query += key.Text
			p.filter()
		}
	}
	return nil
}

func (p *nativeCommandPalette) view(width, height int) string {
	rows := []string{"Commands · Enter selects · Esc closes", "> " + p.query, ""}
	count := max(height-5, 1)
	start := max(min(p.selected-count/2, len(p.items)-count), 0)
	for i := start; i < len(p.items) && i < start+count; i++ {
		marker := "  "
		if i == p.selected {
			marker = "› "
		}
		rows = append(rows, marker+p.items[i].name+" · "+p.items[i].description)
	}
	if len(p.items) == 0 {
		rows = append(rows, "No matching native commands.")
	}
	for i := range rows {
		rows[i] = ansi.Truncate(nativeDisplayText(rows[i]), width, "…")
	}
	return strings.Join(rows[:min(len(rows), height)], "\n")
}

func (m *nativeModel) commandKeepingDraft(command string) tea.Cmd {
	draft := m.input.Value()
	pastes, images := m.pastes, m.images
	result := m.command(command)
	if draft != "" && !strings.HasPrefix(strings.TrimSpace(draft), "/") {
		m.input.SetValue(draft)
		m.pastes, m.images = pastes, images
		m.sizeInput()
	}
	return result
}

func (m *nativeModel) shortcut(key tea.KeyPressMsg) (tea.Cmd, bool) {
	name := key.String()
	if name == "ctrl+p" {
		m.openNativePalette()
		return nil, true
	}
	if name == "ctrl+o" {
		mode := "on"
		if m.showReasoning {
			mode = "off"
		}
		return m.commandKeepingDraft("/reasoning " + mode), true
	}
	if name == "ctrl+x" {
		m.leaderAt = time.Now()
		m.status = "Ctrl+X: m model · l sessions · n clear · b sidebar · r REPL · t theme · c compact · g rewind · s stop child"
		return nil, true
	}
	if m.leaderAt.IsZero() {
		return nil, false
	}
	active := time.Since(m.leaderAt) < 2*time.Second
	m.leaderAt = time.Time{}
	if !active {
		return nil, false
	}
	commands := map[string]string{"m": "/model", "l": "/sessions", "n": "/clear", "b": "/sidebar", "r": "/repl", "t": "/theme", "c": "/compact", "g": "/rewind"}
	if command, ok := commands[name]; ok {
		return m.commandKeepingDraft(command), true
	}
	if name == "esc" {
		return nil, true
	}
	if name == "s" {
		id := m.owner.ID
		child := m.owner.ParentID != nil
		if m.agentsFocus && m.agents != nil {
			id = m.agentSelection
			child = false
			for _, row := range m.agents.rows {
				if row.id == id {
					child = row.parent != ""
					break
				}
			}
		}
		if !child {
			m.status = "Select or open a child to stop it with Ctrl+X S."
			return nil, true
		}
		return m.commandKeepingDraft("/agents stop " + string(id)), true
	}
	return nil, false
}
