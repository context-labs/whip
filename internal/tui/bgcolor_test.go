package tui

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/config"
)

// Bubble Tea's background reply resolves an unknown scheme, and only an
// unknown one: whip's own pre-run query stays authoritative when it answered.
func TestBackgroundColorMsgResolvesOnlyUnknownScheme(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	defer func() { SetLightTheme(false); bgCache = bgResult{} }()

	SetUnknownTheme()
	bgCache = bgResult{}
	m := compactCmdModel()
	m.cfg = &config.Config{}
	m.Update(mkWinSize(100, 30))
	m.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 0xfa, G: 0xfa, B: 0xfa, A: 0xff}})
	if !ocThemeKnown() || !schemeIsLight() || !bgCache.hasRGB || bgCache.r != 0xfa {
		t.Fatalf("light reply not applied: known=%v light=%v cache=%+v", ocThemeKnown(), schemeIsLight(), bgCache)
	}

	// a known dark scheme is not overridden by a later light reply
	SetLightTheme(false)
	m.Update(tea.BackgroundColorMsg{Color: color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}})
	if schemeIsLight() {
		t.Fatal("a known scheme must not be overridden by the terminal reply")
	}
}
