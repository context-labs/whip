package tui

import (
	"strings"
	"testing"
)

// bgQuery outside tmux: bare OSC 11, the ?996 theme-report query, then the
// CSI 6n terminator.
func TestBgQueryPlain(t *testing.T) {
	if got := bgQuery(false); got != "\x1b]11;?\x1b\\\x1b[?996n\x1b[6n" {
		t.Fatalf("plain query wrong: %q", got)
	}
}

// bgQuery inside tmux: the OSC 11 goes out twice — bare (tmux ≥3.4 answers it
// itself) and DCS-passthrough-wrapped with doubled ESCs (for allow-passthrough
// setups) — and the CSI 6n terminator stays OUTSIDE the wrapper so tmux always
// answers it and the read never blocks on passthrough being off.
func TestBgQueryTmux(t *testing.T) {
	got := bgQuery(true)
	if !strings.HasPrefix(got, "\x1b]11;?\x1b\\") {
		t.Fatalf("must start with a bare OSC 11 for tmux itself to answer: %q", got)
	}
	if !strings.Contains(got, "\x1bPtmux;\x1b\x1b]11;?\x1b\x1b\\\x1b\\") {
		t.Fatalf("must include the passthrough-wrapped OSC 11 (ESCs doubled): %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[?996n\x1b[6n") {
		t.Fatalf("996 theme query then the CSI 6n terminator must be last and unwrapped: %q", got)
	}
}

// When no terminal query succeeded, the scheme comes from COLORFGBG or stays
// neutral. Inside tmux the ONLY acceptable outcomes are COLORFGBG or neutral —
// never an assumed dark. (The regression: termenv's fallback can't query
// through tmux and silently assumes dark, which painted the dark palette on
// light terminals.)
func TestFallbackScheme(t *testing.T) {
	// COLORFGBG wins when parseable ("fg;bg", bg 7+ = light)
	if light, ok, _ := fallbackScheme(true, "0;15"); !ok || !light {
		t.Fatal("COLORFGBG 0;15 must resolve light")
	}
	if light, ok, _ := fallbackScheme(false, "15;0"); !ok || light {
		t.Fatal("COLORFGBG 15;0 must resolve dark")
	}
	// unparseable or missing → not-ok (caller goes neutral), with a hint
	// naming the actual remedy per environment
	if _, ok, how := fallbackScheme(true, ""); ok || !strings.Contains(how, "tmux") {
		t.Fatalf("tmux, no signal: must be neutral with a tmux hint, got ok=%v how=%q", ok, how)
	}
	if _, ok, how := fallbackScheme(false, "junk"); ok || !strings.Contains(how, "undetermined") {
		t.Fatalf("no signal: must be neutral, got ok=%v how=%q", ok, how)
	}
}

// While bubbletea runs, theme re-detection (/theme auto, config-watcher sync)
// must NEVER query the tty: the raw-mode query flips the shared terminal to
// VMIN=0 and bubbletea's concurrent input read then returns a spurious EOF —
// its reader exits silently and the session stops seeing input forever (the
// frozen-whip bug). Runtime detection reuses the startup query's answer, or
// COLORFGBG, or the neutral theme.
func TestRuntimeDetectionNeverQueriesTTY(t *testing.T) {
	t.Setenv("WHIP_THEME", "")
	t.Setenv("COLORFGBG", "")
	t.Setenv("TMUX", "1") // any env: the gate must hold everywhere
	tuiRunning = true
	defer func() { tuiRunning = false; bgCache = bgResult{} }()

	// with a cached startup answer, re-detection returns it
	bgCache = bgResult{light: true, valid: true}
	if how := detectColorScheme(); how != "terminal query (cached from startup)" {
		t.Fatalf("runtime detection must reuse the startup query, got %q", how)
	}
	mdMu.Lock()
	light := mdLight
	mdMu.Unlock()
	if !light {
		t.Fatal("cached light answer must apply the light scheme")
	}

	// without a cache, COLORFGBG decides
	bgCache = bgResult{}
	t.Setenv("COLORFGBG", "15;0")
	if how := detectColorScheme(); !strings.Contains(how, "COLORFGBG") {
		t.Fatalf("runtime detection without cache must use COLORFGBG, got %q", how)
	}

	// and with no signal at all: neutral, never a dark guess
	t.Setenv("COLORFGBG", "")
	if how := detectColorScheme(); !strings.Contains(how, "undetermined") {
		t.Fatalf("runtime detection with no signal must stay neutral, got %q", how)
	}
}
