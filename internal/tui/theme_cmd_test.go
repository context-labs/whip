package tui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

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

func TestThemeLabelsUseDisplayNames(t *testing.T) {
	if got := themeLabel("tokyonight"); got != "☾  Tokyo Night" {
		t.Fatalf("Tokyo Night label = %q", got)
	}
	if got := themeLabel("neon-city-dark"); got != "☾  Neon City Dark" {
		t.Fatalf("Neon City Dark label = %q", got)
	}
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

func TestRenderBodyTextResumesAfterNestedStyle(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	SetLightTheme(false)

	got := renderBodyText("plain " + errStyle.Render("error") + " tail")
	want := "\x1b[0m\x1b[38;2;238;238;238m tail"
	if !strings.Contains(got, want) {
		t.Fatalf("body color not resumed after nested reset: %q", got)
	}
}

func TestLiveStreamUsesBodyTextStyle(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	SetLightTheme(false)

	m := compactCmdModel()
	m.width, m.height, m.busy, m.current = 80, 30, true, "live text"
	got := m.currentViewCapped()
	if !strings.Contains(got, "\x1b[38;2;238;238;238m") {
		t.Fatalf("live stream lacks body foreground: %q", got)
	}
}

func TestBlockRenderCachesBodyTextStyle(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	SetLightTheme(false)

	b := block{text: "plain " + errStyle.Render("error") + " tail"}
	first := b.renderAt(80)
	if !strings.Contains(first, "\x1b[38;2;238;238;238m") {
		t.Fatalf("block lacks body foreground: %q", first)
	}
	if b.stale {
		t.Fatal("rendered block should have a warm cache")
	}
	if second := b.renderAt(80); second != first {
		t.Fatalf("cached render changed: first=%q second=%q", first, second)
	}
}

func TestThemeSwitchRefreshesPlainTextAndInput(t *testing.T) {
	m := compactCmdModel()
	m.Update(mkWinSize(80, 30))
	m.input.SetValue("plain input")
	m.append("plain transcript")

	m.applyTheme("light")
	lightBody := currentTheme().Body.GetForeground()
	lightInput := m.input.FocusedStyle.Text.GetForeground()
	if lightBody != currentTheme().Terminal(currentTheme().Text) || lightInput != lightBody {
		t.Fatalf("light text styles not applied: body=%v input=%v", lightBody, lightInput)
	}

	m.applyTheme("dark")
	darkBody := currentTheme().Body.GetForeground()
	if darkBody != currentTheme().Terminal(currentTheme().Text) || m.input.FocusedStyle.Text.GetForeground() != darkBody {
		t.Fatalf("dark text styles not applied: body=%v input=%v", darkBody, m.input.FocusedStyle.Text.GetForeground())
	}
	if darkBody == lightBody {
		t.Fatal("theme switch did not change the plain text foreground")
	}
	setSchemeOverride("")
}

func TestThemePickerPreviewPreservesOpenCodeState(t *testing.T) {
	oldOC := ocActive
	t.Cleanup(func() { ocActive = oldOC })
	m := compactCmdModel()
	m.uiMode = opencodeMode
	m.applyUIMode(opencodeMode)
	m.command("/theme")
	placeholder := m.input.Placeholder
	spinnerFPS := m.spin.Spinner.FPS
	spinnerFrames := append([]string(nil), m.spin.Spinner.Frames...)

	m.previewTheme("light")
	if m.input.Placeholder != placeholder {
		t.Fatalf("preview changed placeholder: got %q, want %q", m.input.Placeholder, placeholder)
	}
	if m.spin.Spinner.FPS != spinnerFPS || !slices.Equal(m.spin.Spinner.Frames, spinnerFrames) {
		t.Fatal("preview rebuilt the spinner")
	}
	m.setTheme("dark")
	setSchemeOverride("")
}

func TestThemePickerPreviewIgnoresAppearancePoll(t *testing.T) {
	m := compactCmdModel()
	m.cfg.Theme = ""
	m.command("/theme")
	m.previewTheme("dark")
	before := len(m.blocks)

	m.Update(themeSyncMsg{light: true, ok: true})
	if CurrentTheme() != "dark" || len(m.blocks) != before {
		t.Fatalf("appearance poll changed preview: theme=%q blocks=%d, want dark/%d", CurrentTheme(), len(m.blocks), before)
	}
	if _, cmd := m.Update(themePollMsg{}); cmd == nil {
		t.Fatal("preview should keep the appearance tick alive")
	}
	m.setTheme("dark")
	setSchemeOverride("")
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
