package tui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/muesli/termenv"
)

// TestGrokLogo: the braille wordmark renders the Grok Build art (full and
// small tiers) with real braille glyphs.
func TestGrokLogo(t *testing.T) {
	full := grokLogo(24)
	if got := len(strings.Split(full, "\n")); got != len(gkLogoFull) {
		t.Fatalf("full logo rows = %d, want %d", got, len(gkLogoFull))
	}
	if !strings.Contains(full, "⣠") || !strings.Contains(full, "⡿") {
		t.Fatalf("full logo missing braille art:\n%s", full)
	}
	small := grokLogo(8)
	if got := len(strings.Split(small, "\n")); got != len(gkLogoSmall) {
		t.Fatalf("small logo rows = %d, want %d", got, len(gkLogoSmall))
	}
}

// TestGrokHome: the empty transcript centers the braille wordmark in the area.
func TestGrokHome(t *testing.T) {
	out := grokHome(60, 20)
	if lipgloss.Height(out) != 20 {
		t.Fatalf("home height = %d, want 20", lipgloss.Height(out))
	}
	for _, l := range strings.Split(out, "\n") {
		if lipgloss.Width(l) > 60 {
			t.Fatalf("home row wider than 60: %d", lipgloss.Width(l))
		}
	}
}

// TestGrokPrompt: the prompt box is the rounded chrome — ╭─╮ top, │ sides,
// ╰─╯ bottom — with the model name inside the bottom border.
func TestGrokPrompt(t *testing.T) {
	m := &model{width: 60, modelName: "grok-code-fast", provName: "xai", input: newInput()}
	m.agent = &agent.Agent{}
	m.applyUIMode(grokMode)
	t.Cleanup(func() { m.applyUIMode("") })
	out := m.grokPrompt(m.input.View(), 60)
	lines := strings.Split(out, "\n")
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasSuffix(ansi.Strip(lines[0]), "╮") {
		t.Fatalf("top border wrong: %q", ansi.Strip(lines[0]))
	}
	last := ansi.Strip(lines[len(lines)-1])
	if !strings.HasPrefix(last, "╰") || !strings.HasSuffix(last, "╯") {
		t.Fatalf("bottom border wrong: %q", last)
	}
	if !strings.Contains(last, "grok-code-fast") {
		t.Fatalf("model name should sit inside the bottom border: %q", last)
	}
	// every row is exactly the content width
	for i, l := range lines {
		if lipgloss.Width(l) != 60 {
			t.Fatalf("row %d width = %d, want 60 (%q)", i, lipgloss.Width(l), ansi.Strip(l))
		}
	}
}

// TestGrokPromptTitle: the session title rides the top divider, right-aligned.
func TestGrokPromptTitle(t *testing.T) {
	m := &model{width: 60, modelName: "grok-code-fast", sessTitle: "fix the flaky test", input: newInput()}
	m.agent = &agent.Agent{}
	m.applyUIMode(grokMode)
	t.Cleanup(func() { m.applyUIMode("") })
	out := m.grokPrompt(m.input.View(), 60)
	top := ansi.Strip(strings.Split(out, "\n")[0])
	if !strings.Contains(top, "fix the flaky test") {
		t.Fatalf("top border should carry the session title: %q", top)
	}
}

// TestGrokUserCard: a user turn is a full-width band with the ❯ prefix.
func TestGrokUserCard(t *testing.T) {
	out := grokUserCard("hello there", 40, false)
	lines := strings.Split(out, "\n")
	if !strings.Contains(ansi.Strip(lines[0]), "❯ hello there") {
		t.Fatalf("user card missing ❯ prefix: %q", ansi.Strip(lines[0]))
	}
	for i, l := range lines {
		if lipgloss.Width(l) != 40 {
			t.Fatalf("row %d width = %d, want 40 (the band spans the column)", i, lipgloss.Width(l))
		}
	}
}

// TestGrokToolRow: completed tool calls carry the ◆ bullet and the verb forms.
func TestGrokToolRow(t *testing.T) {
	cases := map[string]string{
		"read":  "Read",
		"bash":  "$",
		"edit":  "Edit",
		"grep":  "Search",
		"write": "Edit",
	}
	for name, want := range cases {
		out := ansi.Strip(grokToolRow(name, `{"file_path":"a.go","command":"ls","pattern":"x"}`, false))
		if !strings.Contains(out, "◆") || !strings.Contains(out, want) {
			t.Fatalf("%s row = %q, want ◆ and %q", name, out, want)
		}
	}
	if out := ansi.Strip(grokToolRow("read", `{"file_path":"a.go"}`, true)); !strings.Contains(out, "◆") {
		t.Fatalf("failed row should keep the bullet: %q", out)
	}
}

