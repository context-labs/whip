package ui

import (
	"image/color"
	"strings"

	"github.com/context-labs/whip/internal/tui/theme"
)

// Hints renders key hints the way the footer and dialog footers show them:
// the key in the text colour, its label muted, one space inside a hint and
// two between hints. pairs alternate key, label.
func Hints(th *theme.Theme, bg color.Color, pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if i > 0 {
			b.WriteString(th.On(nil, bg).Render("  "))
		}
		b.WriteString(th.On(th.Text, bg).Render(pairs[i]) + th.On(th.Muted, bg).Render(" "+pairs[i+1]))
	}
	return b.String()
}
