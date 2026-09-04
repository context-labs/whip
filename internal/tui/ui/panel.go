package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/context-labs/whip/internal/tui/theme"
)

// Panel is a card: shaded (Surface.Panel fill, no border) or bordered. Width
// is the outer width. Body may be pre-styled multi-line ANSI.
type Panel struct {
	Title    string
	Width    int
	Bordered bool
	Focused  bool
}

func (p Panel) Render(th *theme.Theme, body string) string {
	pad := th.Space.PadX
	if p.Bordered {
		if p.Title != "" {
			body = th.Heading.Render(p.Title) + "\n" + body
		}
		return lipgloss.NewStyle().Border(th.Frame).BorderForeground(borderColor(th, p.Focused)).
			Padding(0, pad).Width(p.Width).Render(body) // v2 Width is border-box
	}
	bg := th.Surface.Panel
	if p.Focused {
		bg = th.Surface.Hover
	}
	if p.Title != "" {
		body = th.On(th.Text, bg).Bold(true).Render(p.Title) + "\n" + body
	}
	return Fill(lipgloss.NewStyle().Padding(0, pad).Render(body), p.Width, bg)
}

func borderColor(th *theme.Theme, focused bool) color.Color {
	if focused {
		return th.BorderFocus
	}
	return th.Border
}