// TestGrokToolPending: queued/running rows render muted with the ◆ bullet.
func TestGrokToolPending(t *testing.T) {
	out := ansi.Strip(grokToolPending("bash", `{"command":"make test"}`))
	if !strings.Contains(out, "◆ $") || !strings.Contains(out, "make test") {
		t.Fatalf("pending bash row = %q", out)
	}
}

// TestGrokToolResult: a result collapses to a muted ↳ hint; expands to the body.
func TestGrokToolResult(t *testing.T) {
	lines := []string{"a", "b", "c", "d"}
	out := ansi.Strip(grokToolResult(lines, false, false, false, 60))
	if !strings.Contains(out, "4 lines") {
		t.Fatalf("collapsed result = %q", out)
	}
	out = ansi.Strip(grokToolResult(lines, true, false, false, 60))
	if !strings.Contains(out, "a") || !strings.Contains(out, "d") {
		t.Fatalf("expanded result = %q", out)
	}
}

// TestGrokThoughtBlock: reasoning collapses to "Thought for Xs" and expands.
func TestGrokThoughtBlock(t *testing.T) {
	old := gkActive
	t.Cleanup(func() { gkActive = old })
	gkActive = true
	b := block{kind: blockThought, text: "some reasoning", live: "2.4s"}
	out := ansi.Strip(b.render(60))
	if !strings.Contains(out, "Thought") || !strings.Contains(out, "2.4s") {
		t.Fatalf("collapsed thought = %q", out)
	}
	if strings.Contains(out, "some reasoning") {
		t.Fatal("collapsed thought must hide the body")
	}
	b.expanded = true
	b.stale = true
	out = ansi.Strip(b.render(60))
	if !strings.Contains(out, "some reasoning") {
		t.Fatalf("expanded thought = %q", out)
	}
}

// TestGrokAssistantFlushLeft: assistant blocks render flush-left (no bullet).
func TestGrokAssistantFlushLeft(t *testing.T) {
	old := gkActive
	t.Cleanup(func() { gkActive = old })
	gkActive = true
	b := block{kind: blockAssistant, text: "the answer"}
	out := ansi.Strip(b.render(60))
	if strings.Contains(out, "●") || strings.Contains(out, "▣") {
		t.Fatalf("grok assistant must not carry a bullet: %q", out)
	}
	if !strings.HasPrefix(out, "the answer") {
		t.Fatalf("assistant body should be flush-left: %q", out)
	}
}

