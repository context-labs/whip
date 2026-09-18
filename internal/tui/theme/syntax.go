package theme

import (
	"fmt"
	"image/color"

	"github.com/alecthomas/chroma/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
	shared "github.com/context-labs/whip/internal/theme"
)

func (t *Theme) ChromaName() string {
	if t.spec.Chroma != "" {
		return t.spec.Chroma
	}
	return fmt.Sprintf("whip-%s-%s", t.Name, hexOf(t.Element))
}

func (t *Theme) ChromaStyle() *chroma.Style {
	name := t.ChromaName()
	if st, ok := chromastyles.Registry[name]; ok {
		return st
	}
	return chromastyles.Register(chroma.MustNewStyle(name, shared.CodeEntries(t.spec, t.Element)))
}

// Markdown creates a Glamour style from the same semantic tokens as the TUI.
func (t *Theme) Markdown() glamouransi.StyleConfig {
	p := t.spec.Palette
	st := styles.DarkStyleConfig
	if !t.Dark {
		st = styles.LightStyleConfig
	}
	md := t.spec.MarkdownColors()
	st.Document.Color = optStr(p.Text)
	st.Heading.Color = optStr(md.Heading)
	st.H1.Color, st.H1.BackgroundColor = optStr(md.Heading), nil
	st.H1.Prefix, st.H1.Suffix = "# ", ""
	st.H6.Color = optStr(p.Muted)
	st.HorizontalRule.Color = optStr(p.Border)
	st.Link.Color = optStr(p.Primary)
	st.LinkText.Color = optStr(p.Link)
	st.Image.Color = optStr(p.Primary)
	st.ImageText.Color = optStr(p.Muted)
	st.Strong.Color = optStr(md.Strong)
	st.Emph.Color = optStr(p.Emphasis)
	st.Item.Color = optStr(p.Primary)
	st.BlockQuote.Color = optStr(md.Quote)
	st.Code.Color = optStr(md.Code)
	st.Code.BackgroundColor = optStr(hexOf(t.Element))
	st.Table.ColumnSeparator = new("│")
	st.Table.CenterSeparator = new("┼")
	st.Table.RowSeparator = new("─")
	st.Table.Margin = new(uint(0))
	if t.Neutral {
		st.CodeBlock.Color, st.CodeBlock.Chroma = nil, nil
		st.CodeBlock.Theme = ""
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

func hexOf(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
