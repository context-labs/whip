package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/tui/theme"
)

// StatusBar is the one-line footer: a left-aligned context fragment and
// right-aligned hints, both already styled by the caller (a spinner, a keycap,
// muted text), laid out on the terminal background and clamped to Width so
// the row can never wrap the frame.
type StatusBar struct {
	Left, Right string
	Width       int
}

func (s StatusBar) Render(_ *theme.Theme) string {
	left, rw := s.Left, ansi.StringWidth(s.Right)
	if s.Width > 0 && ansi.StringWidth(left)+rw+1 > s.Width { // the hints survive: the context gets the ellipsis
		left = ansi.Truncate(left, max(s.Width-rw-1, 0), "…")
	}
	gap := max(s.Width-ansi.StringWidth(left)-rw, 1)
	line := left + strings.Repeat(" ", gap) + s.Right
	if s.Width > 0 {
		line = ansi.Truncate(line, s.Width, "")
	}
	return line
}