// TestGrokFmtTokens / TestFmtGrokDur: the context bar + turn timer formats.
func TestGrokFmtTokens(t *testing.T) {
	for in, want := range map[int]string{
		0: "0", 12: "12", 999: "999",
		1200: "1.2K", 12000: "12K", 999000: "999K",
		1200000: "1.2M", 12000000: "12M",
	} {
		if got := gkFmtTokens(in); got != want {
			t.Fatalf("gkFmtTokens(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFmtGrokDur(t *testing.T) {
	if got := fmtGrokDur(200 * time.Millisecond); got != "0.2s" {
		t.Fatalf("200ms = %q", got)
	}
	if got := fmtGrokDur(80 * time.Second); got != "1m20s" {
		t.Fatalf("80s = %q", got)
	}
}

// TestGrokStatusIdle: the idle status row is cwd-left, usage+model-right.
func TestGrokStatusIdle(t *testing.T) {
	m := &model{width: 80, modelName: "grok-code-fast", provName: "xai"}
	m.agent = &agent.Agent{}
	out := ansi.Strip(m.grokStatus())
	if !strings.Contains(out, "grok-code-fast") {
		t.Fatalf("idle status should name the model: %q", out)
	}
	if lipgloss.Width(out) > 80 {
		t.Fatalf("status wider than width: %d", lipgloss.Width(out))
	}
}

// TestGrokStatusBusy: the busy status row is grok's turn status — braille
// spinner, "Thinking…", the ⇣ token count, and [stop].
func TestGrokStatusBusy(t *testing.T) {
	m := &model{width: 80, modelName: "grok-code-fast", busy: true}
	m.agent = &agent.Agent{}
	m.now = time.Now
	m.turnStart = time.Now().Add(-2 * time.Second)
	m.spin.Spinner = gkBraille
	out := ansi.Strip(m.grokStatus())
	for _, want := range []string{"Thinking", "[stop]"} {
		if !strings.Contains(out, want) {
			t.Fatalf("busy status missing %q: %q", want, out)
		}
	}
	if lipgloss.Width(out) > 80 {
		t.Fatalf("busy status wider than width: %d", lipgloss.Width(out))
	}
}

// TestGrokDialogRows: the command palette dialog renders grok-style and every
// row is exactly the dialog width.
func TestGrokDialogRows(t *testing.T) {
	m := &model{width: 80, cfg: &config.Config{}}
	m.palette = &palette{
		items: []paletteItem{
			{title: "Model", category: "Agent", dynHint: func(*model) string { return "/model" }},
			{title: "Theme", category: "Display"},
		},
	}
	out := strings.Join(m.gkDialogRows(), "\n")
	for _, want := range []string{"Commands", "esc", "Search", "Agent", "Display", "Model", "/model", "Theme"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dialog missing %q:\n%s", want, out)
		}
	}
	for i, r := range m.gkDialogRows() {
		if lipgloss.Width(r) != 64 {
			t.Fatalf("row %d width = %d, want 64", i, lipgloss.Width(r))
		}
	}
	// the selected row carries the ▏ selection bar
	m.palette.idx = 0
	if out := strings.Join(m.gkDialogRows(), "\n"); !strings.Contains(out, "▏") {
		t.Fatalf("selected row should carry the ▏ bar:\n%s", ansi.Strip(out))
	}
	// filter typed: replaces the Search placeholder
	m.palette.filter = "the"
	if out := strings.Join(m.gkDialogRows(), "\n"); !strings.Contains(out, "the") || strings.Contains(out, "Search") {
		t.Fatalf("filter should replace Search placeholder:\n%s", out)
	}
	// no matches
	m.palette.items = nil
	if out := strings.Join(m.gkDialogRows(), "\n"); !strings.Contains(out, "No results found") {
		t.Fatal("empty dialog should say No results found")
	}
}

// TestGrokMsgActionRows: the Message Actions dialog.
func TestGrokMsgActionRows(t *testing.T) {
	m := &model{width: 80, msgActions: &msgActions{}}
	out := strings.Join(m.gkMsgActionRows(), "\n")
	for _, want := range []string{"Message Actions", "esc", "Search", "Revert", "Copy", "Fork"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dialog missing %q:\n%s", want, out)
		}
	}
}

// TestGrokMenuOverlay: the completion popup anchors above the input box and
// never changes the frame's line count.
func TestGrokMenuOverlay(t *testing.T) {
	m := &model{width: 60, termWidth: 64, uiMode: grokMode, inputBodyOff: 20}
	m.menu = &menu{cands: []cand{{Text: "/auth", Desc: "connect"}, {Text: "/cd", Desc: "chdir"}}}
	backdrop := strings.TrimSuffix(strings.Repeat(strings.Repeat("x", 64)+"\n", 30), "\n")
	out := m.gkMenuOverlay(backdrop)
	lines := strings.Split(out, "\n")
	if len(lines) != 30 {
		t.Fatalf("overlay changed line count: %d", len(lines))
	}
	joined := strings.Join(lines[:20], "\n")
	if !strings.Contains(joined, "/auth") || !strings.Contains(joined, "1/2") {
		t.Fatalf("menu not above the input box:\n%s", out)
	}
	if !strings.Contains(out, "▏") {
		t.Fatalf("selected menu row should carry the ▏ bar:\n%s", ansi.Strip(out))
	}
}

// TestGrokToastSplice: the toast paints top-right without changing line count.
func TestGrokToastSplice(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	m := &model{termWidth: 80, now: func() time.Time { return now }}
	m.toast = "Copied to clipboard"
	backdrop := strings.TrimSuffix(strings.Repeat(strings.Repeat("x", 80)+"\n", 10), "\n")
	out := m.gkSpliceToast(backdrop)
	if !strings.Contains(out, "Copied to clipboard") || !strings.Contains(out, "┃") {
		t.Fatalf("toast not spliced:\n%s", out)
	}
	if len(strings.Split(out, "\n")) != 10 {
		t.Fatal("splice must not change line count")
	}
}

// TestGrokAttribution: the per-response footer is the teal model + duration.
func TestGrokAttribution(t *testing.T) {
	m := &model{modelName: "grok-code-fast"}
	m.agent = &agent.Agent{}
	out := ansi.Strip(m.grokAttribution(2400 * time.Millisecond))
	if !strings.Contains(out, "grok-code-fast") || !strings.Contains(out, "2.4s") {
		t.Fatalf("attribution = %q", out)
	}
}

// TestApplyUIModeGrok: entering grok mode sets the flag, the ❯ prompt, the
// placeholder, and the braille spinner; leaving restores whip defaults.
func TestApplyUIModeGrok(t *testing.T) {
	m := &model{input: newInput()}
	m.applyUIMode(grokMode)
	t.Cleanup(func() { m.applyUIMode("") })
	if m.uiMode != grokMode || !gkActive || ocActive {
		t.Fatalf("grok: uiMode=%q gkActive=%v ocActive=%v", m.uiMode, gkActive, ocActive)
	}
	if m.input.Prompt != "❯ " {
		t.Fatalf("grok prompt = %q, want ❯", m.input.Prompt)
	}
	if m.input.Placeholder != "Build anything" {
		t.Fatalf("grok placeholder = %q", m.input.Placeholder)
	}
	if len(m.spin.Spinner.Frames) != len(gkBraille.Frames) || m.spin.Spinner.Frames[0] != "⠋" {
		t.Fatalf("grok spinner should be the braille frames, got %v", m.spin.Spinner.Frames)
	}
	m.applyUIMode("")
	if m.uiMode != "" || gkActive || m.input.Prompt != "┃ " {
		t.Fatalf("default: uiMode=%q gkActive=%v prompt=%q", m.uiMode, gkActive, m.input.Prompt)
	}
	if m.input.Placeholder != whipPlaceholder {
		t.Fatalf("default placeholder = %q", m.input.Placeholder)
	}
}

// TestSetUIModeGrokCycle: the mode cycles default → opencode → grok → default
// and persists.
func TestSetUIModeGrokCycle(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := &model{cfg: &config.Config{}, input: newInput()}
	m.now = time.Now
	t.Cleanup(func() { m.applyUIMode("") })

	if cmd := m.setUIMode(grokMode); cmd == nil {
		t.Fatal("entering grok should return the alt-screen command")
	}
	if m.cfg.UIMode != grokMode || m.uiMode != grokMode {
		t.Fatalf("grok mode not applied/persisted: cfg=%q ui=%q", m.cfg.UIMode, m.uiMode)
	}
	if cmd := m.setUIMode(""); cmd == nil {
		t.Fatal("leaving grok should return the exit-alt-screen command")
	}
	if m.cfg.UIMode != "" {
		t.Fatalf("default should clear the config, got %q", m.cfg.UIMode)
	}
	// a bogus value normalizes to default
	m.setUIMode("bogus")
	if m.uiMode != "" {
		t.Fatalf("bogus mode should normalize to default, got %q", m.uiMode)
	}
}

// TestUIModeLabelGrok: the label names all three modes.
func TestUIModeLabelGrok(t *testing.T) {
	if uiModeLabel(grokMode) != "grok" {
		t.Fatal("grok label")
	}
	if uiModeLabel(opencodeMode) != "opencode" {
		t.Fatal("opencode label")
	}
	if uiModeLabel("") != "default" {
		t.Fatal("default label")
	}
}

// TestGrokChromeOffsets: the full-screen mouse math offsets.
func TestGrokChromeOffsets(t *testing.T) {
	m := &model{uiMode: grokMode}
	if m.vpTopRows() != 0 {
		t.Fatalf("grok vpTopRows = %d, want 0", m.vpTopRows())
	}
	if m.vpXOff() != grokMargin {
		t.Fatalf("grok vpXOff = %d, want %d", m.vpXOff(), grokMargin)
	}
}

// TestApplyUIModeGrokRuntimeMouseToggle: a LIVE switch into grok arms ?1003
// all-motion tracking; leaving drops it.
func TestApplyUIModeGrokRuntimeMouseToggle(t *testing.T) {
	saved := tuiRunning
	t.Cleanup(func() { tuiRunning = saved; gkActive = false })
	tuiRunning = true

	r, w, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	m := &model{input: newInput(), mouseOn: true}
	m.applyUIMode(grokMode)
	m.applyUIMode("")
	w.Close()
	os.Stdout = stdout
	buf := make([]byte, 64)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "\x1b[?1003h") || !strings.Contains(out, "\x1b[?1003l") {
		t.Fatalf("runtime toggle should emit ?1003h then ?1003l, got %q", out)
	}
}

