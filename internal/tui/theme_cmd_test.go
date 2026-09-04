package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/config"
)

// /theme light must switch markdown rendering to the light style (dark text
// 234) immediately, and /theme dark back — and both must survive a render of
// every sample kind (the chroma registry poisoning case).
func TestThemeCommandSwitchesRendering(t *testing.T) {
	t.Cleanup(func() { setSchemeOverride(""); SetLightTheme(false) }) // theme state is process-global
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.command("/theme light")
	if CurrentTheme() != "light" {
		t.Fatalf("theme: %q", CurrentTheme())
	}
	out := renderMarkdown("body **bold** `code`\n\n```go\nx := 1\n```", 70)
	if !strings.Contains(out, "38;2;26;26;26") {
		t.Errorf("light body should use the light text color: %q", out[:80])
	}
	m.command("/theme dark")
	if CurrentTheme() != "dark" {
		t.Fatalf("theme: %q", CurrentTheme())
	}
	out = renderMarkdown("body\n\n```go\nx := 1\n```", 70)
	// code blocks take their text color from the theme's text token
	// (#eeeeee; glamour downgrades code to 256 colors → 255 in tests)
	if !strings.Contains(out, "38;2;238;238;238") || !(strings.Contains(out, "38;5;255") || strings.Contains(out, "38;2;238;238;238mx")) {
		t.Errorf("dark body/code should be the dark text color after switch back: %q", out[:120])
	}
	// and flip back to light once more — the chroma poisoning case
	m.command("/theme light")
	out = renderMarkdown("```go\nx := 1\n```", 70)
	if strings.Contains(out, "38;5;255") || strings.Contains(out, "38;2;238;238;238") {
		t.Errorf("light code block must not use the dark text color: %q", out[:120])
	}
	m.setTheme("dark") // leave tests in dark default
}

// bare /theme opens the theme switcher (palette panel) instead of toggling
// blindly — the whole point is to see the choices.
func TestThemeBareOpensSwitcher(t *testing.T) {
	m := compactCmdModel()
	m.command("/theme")
	if m.palette == nil {
		t.Fatal("bare /theme should open the palette")
	}
	if len(m.palette.items) != 3 || m.palette.items[0].title != "Theme: auto" ||
		m.palette.items[1].title != "Theme: light" || m.palette.items[2].title != "Theme: dark" {
		t.Fatalf("theme palette: %+v", m.palette.items)
	}
	// navigate to light and apply with enter
	tm, _ := m.paletteKey(keyMsg(tea.KeyDown))
	m = tm.(*model)
	tm, _ = m.paletteKey(keyMsg(tea.KeyEnter))
	m = tm.(*model)
	if CurrentTheme() != "light" {
		t.Fatalf("selecting light in the switcher should apply it, got %q", CurrentTheme())
	}
	// the switcher came from /theme, not ctrl+p: commit-and-close, don't
	// strand the user on a palette root they never opened
	if m.palette != nil {
		t.Fatal("enter in a directly-opened switcher should close the palette")
	}
	m.setTheme("dark")    // leave dark default for other tests
	setSchemeOverride("") // theme state is process-global: restore detection mode
}

// Theme defaults to auto ("" in config) unless the user picks one.
func TestThemeDefaultsToAuto(t *testing.T) {
	cfg := config.Default()
	if cfg.Theme != "" {
		t.Fatalf("default theme should be auto (\"\"), got %q", cfg.Theme)
	}
}

// the full screen renders without artifacts under both themes
func TestNoArtifactsBothThemes(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		m := compactCmdModel()
		m.Update(mkWinSize(70, 30))
		m.setTheme(theme)
		m.appendAssistant("Found it. **Fixed**:\n\n1. one\n2. two\n\n```go\nx := 1\n```")
		v := viewStr(m)
		for i, l := range strings.Split(v, "\n") {
			if strings.TrimSpace(ansi.Strip(l)) == "" && strings.Contains(l, "\x1b[") {
				t.Errorf("%s: row %d styled blank: %q", theme, i, l)
			}
			if ansi.StringWidth(l) > 70 {
				t.Errorf("%s: row %d overflows (%d)", theme, i, ansi.StringWidth(l))
			}
		}
		m.setTheme("dark")
	}
	setSchemeOverride("") // theme state is process-global: restore detection mode
}
