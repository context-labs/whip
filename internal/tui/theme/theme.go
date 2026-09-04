package theme

import (
	"image/color"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// Palette is the resolved semantic palette (see PaletteSpec for meanings).
type Palette struct {
	Text, Muted, Faint            color.Color
	Primary, Accent               color.Color
	Success, Warning, Error, Info color.Color
	Link, Emphasis                color.Color
	OnPrimary                     color.Color
	Border, BorderFocus           color.Color
	Bg                            color.Color
	DiffAdd, DiffDel              color.Color // background tints behind diff lines
}

// Surfaces is the raised-layer ladder: terminal background -> Panel (cards,
// sidebar) -> Element (prompt box, code block) -> Hover (selected row,
// hovered card). A nil fill means "leave the terminal background alone".
type Surfaces struct{ Base, Panel, Element, Hover color.Color }

// Spacing is the cell-unit spacing scale. A terminal has no radius; the
// rounded border is the only one.
type Spacing struct{ Gutter, PadX, PadY, Gap int }

// Theme is the resolved token set components consume.
type Theme struct {
	Name    string
	Dark    bool
	Neutral bool // unknown background: ANSI colors only, no fills
	Profile colorprofile.Profile
	Palette
	Surface Surfaces
	Space   Spacing
	spec    Spec

	// Typography: attribute conventions, not fonts.
	Heading, Body, Label, MutedText, FaintText lipgloss.Style
	Kbd, Code                                  lipgloss.Style
	Selected                                   lipgloss.Style // OnPrimary on Primary
	Frame                                      lipgloss.Border

	// Component styles pushed into bubbles on theme swap.
	Textarea textarea.Styles
	Spinner  lipgloss.Style
}

// Resolve builds a Theme from a spec. bg is the terminal's real background
// when known (the OSC 11 reply), nil otherwise; profile is the detected color
// depth. Surfaces step away from the real background so cards read as raised
// layers on any terminal, and vanish under 16 colors, where the nearest match
// for a subtle gray is plain black and borders must carry the layering.
func Resolve(spec Spec, bg color.Color, profile colorprofile.Profile) *Theme {
	if err := spec.Validate(); err != nil {
		// callers validate on load; a spec that still fails here is a built-in
		// bug, so fall back to the shipped theme of the same darkness
		if spec.Dark {
			spec = Dark()
		} else {
			spec = Light()
		}
	}
	p := spec.Palette
	t := &Theme{
		Name: spec.Name, Dark: spec.Dark, Neutral: spec.neutral, Profile: profile, spec: spec,
		Palette: Palette{
			Text: col(p.Text), Muted: col(p.Muted), Faint: col(p.Faint),
			Primary: col(p.Primary), Accent: col(p.Accent),
			Success: col(p.Success), Warning: col(p.Warning), Error: col(p.Error), Info: col(p.Info),
			Link: col(p.Link), Emphasis: col(p.Emphasis),
			OnPrimary: col(p.OnPrimary), Border: col(p.Border), BorderFocus: col(p.BorderFocus),
			Bg: col(p.Bg), DiffAdd: col(p.DiffAdd), DiffDel: col(p.DiffDel),
		},
		Space: Spacing{Gutter: 2, PadX: 1, PadY: 0, Gap: 1},
	}
	t.Surface = surfaces(spec, bg, profile)

	body := lipgloss.NewStyle()
	if t.Text != nil {
		body = body.Foreground(t.Text)
	}
	t.Body = body
	t.Heading = body.Bold(true)
	t.Label = lipgloss.NewStyle().Foreground(t.Muted).Bold(true)
	t.MutedText = lipgloss.NewStyle().Foreground(t.Muted)
	t.FaintText = lipgloss.NewStyle().Foreground(t.Faint)
	t.Kbd = t.On(t.Text, t.Surface.Element).Padding(0, 1)
	t.Code = t.On(t.Success, t.Surface.Element)
	t.Selected = t.On(t.OnPrimary, t.Primary)
	t.Frame = lipgloss.RoundedBorder()
	t.Spinner = lipgloss.NewStyle().Foreground(t.Info)

	ta := textarea.DefaultStyles(t.Dark)
	// The real terminal cursor: keep the user's own cursor colour (nil = no
	// OSC 12 repaint) and a steady block — Blink:true encodes as DECSCUSR 1,
	// which Bubble Tea treats as the default and never resets, so the user's
	// own cursor shape would not come back after whip exits.
	ta.Cursor = textarea.CursorStyle{Color: nil, Shape: tea.CursorBlock, Blink: false}
	elem := t.On(nil, t.Surface.Element)
	for _, st := range []*textarea.StyleState{&ta.Focused, &ta.Blurred} {
		st.Base = lipgloss.NewStyle()
		st.Text = elem
		st.CursorLine = elem
		st.Placeholder = t.On(t.Muted, t.Surface.Element)
		st.Prompt = lipgloss.NewStyle().Foreground(t.Info)
		st.EndOfBuffer = elem
		st.LineNumber = t.On(t.Muted, t.Surface.Element)
		st.CursorLineNumber = t.On(t.Muted, t.Surface.Element)
	}
	t.Textarea = ta
	return t
}

// surfaces derives the fills. Pinned surfaces win; otherwise the real
// background is stepped (dark: lighter, light: darker, bigger steps on light
// because a small delta from white is invisible); with no background RGB the
// built-in constants apply; under 16 colors or the neutral theme, none.
func surfaces(spec Spec, bg color.Color, profile colorprofile.Profile) Surfaces {
	if spec.neutral || profile < colorprofile.ANSI256 {
		return Surfaces{}
	}
	if spec.Surfaces != nil {
		return Surfaces{Base: bg, Panel: col(spec.Surfaces.Panel), Element: col(spec.Surfaces.Element), Hover: col(spec.Surfaces.Hover)}
	}
	if bg != nil {
		dark := isDark(bg) // the real background decides, so a light theme pinned on a dark terminal still raises
		step := func(n float64) color.Color {
			if dark {
				return lipgloss.Lighten(bg, 4*n)
			}
			return lipgloss.Darken(bg, 6*n)
		}
		return Surfaces{Base: bg, Panel: step(1), Element: step(2), Hover: step(3)}
	}
	if spec.Dark { // raised on the common dark schemes, never sunken holes
		return Surfaces{Panel: col("#343434"), Element: col("#404040"), Hover: col("#4c4c4c")}
	}
	return Surfaces{Panel: col("#ebebeb"), Element: col("#e1e1e1"), Hover: col("#d7d7d7")}
}

// On is the "paint a token on a layer" primitive components use instead of
// building styles from scratch: fg over bg, either side optional.
func (t *Theme) On(fg, bg color.Color) lipgloss.Style {
	s := lipgloss.NewStyle()
	if fg != nil {
		s = s.Foreground(fg)
	}
	if bg != nil {
		s = s.Background(bg)
	}
	return s
}

// Spec returns the theme's source, e.g. to list or persist it.
func (t *Theme) Spec() Spec { return t.spec }

// col parses a spec color: "" is nil (the terminal default).
func col(s string) color.Color {
	if s == "" {
		return nil
	}
	return lipgloss.Color(s)
}

// isDark is a luma threshold on the terminal background.
func isDark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return 0.299*float64(r>>8)+0.587*float64(g>>8)+0.114*float64(b>>8) < 128
}
