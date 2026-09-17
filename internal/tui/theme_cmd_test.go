package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/config"
	uitheme "github.com/context-labs/whip/internal/tui/theme"
)

// /theme switches semantic palettes immediately, including after Chroma has
// registered syntax styles for another theme.
func TestThemeCommandSwitchesRendering(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.command("/theme light")
	if CurrentTheme() != "light" {
		t.Fatalf("theme: %q", CurrentTheme())
	}
	out := renderMarkdown("body **bold** `code`\n\n```go\nx := 1\n```", 70)
	if !strings.Contains(out, "38;2;26;26;26") {
		t.Errorf("light body should use the light palette text: %q", out[:80])
	}
	m.command("/theme dark")
	if CurrentTheme() != "dark" {
		t.Fatalf("theme: %q", CurrentTheme())
	}
	out = renderMarkdown("body\n\n```go\nx := 1\n```", 70)
	if !strings.Contains(out, "38;2;238;238;238") {
		t.Errorf("dark body should use the dark palette text after switch back: %q", out[:120])
	}
	// and flip back to light once more — the chroma poisoning case
	m.command("/theme light")
	out = renderMarkdown("```go\nx := 1\n```", 70)
	if strings.Contains(out, "38;5;251") {
		t.Errorf("light code block must not use dark chroma 251: %q", out[:120])
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
	pp := m.palette.top()
	if pp == nil || pp.kind != panelTheme {
		t.Fatalf("expected the theme panel, got %+v", pp)
	}
	// The panel starts with auto, then grouped dark and light catalogs.
	if len(pp.list) < 4 || pp.list[0] != "auto" {
		t.Fatalf("theme panel list: %v", pp.list)
	}
	// Navigate to the first dark theme and apply it with enter.
	tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyDown})
	m = tm.(*model)
	selected := pp.list[1]
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(*model)
	if CurrentTheme() != selected {
		t.Fatalf("selecting %q in the switcher should apply it, got %q", selected, CurrentTheme())
	}
	// the switcher came from /theme, not ctrl+p: commit-and-close, don't
	// strand the user on a palette root they never opened
	if m.palette != nil {
		t.Fatal("enter in a directly-opened switcher should close the palette")
	}
	m.setTheme("dark")    // leave dark default for other tests
	setSchemeOverride("") // theme state is process-global: restore detection mode
}

func TestThemeNamesGroupDarkBeforeLight(t *testing.T) {
	names := themeNames()
	if len(names) == 0 || names[0] != "auto" {
		t.Fatalf("first theme = %q, want auto", names[0])
	}
	seenLight := false
	previous := ""
	for _, name := range names[1:] {
		spec, ok := uitheme.Builtin(name)
		if !ok {
			t.Fatalf("unknown theme in picker: %q", name)
		}
		if !spec.Dark {
			seenLight = true
		} else if seenLight {
			t.Fatalf("dark theme %q appears after light themes", name)
		}
		if previous != "" {
			prev, _ := uitheme.Builtin(previous)
			if prev.Dark == spec.Dark && previous > name {
				t.Fatalf("theme group is not sorted: %q before %q", previous, name)
			}
		}
		previous = name
	}
}

func TestThemePickerPreviewCancelRestoresSelection(t *testing.T) {
	m := compactCmdModel()
	m.cfg.Theme = "dark"
	m.applyTheme("dark")
	m.command("/theme")

	tm, _ := m.paletteKey(tea.KeyMsg{Type: tea.KeyDown})
	m = tm.(*model)
	if CurrentTheme() == "dark" || m.cfg.Theme != "dark" {
		t.Fatalf("preview changed persistence or failed: current=%q config=%q", CurrentTheme(), m.cfg.Theme)
	}
	tm, _ = m.paletteKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = tm.(*model)
	if CurrentTheme() != "dark" || m.cfg.Theme != "dark" || m.palette != nil {
		t.Fatalf("cancel did not restore dark: current=%q config=%q", CurrentTheme(), m.cfg.Theme)
	}
	setSchemeOverride("")
}

func TestNamedThemeCommandPersistsAndInvalidatesMarkdown(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := compactCmdModel()
	before := themeGeneration()
	m.command("/theme dracula")
	if CurrentTheme() != "dracula" || m.cfg.Theme != "dracula" {
		t.Fatalf("named theme not selected: current=%q config=%q", CurrentTheme(), m.cfg.Theme)
	}
	if themeGeneration() <= before {
		t.Fatal("selecting a named theme did not invalidate render caches")
	}
	out := renderMarkdown("body `code`", 60)
	if !strings.Contains(out, "38;2;") {
		t.Fatalf("named theme did not reach markdown rendering: %q", out)
	}
	m.setTheme("dark")
	setSchemeOverride("")
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
		v := m.View()
		for i, l := range strings.Split(v, "\n") {
			if strings.Contains(l, "\x1b[m") {
				t.Errorf("%s: row %d bare SGR: %q", theme, i, l)
			}
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
