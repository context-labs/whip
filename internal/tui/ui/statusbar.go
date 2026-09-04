package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/tui/theme"
)

// StatusBar is the one-line footer: left-aligned context, right-aligned
// hints, both muted, on the terminal background. Left is truncated first.
type StatusBar struct {
	Left, Right string
	Width       int
}

func (s StatusBar) Render(th *theme.Theme) string {
	right := th.MutedText.Render(s.Right)
	rw := ansi.StringWidth(s.Right)
	leftMax := s.Width - rw - 1
	if s.Right == "" {
		leftMax = s.Width
	}
	left := th.MutedText.Render(ansi.Truncate(s.Left, max(0, leftMax), "…"))
	gap := s.Width - ansi.StringWidth(s.Left) - rw
	if gap < 1 {
		gap = 1
	}
	if s.Right == "" {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}
