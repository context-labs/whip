package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	menuRows        = 8
	palHintRewind   = "esc esc"
	palDescRewind   = "rewind the conversation"
	palHintThinking = "ctrl+o"
	palHintQuit     = "ctrl+c ctrl+c"
)

// paletteItem is a presentation-only command. Its action always resolves to
// a local display change or a daemon command through thinCommand.
type paletteItem struct {
	title     string
	category  string
	dynDesc   func(*model) string
	dynHint   func(*model) string
	suggested bool
	run       func(*model) (tea.Model, tea.Cmd)
}

type palette struct {
	items  []paletteItem
	all    []paletteItem
	idx    int
	filter string
}

func (p *palette) applyFilter(m *model) {
	query := strings.ToLower(strings.TrimSpace(p.filter))
	p.items = p.items[:0]
	for _, item := range p.all {
		haystack := strings.ToLower(item.title + " " + item.category)
		if item.dynDesc != nil {
			haystack += " " + strings.ToLower(item.dynDesc(m))
		}
		if query == "" || strings.Contains(haystack, query) {
			p.items = append(p.items, item)
		}
	}
	if len(p.items) == 0 {
		p.idx = 0
	} else if p.idx >= len(p.items) {
		p.idx = len(p.items) - 1
	}
}

func (m *model) openPalette() { m.openThinPalette() }

func (m *model) openPaletteOn(name string) {
	if strings.EqualFold(strings.TrimSpace(name), "theme") {
		m.openThinThemePalette()
		return
	}
	m.openThinPalette()
}

func (m *model) paletteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.palette
	if p == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		m.palette = nil
	case "up":
		if len(p.items) > 0 {
			p.idx = (p.idx + len(p.items) - 1) % len(p.items)
		}
	case "down":
		if len(p.items) > 0 {
			p.idx = (p.idx + 1) % len(p.items)
		}
	case "enter":
		if len(p.items) > 0 && p.items[p.idx].run != nil {
			return p.items[p.idx].run(m)
		}
	case "backspace", "delete":
		if len(p.filter) > 0 {
			p.filter = p.filter[:len(p.filter)-1]
			p.applyFilter(m)
		}
	default:
		if msg.Text == "" {
			return m, nil
		}
		p.filter += msg.Text
		p.applyFilter(m)
	}
	return m, nil
}

func (m *model) paletteChrome(value string) string {
	return lipgloss.NewStyle().PaddingLeft(3).Render(value)
}
