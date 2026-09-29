package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// On a light terminal the markdown body must render in the light style's
// dark text color (#1a1a1a), not the dark style's #eeeeee (near-invisible on white).
func TestLightThemeRendersDarkText(t *testing.T) {
	SetLightTheme(true)
	defer SetLightTheme(false)
	out := renderMarkdown("plain body text", 60)
	if !strings.Contains(out, "\x1b[38;2;26;26;26m") {
		t.Errorf("light theme should render body in the light text color, got %q", out)
	}
	if strings.Contains(out, "\x1b[38;2;238;238;238m") {
		t.Errorf("light theme must not use the dark text color: %q", out)
	}
	// width behavior unchanged
	for l := range strings.SplitSeq(out, "\n") {
		if ansi.StringWidth(l) > 60 {
			t.Errorf("light render overflow: %q", l)
		}
	}
}

// When the background can't be determined (auto, no signal), markdown must
// render in the neutral default style — NOT a forced dark/light guess — so
// body text carries no hardcoded color (stays at the terminal default).
func TestUnknownThemeIsNeutral(t *testing.T) {
	setSchemeOverride("") // detection mode: no pinned theme from an earlier test
	SetUnknownTheme()
	defer SetLightTheme(false)
	out := renderMarkdown("plain body text", 60)
	// dark style would force body color 252; light forces 234. Neutral: neither.
	if strings.Contains(out, "\x1b[38;5;252m") || strings.Contains(out, "\x1b[38;5;234m") {
		t.Errorf("unknown theme should not force a body color: %q", out)
	}
	if got := CurrentTheme(); got != "auto" {
		t.Errorf("CurrentTheme = %q, want auto", got)
	}
}

// The neutral (unknown-background) style must still RENDER markdown — bold
// applied, box-drawing table rules, only terminal-palette ANSI colors — not
// glamour's ASCII fallback (kept ** markers, raw pipe tables, zero styling),
// which users read as "markdown rendering is broken".
func TestUnknownThemeStillRendersMarkdown(t *testing.T) {
	SetUnknownTheme()
	defer SetLightTheme(false)
	src := "## Head\n\n**bold** text\n\n| A | B |\n|---|---|\n| 1 | 2 |"
	out := renderMarkdown(src, 60)
	if strings.Contains(ansi.Strip(out), "**") {
		t.Errorf("neutral style left literal ** markers (ASCII style?): %q", out)
	}
	if !strings.Contains(out, "1mbold") { // bold, possibly combined with a palette color (\x1b[33;1m)
		t.Errorf("neutral style should render bold: %q", out)
	}
	if !strings.Contains(out, "─") {
		t.Errorf("neutral style should draw the table header rule: %q", out)
	}
	if strings.Contains(out, "\x1b[38;5;") || strings.Contains(out, "\x1b[38;2;") {
		t.Errorf("neutral style must only use basic ANSI colors: %q", out)
	}
}

// Every styled rendered line must end in a full reset. Glamour puts a line's
// closing reset inside the right-padding stripLinePadding removes (heading
// lines especially); left un-reset, a blue heading bleeds into every line
// below it — the whole table after "## Table" rendered blue+bold.
func TestRenderedLinesSelfTerminate(t *testing.T) {
	SetUnknownTheme()
	defer SetLightTheme(false)
	out := renderMarkdown("## Head\n\n| A | B |\n|---|---|\n| 1 | 2 |", 60)
	for l := range strings.SplitSeq(out, "\n") {
		if strings.Contains(l, "\x1b[") && !strings.HasSuffix(l, "\x1b[0m") {
			t.Errorf("styled line not reset-terminated: %q", l)
		}
	}
}

// After an unknown (neutral) render, an explicit theme switch must re-render
// in the new theme — the renderer cache must key on the known/unknown state.
func TestThemeSwitchAfterUnknown(t *testing.T) {
	SetUnknownTheme()
	_ = renderMarkdown("plain body text", 60) // build + cache neutral renderer
	SetLightTheme(true)
	defer SetLightTheme(false)
	out := renderMarkdown("plain body text", 60)
	if !strings.Contains(out, "\x1b[38;2;26;26;26m") {
		t.Errorf("switching unknown→light should re-render in the light text color: %q", out)
	}
}
