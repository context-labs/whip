package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The frame is always exactly the terminal size: every row is termWidth
// cells and there are height rows, at every size including degenerate ones
// from a PTY handshake.
func TestComposeKeepsFrameSize(t *testing.T) {
	for _, size := range [][2]int{{140, 40}, {79, 24}, {70, 30}, {1, 30}, {5, 3}} {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			m := compactCmdModel()
			m.Update(mkWinSize(w, h))
			m.append(" ❯ hello there")
			m.appendAssistant("a **bold** reply with a longer line that certainly wraps at narrow widths")
			m.layout()
			lines := strings.Split(ansi.Strip(viewStr(m)), "\n")
			if len(lines) != h {
				t.Fatalf("frame has %d rows, want %d", len(lines), h)
			}
			for i, l := range lines {
				if ansi.StringWidth(l) != w {
					t.Fatalf("row %d is %d cells, want %d: %q", i, ansi.StringWidth(l), w, l)
				}
			}
		})
	}
}

// The frame buffer is reused between frames of the same size and replaced on
// resize, so drawing does not allocate a fresh screen per View.
func TestComposeReusesScreenBuffer(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(100, 30))
	viewStr(m)
	first := m.scr
	viewStr(m)
	if m.scr != first {
		t.Fatal("same-size frames must reuse the screen buffer")
	}
	m.Update(mkWinSize(120, 30))
	viewStr(m)
	if m.scr == first || m.scr.Width() != 120 {
		t.Fatalf("resize must replace the buffer: %dx%d", m.scr.Width(), m.scr.Height())
	}
}
