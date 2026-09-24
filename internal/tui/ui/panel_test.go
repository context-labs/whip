package ui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/tui/theme"
)

// A collapsed panel is exactly the header (PadY + title + PadY); an expanded
// one with a Height is exactly that tall; every row is Width cells; the inner
// text width is the fill minus PadX on both sides.
func TestPanelGeometry(t *testing.T) {
	th := theme.Resolve(theme.Dark(), color.RGBA{R: 0x1e, G: 0x1e, B: 0x1e, A: 0xff}, colorprofile.TrueColor)
	p := Panel{Title: "Agents", Key: "1", Count: "3", Width: 42}
	if got := p.Inner(th); got != 42-1-2*th.Space.PadX {
		t.Fatalf("Inner = %d", got)
	}
	rows := strings.Split(Panel{Title: "Context", Key: "2", Count: "12%", Width: 42, Collapsed: true}.Render(th, ""), "\n")
	if len(rows) != 2*th.Space.PadY+1 {
		t.Fatalf("collapsed panel has %d rows, want %d", len(rows), 2*th.Space.PadY+1)
	}
	if plain := ansi.Strip(rows[th.Space.PadY]); !strings.HasPrefix(plain, strings.Repeat(" ", 1+th.Space.PadX)+"[2] Context") || !strings.HasSuffix(strings.TrimRight(plain, " "), "12%") {
		t.Fatalf("collapsed header row: %q", plain)
	}
	for _, r := range rows {
		if w := ansi.StringWidth(r); w != 42 {
			t.Fatalf("collapsed row width %d", w)
		}
	}
	exp := strings.Split(Panel{Title: "Agents", Key: "1", Count: "3", Width: 42, Height: 12}.Render(th, "one\ntwo"), "\n")
	if len(exp) != 12 {
		t.Fatalf("expanded panel has %d rows, want 12", len(exp))
	}
	focused := (Panel{Title: "A", Width: 20, Focused: true, Height: 4}).Render(th, "")
	if !strings.Contains(ansi.Strip(focused), "┃") {
		t.Fatal("focused panel must show its bar")
	}
	idle := (Panel{Title: "A", Width: 20, Height: 4}).Render(th, "")
	if strings.Contains(ansi.Strip(idle), "┃") {
		t.Fatal("an unfocused panel paints its bar invisibly")
	}
}
