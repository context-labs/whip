package ui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/tui/theme"
)

// CodeBlock renders highlighted source on the element surface with the same
// chroma style glamour uses for fenced code, so a code block looks the same
// whether it came from markdown or a tool result. Lines are truncated, never
// wrapped: code wraps badly.
type CodeBlock struct {
	Lang        string
	Source      string
	Width       int
	LineNumbers bool
	MaxLines    int // 0 = all; otherwise clip with a "… N more lines" footer
}

func (c CodeBlock) Render(th *theme.Theme) string {
	lex := lexers.Get(c.Lang)
	if lex == nil {
		lex = lexers.Analyse(c.Source)
	}
	if lex == nil {
		lex = lexers.Fallback
	}
	it, err := chroma.Coalesce(lex).Tokenise(nil, c.Source)
	if err != nil {
		return c.Source
	}
	bg := th.Surface.Element
	style := th.ChromaStyle()

	// tokens render per line so lipgloss never equalizes widths across a
	// multi-line token
	var sb strings.Builder
	for tok := it(); tok != chroma.EOF; tok = it() {
		e := style.Get(tok.Type)
		s := th.On(th.Text, bg)
		if e.Colour.IsSet() {
			s = s.Foreground(lipgloss.Color(e.Colour.String()))
		}
		s = s.Bold(e.Bold == chroma.Yes).Italic(e.Italic == chroma.Yes).Underline(e.Underline == chroma.Yes)
		for i, part := range strings.Split(tok.Value, "\n") {
			if i > 0 {
				sb.WriteByte('\n')
			}
			if part != "" {
				sb.WriteString(s.Render(part))
			}
		}
	}
	lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")

	var footer string
	if c.MaxLines > 0 && len(lines) > c.MaxLines {
		footer = th.On(th.Muted, bg).Italic(true).Render(fmt.Sprintf("… %d more lines", len(lines)-c.MaxLines))
		lines = lines[:c.MaxLines]
	}
	gutter := 0
	if c.LineNumbers {
		gutter = len(strconv.Itoa(len(lines))) + 1
	}
	pad := th.Space.PadX
	inner := max(1, c.Width-2*pad-gutter)
	num := th.On(th.Muted, bg)
	out := make([]string, 0, len(lines)+1)
	for i, l := range lines {
		row := strings.Repeat(" ", pad)
		if c.LineNumbers {
			row += num.Render(fmt.Sprintf("%*d ", gutter-1, i+1))
		}
		out = append(out, row+ansi.Truncate(l, inner, "…"))
	}
	if footer != "" {
		out = append(out, strings.Repeat(" ", pad)+footer)
	}
	return Fill(strings.Join(out, "\n"), c.Width, bg)
}
