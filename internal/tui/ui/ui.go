// Package ui is whip's component kit: pure render functions and small prop
// structs that turn theme tokens into strings. Components never build colors
// of their own; everything comes from *theme.Theme, so a theme change repaints
// every component the same way. They are not tea.Models: state lives in the
// root model, components only draw.
package ui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/context-labs/whip/internal/tui/theme"
)

// Heading renders section titles.
func Heading(th *theme.Theme, s string) string { return th.Heading.Render(s) }

// Label renders a small bold category label.
func Label(th *theme.Theme, s string) string { return th.Label.Render(s) }

// Muted renders secondary text.
func Muted(th *theme.Theme, s string) string { return th.MutedText.Render(s) }

// Kbd renders a keyboard hint: text on the element surface.
func Kbd(th *theme.Theme, key string) string { return th.Kbd.Render(key) }

// Fill lays a pre-styled block onto bg at exactly width columns, back-filling
// every cell the body left unpainted. Nested resets inside body would
// otherwise punch holes in the surface (the ocOnBg/ocPadTo problem), so this
// is done at the cell level once, here, for every surface-backed component.
func Fill(body string, width int, bg color.Color) string {
	h := max(1, lipgloss.Height(body))
	scr := uv.NewScreenBuffer(width, h)
	uv.NewStyledString(body).Draw(scr, scr.Bounds())
	if bg != nil {
		for y := 0; y < h; y++ {
			for x := 0; x < width; x++ {
				c := scr.CellAt(x, y)
				if c == nil {
					continue
				}
				if c.Style.Bg == nil {
					n := *c
					if n.Width == 0 {
						n = uv.Cell{Content: " ", Width: 1}
					}
					n.Style.Bg = bg
					scr.SetCell(x, y, &n)
				}
			}
		}
	}
	return scr.Render()
}

// PadRow pads a single pre-styled row to width with spaces painted in bg (nil
// bg: plain spaces). Style.Width padding would land after the row's closing
// reset and drop the fill; this keeps a panel row filled edge to edge.
func PadRow(row string, width int, bg color.Color) string {
	if pad := width - lipgloss.Width(row); pad > 0 {
		row += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
	}
	return row
}
