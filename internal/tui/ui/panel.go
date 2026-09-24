package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/tui/theme"
)

// Panel is the shell's card, modelled on opendocker's pane: a one-column
// left bar that is coloured only while the panel is focused (otherwise it is
// painted in the panel colour so the width never changes), a Surface.Panel
// fill, PadY rows above and below, a title row "[Key] Title … Count", a blank
// row, then the body. A collapsed panel is exactly PadY+1+PadY rows: the
// header alone. Width is the outer width including the bar. Bordered keeps
// the rounded-frame variant used by dialogs.
type Panel struct {
	Title     string
	Key       string // "1" → "[1] " in Muted before the title
	Count     string // Muted, right-aligned on the title row
	Width     int
	Height    int  // > 0: exactly Height rows (padded or clipped)
	Collapsed bool // header only
	Focused   bool // the bar shows in th.Border
	Band      bool // body rows are Inner()+2 wide bands (selection fills) placed one cell left of the text inset
	Bordered  bool
}

// Inner is the text width inside the fill: the bar and PadX on both sides.
func (p Panel) Inner(th *theme.Theme) int { return max(p.Width-1-2*th.Space.PadX, 0) }

// Render lays the header and body onto the panel. Body rows may be
// pre-styled; they are clipped to the inner width (or the band width).
func (p Panel) Render(th *theme.Theme, body string) string {
	if p.Bordered {
		if p.Title != "" {
			body = th.Heading.Render(p.Title) + "\n" + body
		}
		return lipgloss.NewStyle().Border(th.Frame).BorderForeground(th.Border).
			Padding(0, th.Space.PadX).Width(p.Width).Render(body) // v2 Width is border-box
	}
	bg := th.Surface.Panel
	fill := th.On(nil, bg)
	bar := fill.Render(" ")
	if p.Focused {
		bar = th.On(th.Border, bg).Render("┃")
	}
	pad := th.Space.PadX
	inner := p.Inner(th)
	row := func(content string, inset int) string { // bar, inset cells of fill, content, fill to the edge
		return bar + PadRow(fill.Render(strings.Repeat(" ", inset))+content, p.Width-1, bg)
	}
	blank := row("", 0)

	rows := make([]string, 0, p.Height)
	for range th.Space.PadY {
		rows = append(rows, blank)
	}
	title := ""
	if p.Key != "" {
		title = th.On(th.Muted, bg).Render("[" + p.Key + "] ")
	}
	title += th.On(th.Text, bg).Bold(true).Render(p.Title)
	count := th.On(th.Muted, bg).Render(p.Count)
	gap := inner - lipgloss.Width(title) - lipgloss.Width(count)
	if gap < 1 && p.Count != "" {
		gap = 1
	}
	rows = append(rows, row(title+fill.Render(strings.Repeat(" ", max(gap, 0)))+count, pad))
	if !p.Collapsed {
		rows = append(rows, blank)
		if body != "" {
			for ln := range strings.SplitSeq(body, "\n") {
				if p.Band {
					rows = append(rows, row(ansi.Truncate(ln, inner+2, ""), pad-1))
				} else {
					rows = append(rows, row(ansi.Truncate(ln, inner, "…"), pad))
				}
			}
		}
		for range th.Space.PadY {
			rows = append(rows, blank)
		}
	} else {
		for range th.Space.PadY {
			rows = append(rows, blank)
		}
	}
	if p.Height > 0 {
		for len(rows) < p.Height {
			rows = append(rows, blank)
		}
		rows = rows[:p.Height]
	}
	return strings.Join(rows, "\n")
}
