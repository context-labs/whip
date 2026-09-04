package ui

import (
	"image/color"

	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/tui/theme"
)

// Kind is the semantic weight of a message.
type Kind int

const (
	Info Kind = iota
	Success
	Warning
	Error
)

// Color maps a Kind to its palette token.
func (k Kind) Color(th *theme.Theme) color.Color {
	switch k {
	case Success:
		return th.Success
	case Warning:
		return th.Warning
	case Error:
		return th.Error
	}
	return th.Info
}

// Toast is a one-line notice on the element surface with a colored leading
// bar, sized to Width.
type Toast struct {
	Text  string
	Kind  Kind
	Width int
}

func (t Toast) Render(th *theme.Theme) string {
	bg := th.Surface.Element
	bar := th.On(t.Kind.Color(th), bg).Render("▍")
	body := th.On(th.Text, bg).Render(ansi.Truncate(t.Text, max(1, t.Width-3), "…"))
	return Fill(bar+" "+body, t.Width, bg)
}
