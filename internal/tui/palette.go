package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

func (m *model) paletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.palette
	if p == nil {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		m.palette = nil
	case tea.KeyUp:
		if len(p.items) > 0 {
			p.idx = (p.idx + len(p.items) - 1) % len(p.items)
		}
	case tea.KeyDown:
		if len(p.items) > 0 {
			p.idx = (p.idx + 1) % len(p.items)
		}
	case tea.KeyEnter:
		if len(p.items) > 0 && p.items[p.idx].run != nil {
			return p.items[p.idx].run(m)
		}
	case tea.KeyBackspace, tea.KeyDelete:
		if len(p.filter) > 0 {
			p.filter = p.filter[:len(p.filter)-1]
			p.applyFilter(m)
		}
	case tea.KeyRunes, tea.KeySpace:
		p.filter += string(msg.Runes)
		if msg.Type == tea.KeySpace {
			p.filter += " "
		}
		p.applyFilter(m)
	}
	return m, nil
}

func (m *model) paletteView() string {
	p := m.palette
	if p == nil {
		return ""
	}
	var out strings.Builder
	out.WriteString(botStyle.Render(" Commands"))
	out.WriteString("\n\n " + youStyle.Render(glyphUser) + p.filter + dimStyle.Render("█") + "\n\n")
	lastCategory := ""
	for index, item := range p.items {
		if item.category != lastCategory {
			if lastCategory != "" {
				out.WriteByte('\n')
			}
			out.WriteString(dimStyle.Render("  " + item.category))
			out.WriteByte('\n')
			lastCategory = item.category
		}
		marker := " "
		if index == p.idx {
			marker = botStyle.Render("→")
		}
		line := marker + " " + item.title
		if item.dynDesc != nil {
			line += dimStyle.Render(" — " + item.dynDesc(m))
		}
		if item.dynHint != nil {
			line += dimStyle.Render("  " + item.dynHint(m))
		}
		out.WriteString(line + "\n")
	}
	if len(p.items) == 0 {
		out.WriteString(dimStyle.Render("  (no matches)") + "\n")
	}
	out.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  (%d/%d) ↑/↓ select · enter run · esc close", min(p.idx+1, len(p.items)), len(p.items))))
	return m.paletteChrome(out.String())
}

func (m *model) paletteChrome(value string) string {
	if m.uiMode != opencodeMode {
		return value
	}
	return lipgloss.NewStyle().PaddingLeft(3).Render(value)
}
