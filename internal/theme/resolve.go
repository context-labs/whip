package theme

import (
	"fmt"
	"image/color"
	"reflect"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

// Colors are the fixed portable color roles. Every value is normalized #rrggbb.
// The original TUI JSON spelling remains unchanged in Spec; wire fields use snake_case.
type Colors struct {
	Background  string `json:"background"`
	Foreground  string `json:"foreground"`
	Muted       string `json:"muted"`
	Faint       string `json:"faint"`
	Primary     string `json:"primary"`
	OnPrimary   string `json:"on_primary"`
	Accent      string `json:"accent"`
	Success     string `json:"success"`
	Warning     string `json:"warning"`
	Error       string `json:"error"`
	Info        string `json:"info"`
	Link        string `json:"link"`
	Emphasis    string `json:"emphasis"`
	Border      string `json:"border"`
	BorderFocus string `json:"border_focus"`
	DiffAdd     string `json:"diff_add"`
	DiffDel     string `json:"diff_del"`
	Panel       string `json:"panel"`
	Element     string `json:"element"`
	Hover       string `json:"hover"`
}

// TokenStyle is effective Chroma styling, without CSS or terminal escapes.
type TokenStyle struct {
	Color      string `json:"color"`
	Background string `json:"background"`
	Bold       bool   `json:"bold"`
	Italic     bool   `json:"italic"`
	Underline  bool   `json:"underline"`
}

// CodeStyle preserves explicit Chroma overrides, including token attributes.
// Tokens use Chroma's symbolic token names (for example NameFunction).
type CodeStyle struct {
	Foreground string                `json:"foreground"`
	Background string                `json:"background"`
	Tokens     map[string]TokenStyle `json:"tokens"`
}

// Resolved is presentation data, safe to use without a terminal renderer.
type Resolved struct {
	ID       string       `json:"id"`
	Name     string       `json:"name"`
	Dark     bool         `json:"dark"`
	Colors   Colors       `json:"colors"`
	Syntax   SyntaxSpec   `json:"syntax"`
	Markdown MarkdownSpec `json:"markdown"`
	Code     CodeStyle    `json:"code"`
}

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
		return color.RGBA{R: uint8(n >> 16), G: uint8(n >> 8), B: uint8(n), A: 255}
	}
	n, _ := strconv.Atoi(s)
	if n < 16 {
		return ansi.BasicColor(n)
	}
	return ansi.IndexedColor(n)
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
			return color.RGBA{R: channel(r), G: channel(g), B: channel(b), A: uint8(a >> 8)}
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
	out := SyntaxSpec{Keyword: p.Primary, Type: p.Info, Function: p.Accent, String: p.Success,
		Number: p.Warning, Comment: p.Faint, Punctuation: p.Muted, Operator: p.Text}
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
	for i := range v.NumField() {
		v.Field(i).SetString(Hex(ParseColor(v.Field(i).String())))
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

// ResolveSpec resolves a validated browser theme without changing any catalog or
// Chroma registry. Neutral is terminal-specific; browsers use light/dark for auto.
func ResolveSpec(spec Spec) (Resolved, error) {
	if spec.Neutral() {
		return Resolved{}, fmt.Errorf("neutral is a terminal-only fallback")
	}
	if err := spec.Validate(); err != nil {
		return Resolved{}, err
	}
	p := spec.Palette
	normalizeColors(&p)
	surfaces := Surfaces(spec, ParseColor(p.Bg))
	colors := Colors{Background: p.Bg, Foreground: p.Text, Muted: p.Muted, Faint: p.Faint, Primary: p.Primary,
		OnPrimary: p.OnPrimary, Accent: p.Accent, Success: p.Success, Warning: p.Warning, Error: p.Error, Info: p.Info,
		Link: p.Link, Emphasis: p.Emphasis, Border: p.Border, BorderFocus: p.BorderFocus, DiffAdd: p.DiffAdd, DiffDel: p.DiffDel,
		Panel: orString(Hex(surfaces.Panel), p.Bg), Element: orString(Hex(surfaces.Element), p.Bg), Hover: orString(Hex(surfaces.Hover), p.Bg)}
	syntax, md := spec.SyntaxColors(), spec.MarkdownColors()
	normalizeColors(&syntax)
	normalizeColors(&md)
	entries := CodeEntries(spec, ParseColor(colors.Element))
	style, err := chroma.NewStyle(spec.Name, entries)
	if err != nil {
		return Resolved{}, fmt.Errorf("theme code styles: %w", err)
	}
	if spec.Chroma != "" {
		style = chromastyles.Registry[spec.Chroma]
	}
	tokenTypes := style.Types()
	for tt := range entries {
		tokenTypes = append(tokenTypes, tt)
	}
	code := CodeStyle{Foreground: p.Text, Background: colors.Element, Tokens: map[string]TokenStyle{}}
	text, bg := style.Get(chroma.Text), style.Get(chroma.Background)
	if text.Colour.IsSet() {
		code.Foreground = text.Colour.String()
	}
	if bg.Background.IsSet() {
		code.Background = bg.Background.String()
	}
	for _, tt := range tokenTypes {
		entry := style.Get(tt)
		item := TokenStyle{Color: code.Foreground, Background: code.Background, Bold: entry.Bold == chroma.Yes,
			Italic: entry.Italic == chroma.Yes, Underline: entry.Underline == chroma.Yes}
		if entry.Colour.IsSet() {
			item.Color = entry.Colour.String()
		}
		if entry.Background.IsSet() {
			item.Background = entry.Background.String()
		}
		code.Tokens[tt.String()] = item
	}
	// Explicit Chroma wins over a syntax block, exactly as in the terminal renderer.
	if spec.Chroma != "" {
		syntax = SyntaxSpec{Keyword: code.Tokens[chroma.Keyword.String()].Color, Type: code.Tokens[chroma.KeywordType.String()].Color,
			Function: code.Tokens[chroma.NameFunction.String()].Color, String: code.Tokens[chroma.LiteralString.String()].Color,
			Number: code.Tokens[chroma.LiteralNumber.String()].Color, Comment: code.Tokens[chroma.Comment.String()].Color,
			Punctuation: code.Tokens[chroma.Punctuation.String()].Color, Operator: code.Tokens[chroma.Operator.String()].Color}
	}
	return Resolved{ID: spec.Name, Name: spec.Name, Dark: spec.Dark, Colors: colors, Syntax: syntax, Markdown: md, Code: code}, nil
}

func orString(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
