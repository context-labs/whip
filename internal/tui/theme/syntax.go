package theme

import (
	"fmt"
	"image/color"

	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

// Syntax is the syntax-highlighting palette. Both chroma (standalone code
// blocks, diff views) and glamour (fenced code inside markdown) are generated
// from it, so prose and code share one theme.
type Syntax struct{ Keyword, Type, Func, String, Number, Comment, Punct, Op color.Color }

// Syntax derives the code roles from the semantic palette, unless the spec
// pins them.
func (t *Theme) Syntax() Syntax {
	s := Syntax{
		Keyword: t.Primary, Type: t.Info, Func: t.Accent, String: t.Success,
		Number: t.Warning, Comment: t.Faint, Punct: t.Muted, Op: t.Text,
	}
	if p := t.spec.Syntax; p != nil {
		s.Keyword, s.Type, s.Func, s.String = or(p.Keyword, s.Keyword), or(p.Type, s.Type), or(p.Function, s.Func), or(p.String, s.String)
		s.Number, s.Comment, s.Punct, s.Op = or(p.Number, s.Number), or(p.Comment, s.Comment), or(p.Punctuation, s.Punct), or(p.Operator, s.Op)
	}
	return s
}

// or is the pinned color when the spec names one, else the derived one.
func or(pinned string, derived color.Color) color.Color {
	if pinned == "" {
		return derived
	}
	return col(pinned)
}

// ChromaName is the registered chroma style for this theme: the user's pick
// when the spec names one, else a generated style keyed by theme and surface
// so glamour's first-registration-wins registry never serves a stale palette
// after a theme change.
func (t *Theme) ChromaName() string {
	if t.spec.Chroma != "" {
		return t.spec.Chroma
	}
	return fmt.Sprintf("whip-%s-%s", t.Name, hexOf(t.Surface.Element))
}

// ChromaEntries is the generated token->style map.
func (t *Theme) ChromaEntries() chroma.StyleEntries {
	s := t.Syntax()
	entries := chroma.StyleEntries{
		chroma.Text:            hexOf(t.Text),
		chroma.Keyword:         hexOf(s.Keyword),
		chroma.KeywordType:     hexOf(s.Type),
		chroma.NameFunction:    hexOf(s.Func),
		chroma.NameClass:       hexOf(s.Type) + " bold",
		chroma.NameBuiltin:     hexOf(s.Func),
		chroma.LiteralString:   hexOf(s.String),
		chroma.LiteralNumber:   hexOf(s.Number),
		chroma.Comment:         hexOf(s.Comment) + " italic",
		chroma.Punctuation:     hexOf(s.Punct),
		chroma.Operator:        hexOf(s.Op),
		chroma.GenericInserted: hexOf(t.Success),
		chroma.GenericDeleted:  hexOf(t.Error),
		chroma.Error:           hexOf(t.Error),
	}
	if t.Surface.Element != nil {
		entries[chroma.Background] = "bg:" + hexOf(t.Surface.Element)
	}
	return entries
}

// ChromaStyle registers (once) and returns the chroma style code blocks and
// glamour share.
func (t *Theme) ChromaStyle() *chroma.Style {
	name := t.ChromaName()
	if st, ok := chromastyles.Registry[name]; ok {
		return st
	}
	return chromastyles.Register(chroma.MustNewStyle(name, t.ChromaEntries()))
}

// Markdown is the glamour style generated from the tokens. The neutral theme
// keeps glamour's structure but only terminal-palette colors. Fenced code
// uses the registered chroma style by name (CodeBlock.Theme), so glamour and
// standalone code blocks render identically.
func (t *Theme) Markdown() ansi.StyleConfig {
	p := t.spec.Palette
	st := styles.DarkStyleConfig
	if !t.Dark {
		st = styles.LightStyleConfig
	}
	heading, strong, code, quote := p.Accent, p.Warning, p.Success, p.Muted
	if md := t.spec.Markdown; md != nil {
		heading, strong, code, quote = orStr(md.Heading, heading), orStr(md.Strong, strong), orStr(md.Code, code), orStr(md.Quote, quote)
	}
	st.Document.Color = optStr(p.Text) // "" = terminal default foreground
	st.Heading.Color = optStr(heading)
	st.H1.Color, st.H1.BackgroundColor = optStr(heading), nil // no color chip
	st.H1.Prefix, st.H1.Suffix = "# ", ""
	st.H6.Color = optStr(p.Muted)
	st.HorizontalRule.Color = optStr(p.Border)
	st.Link.Color = optStr(p.Primary)
	st.LinkText.Color = optStr(p.Link)
	st.Image.Color = optStr(p.Primary)
	st.ImageText.Color = optStr(p.Muted)
	st.Strong.Color = optStr(strong)
	st.Emph.Color = optStr(p.Emphasis)
	st.Item.Color = optStr(p.Primary)
	st.BlockQuote.Color = optStr(quote)
	st.Code.Color = optStr(code)
	st.Code.BackgroundColor = optStr(hexOf(t.Surface.Element)) // nil chip when no surfaces
	st.Table.ColumnSeparator = new("│")
	st.Table.CenterSeparator = new("┼")
	st.Table.RowSeparator = new("─")
	st.Table.Margin = new(uint(0))
	if t.Neutral {
		st.CodeBlock.Color, st.CodeBlock.Chroma = nil, nil // glamour's plain code fence
	} else {
		t.ChromaStyle()
		st.CodeBlock.Chroma = nil
		st.CodeBlock.Theme = t.ChromaName()
	}
	return st
}

func orStr(pinned, derived string) string {
	if pinned == "" {
		return derived
	}
	return pinned
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// hexOf renders a color as "#rrggbb" ("" for nil).
func hexOf(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
