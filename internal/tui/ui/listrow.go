package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/tui/theme"
)

// ListRow is opendocker's list row: a coloured state badge, a label indented
// by depth, and a right-aligned slot, on a fill that overhangs the text by
// one cell on each side. The selected row sits on Surface.Hover; the open
// row (the agent whose transcript is shown) is bold.
type ListRow struct {
	Badge      string      // state word; the caller pads badges to the widest one in view
	BadgeColor color.Color // Success running, Warning blocked, Muted idle, Error failed, Info queued
	Label      string
	Right      string // current cell / tool + elapsed, "mail N", or ""
	Depth      int    // indent Depth*2 before the label
	Selected   bool
	Open       bool
	Width      int // the band width: Panel.Inner()+2
}

// Render draws one row of exactly Width cells on bg (nil = the terminal).
func (r ListRow) Render(th *theme.Theme, bg color.Color) string {
	fill := bg
	labelFg := th.Muted
	if r.Selected {
		fill = th.Surface.Hover
		if fill == nil { // ANSI-16 / neutral: no surfaces, use the selection fill
			fill = th.Primary
		}
	}
	if r.Selected || r.Open {
		labelFg = th.Text
		if fill != nil && fill == th.Primary {
			labelFg = th.OnPrimary
		}
	}
	on := func(fg color.Color) lipgloss.Style { return th.On(fg, fill) }
	badge := on(r.BadgeColor).Render(r.Badge)
	indent := strings.Repeat(" ", r.Depth*2)
	right := ""
	if r.Right != "" {
		right = on(th.Muted).Render(r.Right)
	}
	// [1 cell][badge][1][indent][label][gap][right][1 cell]
	labelMax := r.Width - 2 - lipgloss.Width(badge) - 1 - len(indent) - lipgloss.Width(right)
	if right != "" {
		labelMax-- // at least one cell between label and right slot
	}
	label := on(labelFg).Bold(r.Selected || r.Open).Render(ansi.Truncate(r.Label, max(labelMax, 1), "…"))
	left := on(nil).Render(" ") + badge + on(nil).Render(" "+indent) + label
	if right != "" {
		gap := r.Width - 1 - lipgloss.Width(left) - lipgloss.Width(right)
		left += on(nil).Render(strings.Repeat(" ", max(gap, 1))) + right
	}
	return PadRow(ansi.Truncate(left, r.Width, ""), r.Width, fill)
}
