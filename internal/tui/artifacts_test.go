package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The rendered screen must be artifact-free: no bare/empty SGR escapes
// (lipgloss' Width() padding artifact), no styled-blank rows, and no line
// wider than the terminal.
func TestNoArtifacts(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(70, 30))
	m.appendAssistant("Found the bug. Fixes:\n\n1. isolate HOME\n\n```go\nx := 1\n```")
	m.append("some status line")
	v := viewStr(m)
	for i, l := range strings.Split(v, "\n") {
		if strings.Contains(l, "\x1b[m") {
			t.Errorf("row %d still has bare SGR: %q", i, l)
		}
		if strings.TrimSpace(ansi.Strip(l)) == "" && strings.Contains(l, "\x1b[") {
			t.Errorf("row %d is a styled blank: %q", i, l)
		}
		if w := ansi.StringWidth(l); w > 70 {
			t.Errorf("row %d overflows width 70 (%d)", i, w)
		}
	}
}
