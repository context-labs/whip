package theme

import (
	"fmt"
	"image/color"
	"reflect"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
)

// SurfaceColors is the raised-layer ladder, independent of terminal color depth.
type SurfaceColors struct{ Base, Panel, Element, Hover color.Color }

// ParseColor uses the same reference ANSI palette as the TUI's lipgloss colors:
// x/ansi's VGA-compatible first 16 entries, xterm's 6x6x6 cube, then grays.
// Actual terminal ANSI overrides cannot be observed by browser clients.
// An empty color means the terminal default; callers validate Specs first.
func ParseColor(s string) color.Color {
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "#") {
		n, _ := strconv.ParseUint(s[1:], 16, 32)
		return color.RGBA{R: uint8((n >> 16) & 0xff), G: uint8((n >> 8) & 0xff), B: uint8(n & 0xff), A: 255}
	}
	n, _ := strconv.Atoi(s)
	if n < 16 {
		return ansi.BasicColor(n) //nolint:gosec // Callers validate ANSI indexes as 0..255; this branch selects 0..15.
	}
	return ansi.IndexedColor(n) //nolint:gosec // Callers validate ANSI indexes as 0..255.
}

// Hex converts a color into an explicit browser color; nil remains empty.
func Hex(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// IsDark uses the TUI's background luma threshold.
func IsDark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return .299*float64(r>>8)+.587*float64(g>>8)+.114*float64(b>>8) < 128
}

// Surfaces preserves pinned fills and the TUI's exact 4% lightening / 6%
// darkening ladder. The terminal adapter suppresses fills at low color depth.
func Surfaces(spec Spec, bg color.Color) SurfaceColors {
	if spec.Neutral() {
		return SurfaceColors{}
	}
	if p := spec.Surfaces; p != nil {
		return SurfaceColors{Base: bg, Panel: ParseColor(p.Panel), Element: ParseColor(p.Element), Hover: ParseColor(p.Hover)}
	}
	if bg != nil {
		step := func(n float64) color.Color {
			r, g, b, a := bg.RGBA()
			channel := func(v uint32) uint8 {
				if IsDark(bg) {
					return uint8(min(255, float64(v>>8)+255*.04*n))
				}
				return uint8(float64(v>>8) * (1 - .06*n))
			}
			return color.RGBA{R: channel(r), G: channel(g), B: channel(b), A: uint8((a >> 8) & 0xff)}
		}
		return SurfaceColors{Base: bg, Panel: step(1), Element: step(2), Hover: step(3)}
	}
	if spec.Dark {
		return SurfaceColors{Panel: ParseColor("#343434"), Element: ParseColor("#404040"), Hover: ParseColor("#4c4c4c")}
	}
	return SurfaceColors{Panel: ParseColor("#ebebeb"), Element: ParseColor("#e1e1e1"), Hover: ParseColor("#d7d7d7")}
}

// SyntaxColors resolves palette-derived syntax roles before an explicit Chroma
// override. The terminal's ChromaStyle applies that override separately.
func (s Spec) SyntaxColors() SyntaxSpec {
	p := s.Palette
	out := SyntaxSpec{
		Keyword: p.Primary, Type: p.Info, Function: p.Accent, String: p.Success,
		Number: p.Warning, Comment: p.Faint, Punctuation: p.Muted, Operator: p.Text,
	}
	if s.Syntax != nil {
		fillOverrides(&out, *s.Syntax)
	}
	return out
}

// MarkdownColors resolves optional Markdown accents using TUI precedence.
func (s Spec) MarkdownColors() MarkdownSpec {
	p := s.Palette
	out := MarkdownSpec{Heading: p.Accent, Strong: p.Warning, Code: p.Success, Quote: p.Muted}
	if s.Markdown != nil {
		fillOverrides(&out, *s.Markdown)
	}
	return out
}

func fillOverrides[T any](dest *T, source T) {
	d, s := reflect.ValueOf(dest).Elem(), reflect.ValueOf(source)
	for i := range s.NumField() {
		if s.Field(i).String() != "" {
			d.Field(i).Set(s.Field(i))
		}
	}
}

func normalizeColors[T any](value *T) {
	v := reflect.ValueOf(value).Elem()
	for _, field := range v.Fields() {
		field.SetString(Hex(ParseColor(field.String())))
	}
}

// CodeEntries is the token map shared by Chroma terminal rendering and portable
// exports. This returns data without registering a process-global style.
func CodeEntries(spec Spec, background color.Color) chroma.StyleEntries {
	p, s := spec.Palette, spec.SyntaxColors()
	normalizeColors(&p)
	normalizeColors(&s)
	entries := chroma.StyleEntries{
		chroma.Text:            p.Text,
		chroma.Keyword:         s.Keyword,
		chroma.KeywordType:     s.Type,
		chroma.NameFunction:    s.Function,
		chroma.NameClass:       s.Type + " bold",
		chroma.NameBuiltin:     s.Function,
		chroma.LiteralString:   s.String,
		chroma.LiteralNumber:   s.Number,
		chroma.Comment:         s.Comment + " italic",
		chroma.Punctuation:     s.Punctuation,
		chroma.Operator:        s.Operator,
		chroma.GenericInserted: p.Success,
		chroma.GenericDeleted:  p.Error,
		chroma.Error:           p.Error,
	}
	if background != nil {
		entries[chroma.Background] = "bg:" + Hex(background)
	}
	return entries
}