// TestGrokColorsThemeResolved: the palette resolves against whip's detected
// theme — dark values on dark terminals, light on light, and a safe neutral
// (or no fill) when the background is unknown.
func TestGrokColorsThemeResolved(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	mdMu.Lock()
	sl, sk := mdLight, mdKnown
	mdMu.Unlock()
	t.Cleanup(func() { mdMu.Lock(); mdLight, mdKnown = sl, sk; mdMu.Unlock() })

	mdMu.Lock()
	mdLight, mdKnown = false, true
	mdMu.Unlock()
	if got := gkTextCol(); got != lipgloss.Color("#e1e1e1") {
		t.Fatalf("dark text_primary = %v", got)
	}
	mdMu.Lock()
	mdLight, mdKnown = true, true
	mdMu.Unlock()
	if got := gkTextCol(); got != lipgloss.Color("#262626") {
		t.Fatalf("light text_primary = %v", got)
	}
	mdMu.Lock()
	mdLight, mdKnown = false, false
	mdMu.Unlock()
	if got := gkBandBg(); got != (lipgloss.NoColor{}) {
		t.Fatalf("unknown-bg band must be NoColor, got %v", got)
	}
	if got := gkMutedCol(); got != lipgloss.Color("8") {
		t.Fatalf("unknown-bg muted should fall back to ANSI 8, got %v", got)
	}
}
