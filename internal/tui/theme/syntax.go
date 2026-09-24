package theme

import (
	"fmt"
	"image/color"

	shared "github.com/context-labs/whip/internal/theme"

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
	s := t.spec.SyntaxColors()
	return Syntax{
		Keyword: col(s.Keyword), Type: col(s.Type), Func: col(s.Function), String: col(s.String),
		Number: col(s.Number), Comment: col(s.Comment), Punct: col(s.Punctuation), Op: col(s.Operator),
	}
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
	return shared.CodeEntries(t.spec, t.Surface.Element)
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
	md := t.spec.MarkdownColors()
	heading, strong, code, quote := md.Heading, md.Strong, md.Code, md.Quote
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
