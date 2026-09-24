package ui

import (
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/context-labs/whip/internal/tui/theme"
)

// Scrollbar draws a one-column bar for a pane showing h rows of total at
// offset: a solid thumb on a filled track, opendocker's look. Nothing is drawn
// when the content fits. Under a theme without surfaces (16 colours, the
// neutral theme) the track is a faint hairline and the thumb a solid block.
func Scrollbar(scr uv.Screen, th *theme.Theme, x, y, h, total, offset int, focused bool) {
	if total <= h || h <= 0 {
		return
	}
	thumbFg := th.Border
	if focused {
		thumbFg = th.BorderFocus
	}
	thumb := max(h*h/total, 1)
	top := y + offset*(h-thumb)/max(total-h, 1)
	track := &uv.Cell{Content: " ", Width: 1, Style: uv.Style{Bg: th.Surface.Element}}
	if th.Surface.Element == nil {
		track = &uv.Cell{Content: "│", Width: 1, Style: uv.Style{Fg: th.Faint}}
		thumbFg = th.Muted
	}
	grip := &uv.Cell{Content: "█", Width: 1, Style: uv.Style{Fg: thumbFg}}
	for row := y; row < y+h; row++ {
		c := track
		if row >= top && row < top+thumb {
			c = grip
		}
		scr.SetCell(x, row, c)
	}
}
