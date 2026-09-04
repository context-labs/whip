package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The rendered screen must be artifact-free: a blank row carries exactly the
// theme's background (never a stray surface, attribute or stale style) and no
// line is wider than the terminal.
func TestNoArtifacts(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(70, 30))
	m.appendAssistant("Found the bug. Fixes:\n\n1. isolate HOME\n\n```go\nx := 1\n```")
	m.append("some status line")
	assertNoArtifacts(t, m, 70)
}

// assertNoArtifacts checks the frame just drawn: blank cells sit on the theme
// background with no attributes, and every row is exactly width cells.
func assertNoArtifacts(t *testing.T, m *model, width int) {
	t.Helper()
	th := currentTheme()
	for i, l := range strings.Split(viewStr(m), "\n") {
		if w := ansi.StringWidth(l); w != width {
			t.Errorf("row %d is %d cells, want %d", i, w, width)
		}
		if strings.TrimSpace(ansi.Strip(l)) != "" {
			continue
		}
		for x := 0; x < width; x++ {
			c := m.scr.CellAt(x, i)
			if c == nil {
				continue
			}
			if hexOfColor(c.Style.Bg) != hexOfColor(th.Bg) || c.Style.Attrs != 0 {
				t.Errorf("row %d: blank cell %d carries bg %s attrs %d (theme bg %s)", i, x, hexOfColor(c.Style.Bg), c.Style.Attrs, hexOfColor(th.Bg))
				break
			}
		}
	}
}
