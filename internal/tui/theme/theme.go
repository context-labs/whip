package theme

import (
	"image/color"

	"github.com/charmbracelet/lipgloss"
	shared "github.com/context-labs/whip/internal/theme"
)

// Theme is a semantic palette resolved for terminal rendering.
type Theme struct {
	Name                          string
	Dark                          bool
	Neutral                       bool
	Text, Muted, Faint            color.Color
	Primary, Accent               color.Color
	Success, Warning, Error, Info color.Color
	Link, Emphasis                color.Color
	OnPrimary                     color.Color
	Border, BorderFocus           color.Color
	Bg, DiffAdd, DiffDel          color.Color
	Panel, Element, Hover         color.Color
	spec                          Spec
}

// Resolve converts a validated spec into terminal colors and local surfaces.
func Resolve(spec Spec, bg color.Color) *Theme {
	if err := spec.Validate(); err != nil {
		if spec.Dark {
			spec = Dark()
		} else {
			spec = Light()
		}
	}
	p := spec.Palette
	base := col(p.Bg)
	if bg != nil {
		base = bg
	}
	s := shared.Surfaces(spec, base)
	return &Theme{
		Name: spec.Name, Dark: spec.Dark, Neutral: spec.Neutral(), spec: spec,
		Text: col(p.Text), Muted: col(p.Muted), Faint: col(p.Faint),
		Primary: col(p.Primary), Accent: col(p.Accent),
		Success: col(p.Success), Warning: col(p.Warning), Error: col(p.Error), Info: col(p.Info),
		Link: col(p.Link), Emphasis: col(p.Emphasis), OnPrimary: col(p.OnPrimary),
		Border: col(p.Border), BorderFocus: col(p.BorderFocus), Bg: col(p.Bg),
		DiffAdd: col(p.DiffAdd), DiffDel: col(p.DiffDel),
		Panel: s.Panel, Element: s.Element, Hover: s.Hover,
	}
}

func (t *Theme) On(fg, bg color.Color) lipgloss.Style {
	s := lipgloss.NewStyle()
	if fg != nil {
		s = s.Foreground(t.Terminal(fg))
	}
	if bg != nil {
		s = s.Background(t.Terminal(bg))
	}
	return s
}

// Terminal preserves ANSI-index colors (notably neutral mode) and emits a hex
// color for derived surfaces.
func (t *Theme) Terminal(c color.Color) lipgloss.TerminalColor {
	if c == nil {
		return lipgloss.NoColor{}
	}
	want := shared.Hex(c)
	p := t.spec.Palette
	for _, raw := range []string{
		p.Text, p.Muted, p.Faint, p.Primary, p.Accent,
		p.Success, p.Warning, p.Error, p.Info, p.Link, p.Emphasis,
		p.OnPrimary, p.Border, p.BorderFocus, p.Bg, p.DiffAdd, p.DiffDel,
	} {
		if raw != "" && shared.Hex(shared.ParseColor(raw)) == want {
			return lipgloss.Color(raw)
		}
	}
	return lipgloss.Color(want)
}

func (t *Theme) Spec() Spec { return t.spec }

func col(s string) color.Color {
	if s == "" {
		return nil
	}
	return shared.ParseColor(s)
}
