package tui

import (
	"image/color"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func hexOfColor(c color.Color) string {
	if c == nil {
		return ""
	}
	r, g, b, _ := c.RGBA()
	return strings.ToLower(ansiHex(int(r>>8), int(g>>8), int(b>>8)))
}

func ansiHex(r, g, b int) string { return "#" + hex2(r) + hex2(g) + hex2(b) }
func hex2(v int) string          { const d = "0123456789abcdef"; return string(d[v>>4]) + string(d[v&15]) }

// Every cell of the frame carries a background under a theme with one: the
// view is the theme's, not the terminal's, so a light theme reads on a dark
// terminal. The terminal's own colours follow through the view's fields.
func TestFrameIsFullyPainted(t *testing.T) {
	pinDarkTheme(t)
	for _, light := range []bool{false, true} {
		SetLightTheme(light)
		m := goldenModel(140, 40)
		view := m.View()
		th := currentTheme()
		if view.BackgroundColor == nil || hexOfColor(view.BackgroundColor) != hexOfColor(th.Bg) || view.ForegroundColor == nil {
			t.Fatalf("light=%v: view colours %v/%v, want theme bg %v", light, view.BackgroundColor, view.ForegroundColor, th.Bg)
		}
		for y := 0; y < 40; y++ {
			for x := 0; x < 140; x++ {
				c := m.scr.CellAt(x, y)
				if c == nil || c.Width == 0 {
					continue
				}
				if c.Style.Bg == nil {
					t.Fatalf("light=%v: cell (%d,%d) %q has no background", light, x, y, c.Content)
				}
				if c.Style.Fg == nil {
					t.Fatalf("light=%v: cell (%d,%d) %q has no foreground", light, x, y, c.Content)
				}
			}
		}
		if got := hexOfColor(m.scr.CellAt(0, 0).Style.Bg); got != hexOfColor(th.Bg) {
			t.Fatalf("light=%v: margin cell bg %s, want theme bg %s", light, got, hexOfColor(th.Bg))
		}
	}
}

// A catalog theme switches the whole view: /theme tokyonight paints its
// background, the switcher lists the catalog, and auto comes back clean.
func TestSwitchToCatalogTheme(t *testing.T) {
	t.Cleanup(func() { setSchemeOverride(""); SetLightTheme(false) })
	m := goldenModel(140, 40)
	m.command("/theme tokyonight")
	if CurrentTheme() != "tokyonight" || currentTheme().Name != "tokyonight" {
		t.Fatalf("theme = %q / %q", CurrentTheme(), currentTheme().Name)
	}
	viewStr(m)
	if got := hexOfColor(m.scr.CellAt(0, 0).Style.Bg); got != "#1a1b26" {
		t.Fatalf("tokyonight background not painted: %s", got)
	}
	if !strings.Contains(ansi.Strip(viewStr(m)), "find the config loader") {
		t.Fatal("transcript vanished under the theme switch")
	}
	m.command("/theme tokyonight-light")
	if th := currentTheme(); th.Dark || th.Name != "tokyonight-light" {
		t.Fatalf("light variant: %+v", th.Name)
	}
	names := themeNames()
	if len(names) < 60 || names[0] != "auto" || names[1] != "light" || names[2] != "dark" {
		t.Fatalf("switcher names: %v", names[:5])
	}
	m.command("/theme auto")
	if CurrentTheme() == "tokyonight-light" {
		t.Fatal("auto should unpin the catalog theme")
	}
}
