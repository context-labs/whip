package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
	"github.com/muesli/termenv"
)

func TestOpencodeLogo(t *testing.T) {
	logo := opencodeLogo()
	lines := strings.Split(logo, "\n")
	if len(lines) != 4 {
		t.Fatalf("logo has %d lines, want 4", len(lines))
	}
	if !strings.Contains(logo, "█") || !strings.Contains(logo, "▀") {
		t.Fatal("logo missing block glyphs")
	}
}

func TestPaletteChrome(t *testing.T) {
	m := &model{}
	if got := m.paletteChrome("x"); got != "x" {
		t.Fatalf("default mode should pass through, got %q", got)
	}
	m.uiMode = opencodeMode
	if got := m.paletteChrome("x"); !strings.HasPrefix(got, "   ") {
		t.Fatalf("opencode mode should indent, got %q", got)
	}
}

func TestSetThemeRefreshesOpencodeInputStyles(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir()) // keep cfg.Save() off the real config
	mdMu.Lock()
	sl, sk := mdLight, mdKnown
	mdMu.Unlock()
	t.Cleanup(func() { mdMu.Lock(); mdLight, mdKnown = sl, sk; mdMu.Unlock() })

	m := &model{cfg: &config.Config{}, input: newInput()}
	t.Cleanup(func() { m.applyUIMode("") }) // don't leak ocActive into other tests
	mdMu.Lock()
	mdKnown = false // start unknown: styles bake NoColor
	mdMu.Unlock()
	m.applyUIMode(opencodeMode)
	m.setTheme("light") // must re-bake the input styles with the light palette
	if got := m.input.FocusedStyle.Placeholder.GetBackground(); got != lipgloss.Color("#e1e1e1") {
		t.Fatalf("placeholder bg after /theme light = %v, want #e1e1e1", got)
	}
}

func TestStartupReportUnknownBgNotice(t *testing.T) {
	mdMu.Lock()
	sl, sk := mdLight, mdKnown
	mdMu.Unlock()
	t.Cleanup(func() { mdMu.Lock(); mdLight, mdKnown = sl, sk; mdMu.Unlock() })

	mdMu.Lock()
	mdKnown = false
	mdMu.Unlock()
	m := &model{uiMode: opencodeMode}
	m.startupReport()
	if len(m.blocks) == 0 || !strings.Contains(m.blocks[len(m.blocks)-1].text, "background unknown") {
		t.Fatal("unknown background should append an actionable notice")
	}

	mdMu.Lock()
	mdKnown, mdLight = true, true
	mdMu.Unlock()
	m2 := &model{uiMode: opencodeMode}
	m2.startupReport()
	if len(m2.blocks) != 0 {
		t.Fatalf("known background should append nothing, got %d blocks", len(m2.blocks))
	}
}

func TestOcDialogRows(t *testing.T) {
	m := &model{width: 80, cfg: &config.Config{}} // cfg: the theme sub-panel reads cfg.Theme
	m.palette = &palette{
		items: []paletteItem{
			{title: "Model", category: "Agent", dynHint: func(*model) string { return "/model" }},
			{title: "Theme", category: "Display"},
		},
	}
	out := strings.Join(m.ocDialogRows(), "\n")
	for _, want := range []string{"Commands", "esc", "Search", "Agent", "Display", "Model", "/model", "Theme"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dialog missing %q:\n%s", want, out)
		}
	}
	// every row is exactly the dialog width
	for i, r := range m.ocDialogRows() {
		if lipgloss.Width(r) != 64 {
			t.Fatalf("row %d width = %d, want 64", i, lipgloss.Width(r))
		}
	}
	// filter typed: replaces the Search placeholder
	m.palette.filter = "the"
	if out := strings.Join(m.ocDialogRows(), "\n"); !strings.Contains(out, "the") || strings.Contains(out, "Search") {
		t.Fatalf("filter should replace Search placeholder:\n%s", out)
	}
	// no matches
	m.palette.items = nil
	if out := strings.Join(m.ocDialogRows(), "\n"); !strings.Contains(out, "No results found") {
		t.Fatal("empty palette should say No results found")
	}
	// sub-panel renders inside the box with a breadcrumb header
	m.palette.stack = []*ppanel{{kind: panelTheme, title: "Theme", list: []string{"auto", "light", "dark"}}}
	if out := strings.Join(m.ocDialogRows(), "\n"); !strings.Contains(out, "Commands › Theme") {
		t.Fatalf("sub-panel missing breadcrumb:\n%s", out)
	}
	m.palette.stack = nil

	// narrow terminal: the left/right gap clamps to 1 instead of going negative
	m.width = 20
	m.palette.items = []paletteItem{
		{title: "First", category: "Agent"},
		{title: "A very long item title here", category: "Agent", dynHint: func(*model) string { return "/hint" }},
	}
	m.palette.filter = ""
	if out := strings.Join(m.ocDialogRows(), "\n"); !strings.Contains(out, "Commands") {
		t.Fatal("narrow dialog should still render")
	}
}

func TestOcOverlay(t *testing.T) {
	m := &model{width: 80, termWidth: 80, height: 30}
	m.palette = &palette{items: []paletteItem{{title: "Model", category: "Agent"}}}
	backdrop := strings.TrimSuffix(strings.Repeat("session line here\n", 30), "\n")
	out := m.ocOverlay(backdrop)
	if !strings.Contains(out, "Commands") {
		t.Fatal("overlay missing dialog")
	}
	if !strings.Contains(out, "\x1b[2m") {
		t.Fatal("backdrop should be dimmed (SGR 2)")
	}
	if got := len(strings.Split(out, "\n")); got != 30 {
		t.Fatalf("overlay changed line count: %d, want 30", got)
	}
	// dialog taller than the backdrop: clipped, line count preserved
	short := "one\ntwo\nthree"
	if got := len(strings.Split(m.ocOverlay(short), "\n")); got != 3 {
		t.Fatalf("clipped overlay lines = %d, want 3", got)
	}
}

func TestOcToolRows(t *testing.T) {
	if ocToolIcon("bash") != "$" || ocToolIcon("read") != "←" || ocToolIcon("subagent") != "→" || ocToolIcon("grep") != "✱" ||
		ocToolIcon("webfetch") != "%" || ocToolIcon("websearch") != "◈" || ocToolIcon("todowrite") != "⚙" {
		t.Fatal("icon map wrong")
	}
	row := ocToolRow("bash", `{"command":"git status"}`, false)
	if !strings.HasPrefix(row, "   ") || !strings.Contains(row, "$") || !strings.Contains(row, "Bash") || !strings.Contains(row, "git status") {
		t.Fatalf("tool row = %q", row)
	}
	if failed := ocToolRow("bash", `{}`, true); !strings.Contains(failed, "Bash") {
		t.Fatalf("failed row = %q", failed)
	}
	if p := ocToolPending("subagent", `{"description":"map repo"}`); !strings.Contains(p, "~ Task — map repo") {
		t.Fatalf("pending row = %q", p)
	}
	if r := ocToolRow("subagent", `{"description":"map repo"}`, false); !strings.Contains(r, "Task") || !strings.Contains(r, "— map repo") {
		t.Fatalf("subagent row = %q", r)
	}
}

func TestOcToolResult(t *testing.T) {
	lines := []string{"a", "b", "c"}
	col := ocToolResult(lines, false, false, false, 80)
	if !strings.Contains(col, "↳ 3 lines") {
		t.Fatalf("collapsed = %q", col)
	}
	if one := ocToolResult([]string{"only"}, false, false, false, 80); !strings.Contains(one, "↳ only") {
		t.Fatalf("short results render inline: %q", one)
	}
	exp := ocToolResult(lines, true, false, false, 80)
	if !strings.Contains(exp, "↳ a") || !strings.Contains(exp, "b") {
		t.Fatalf("expanded = %q", exp)
	}
	if e := ocToolResult(lines, false, true, false, 80); !strings.Contains(e, "↳ 3 lines") {
		t.Fatalf("error collapsed = %q", e)
	}
	if h := ocToolResult(lines, false, false, true, 80); !strings.Contains(h, "↳ 3 lines") {
		t.Fatalf("hover collapsed = %q", h) // hover brightens the hint but keeps the text
	}
}

// TestApplyUIModeRuntimeMouseToggle: a LIVE mode switch (tuiRunning) must arm
// ?1003 all-motion tracking on entry and drop it on exit — Run's own ?1003h
// only covers sessions that start in opencode mode — and exit must clear any
// hover highlight the tracked motion left behind.
func TestApplyUIModeRuntimeMouseToggle(t *testing.T) {
	saved := tuiRunning
	t.Cleanup(func() { tuiRunning = saved; ocActive = false })
	tuiRunning = true

	// capture what applyUIMode writes to the terminal
	r, w, _ := os.Pipe()
	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	m := &model{input: newInput(), mouseOn: true}
	m.blocks = []block{{kind: blockAssistant, hover: true}}
	m.hoverIdx = 0
	m.applyUIMode(opencodeMode)
	m.applyUIMode("")
	w.Close()
	os.Stdout = stdout
	buf := make([]byte, 64)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "\x1b[?1003h") || !strings.Contains(out, "\x1b[?1003l") {
		t.Fatalf("runtime toggle should emit ?1003h then ?1003l, got %q", out)
	}
	if m.hoverIdx != -1 || m.blocks[0].hover || !m.blocks[0].stale {
		t.Fatalf("exit should clear the hover highlight: hoverIdx=%d hover=%v stale=%v",
			m.hoverIdx, m.blocks[0].hover, m.blocks[0].stale)
	}
}

// TestToolEndRenumbersCaches: the mid-slice result insert shifts every block
// past the header — the msgBlock map, hover index, and an open Message
// Actions dialog must all follow or they point one block short.
func TestToolEndRenumbersCaches(t *testing.T) {
	m := tasksModel("http://unused")
	m.blocks = []block{
		{kind: blockUser, text: "hi"},
		{kind: blockToolRun, toolRunning: true, toolID: "c1", toolName: "bash"},
		{kind: blockAssistant, text: "done"},
	}
	m.msgBlock = []int{0, 2}
	m.hoverIdx = 2
	m.msgActions = &msgActions{block: 2}
	m.Update(toolEndMsg{id: "c1", name: "bash", result: "ok"})
	if m.blocks[2].kind != blockTool {
		t.Fatalf("result should insert directly under its header, got kind %d", m.blocks[2].kind)
	}
	if m.msgBlock[0] != 0 || m.msgBlock[1] != 3 {
		t.Fatalf("msgBlock not renumbered: %v", m.msgBlock)
	}
	if m.hoverIdx != 3 || m.msgActions.block != 3 {
		t.Fatalf("hover/dialog indexes not renumbered: hover=%d dialog=%d", m.hoverIdx, m.msgActions.block)
	}
}

func TestVpTopRowsAndXOff(t *testing.T) {
	m := &model{}
	if m.vpTopRows() != 3 || m.vpXOff() != 0 {
		t.Fatal("default mode chrome offsets wrong")
	}
	m.uiMode = opencodeMode
	if m.vpTopRows() != 0 || m.vpXOff() != opencodeLeftMargin {
		t.Fatal("opencode mode chrome offsets wrong")
	}
}

func TestOcMsgActionRows(t *testing.T) {
	m := &model{width: 80, msgActions: &msgActions{}}
	out := strings.Join(m.ocMsgActionRows(), "\n")
	for _, want := range []string{"Message Actions", "esc", "Search", "Revert", "Copy", "Fork"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dialog missing %q:\n%s", want, out)
		}
	}
	m.msgActions.filter = "cop"
	out = strings.Join(m.ocMsgActionRows(), "\n")
	if !strings.Contains(out, "Copy") || strings.Contains(out, "Revert") || strings.Contains(out, "Search") {
		t.Fatalf("filtered dialog wrong:\n%s", out)
	}
	m.msgActions.filter = "zzz"
	if out := strings.Join(m.ocMsgActionRows(), "\n"); !strings.Contains(out, "No results found") {
		t.Fatal("no-match filter should say No results found")
	}
}

func TestMsgActionsKey(t *testing.T) {
	m := &model{width: 80, agent: &agent.Agent{}}
	m.blocks = []block{{kind: blockUser, text: "hello"}}
	m.msgActions = &msgActions{block: 0}

	key := func(s string) { m.msgActionsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}) }
	m.msgActionsKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.msgActions.sel != 1 {
		t.Fatalf("down: sel = %d", m.msgActions.sel)
	}
	m.msgActionsKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.msgActions.sel != 0 {
		t.Fatalf("up: sel = %d", m.msgActions.sel)
	}
	key("z") // filter that matches no action
	if m.msgActions.filter != "z" {
		t.Fatalf("filter = %q", m.msgActions.filter)
	}
	m.msgActionsKey(tea.KeyMsg{Type: tea.KeyEnter}) // no matches: no-op, stays open
	if m.msgActions == nil {
		t.Fatal("enter with no matches should keep the dialog open")
	}
	m.msgActionsKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if m.msgActions.filter != "" {
		t.Fatalf("backspace: filter = %q", m.msgActions.filter)
	}
	key("copy")
	m.msgActionsKey(tea.KeyMsg{Type: tea.KeyEnter}) // runs Copy, closes
	if m.msgActions != nil {
		t.Fatal("enter should close the dialog")
	}

	m.msgActions = &msgActions{}
	m.msgActionsKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.msgActions != nil {
		t.Fatal("esc should close the dialog")
	}

	// the remaining actions are nil-safe: Revert appends a notice with no
	// history, Fork reports the missing store
	msgActionList[0].run(m, 0)
	msgActionList[2].run(m, 0)
}

func TestClickOpensMsgActions(t *testing.T) {
	old := ocActive
	t.Cleanup(func() { ocActive = old })
	ocActive = true
	m := &model{width: 80, uiMode: opencodeMode, viewH: 10}
	m.blocks = []block{{kind: blockUser, text: "hello", y0: 0, y1: 2}}
	m.clickAt(5, 1)
	if m.msgActions == nil || m.msgActions.block != 0 {
		t.Fatalf("click should open Message Actions on block 0, got %+v", m.msgActions)
	}
}

// TestClickAtBounds: clicks left of the margin, past the main column (the
// sidebar/gap), or under an open completion menu must not act on the
// transcript block sharing that row.
func TestClickAtBounds(t *testing.T) {
	old := ocActive
	t.Cleanup(func() { ocActive = old })
	ocActive = true
	m := &model{width: 80, uiMode: opencodeMode, viewH: 10}
	m.blocks = []block{{kind: blockUser, text: "hello", y0: 0, y1: 2}}
	m.clickAt(1, 1) // left of the opencode margin
	m.clickAt(m.vpXOff()+m.width, 1)
	m.clickAt(120, 1) // sidebar column
	if m.msgActions != nil {
		t.Fatalf("out-of-column click opened Message Actions: %+v", m.msgActions)
	}
	m.menu = &menu{}
	m.clickAt(5, 1) // under the spliced completion popup
	if m.msgActions != nil {
		t.Fatal("click under the open menu should not reach the transcript")
	}
}

func TestUpdateHover(t *testing.T) {
	m := &model{width: 80, uiMode: opencodeMode, hoverIdx: -1}
	m.blocks = []block{
		{kind: blockUser, text: "hello", y0: 0, y1: 2},
		{kind: blockAssistant, text: "hi", y0: 4, y1: 5},
	}
	// row 0 stays inside the card even after refreshVP recomputes y0/y1 from
	// the actual (1-line) render
	m.updateHover(5, 0) // over the user card
	if m.hoverIdx != 0 || !m.blocks[0].hover {
		t.Fatalf("hover = %d, want 0", m.hoverIdx)
	}
	m.updateHover(5, 0) // unchanged: no flip
	if m.hoverIdx != 0 {
		t.Fatal("hover should stay put")
	}
	m.updateHover(5, 30) // off any card
	if m.hoverIdx != -1 || m.blocks[0].hover {
		t.Fatalf("hover off = %d", m.hoverIdx)
	}
}

func TestOcWindow(t *testing.T) {
	if lo, hi := ocWindow(3, 0, 10); lo != 0 || hi != 3 {
		t.Fatalf("small list window = %d,%d", lo, hi)
	}
	if lo, hi := ocWindow(100, 50, 10); hi-lo != 10 || 50 < lo || 50 >= hi {
		t.Fatalf("centered window = %d,%d", lo, hi)
	}
	if lo, hi := ocWindow(100, 99, 10); lo != 90 || hi != 100 {
		t.Fatalf("end window = %d,%d", lo, hi)
	}
}

func TestOcModelDialogRows(t *testing.T) {
	m := &model{width: 80, height: 40, modelName: "kimi-k3", provName: "inference-net"}
	m.mpicker = &modelPicker{items: []modelItem{
		{model: "kimi-k3", provider: "inference-net"},
		{model: "kimi-k3-fast", provider: "inference-net"},
		{model: "gpt-x", provider: "openrouter", fromCatalog: true},
	}}
	out := strings.Join(m.ocModelDialogRows(), "\n")
	for _, want := range []string{"Select model", "esc", "Search", "inference-net", "openrouter", "kimi-k3", "(new)", "enter", "● kimi-k3"} {
		if !strings.Contains(out, want) {
			t.Fatalf("model dialog missing %q:\n%s", want, out)
		}
	}
	m.mpicker.filter.query = "kimi"
	if out := strings.Join(m.ocModelDialogRows(), "\n"); strings.Contains(out, "Search") || !strings.Contains(out, "kimi") {
		t.Fatalf("query should replace Search:\n%s", out)
	}
	m.mpicker.items = nil
	m.mpicker.filter.query = ""
	if out := strings.Join(m.ocModelDialogRows(), "\n"); !strings.Contains(out, "No results found") {
		t.Fatal("empty model dialog should say No results found")
	}
}

func TestOcSessionDialogRows(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	m := &model{width: 80, height: 40, now: func() time.Time { return now }}
	m.picker = &picker{metas: []session.Meta{
		{ID: "a", Title: "Greeting", UpdatedAt: now.Add(-time.Hour)},
		{ID: "b", Title: "", UpdatedAt: now.Add(-48 * time.Hour)},
	}}
	out := strings.Join(m.ocSessionDialogRows(), "\n")
	for _, want := range []string{"Sessions", "esc", "Today", "Greeting", "(untitled)", "enter"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sessions dialog missing %q:\n%s", want, out)
		}
	}
	m.picker.metas = nil
	if out := strings.Join(m.ocSessionDialogRows(), "\n"); !strings.Contains(out, "No sessions") {
		t.Fatal("empty sessions dialog should say No sessions")
	}
}

func TestOcLeaderChord(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := &model{cfg: &config.Config{}, agent: &agent.Agent{}, input: newInput(), termWidth: 200, width: 100, uiMode: opencodeMode}
	m.now = time.Now

	if _, _, ok := m.ocLeaderChord("z"); ok {
		t.Fatal("unknown chord should not handle")
	}
	if _, _, ok := m.ocLeaderChord("b"); !ok || !m.sidebarHide {
		t.Fatal("b should hide the sidebar")
	}
	if _, _, ok := m.ocLeaderChord("g"); !ok {
		t.Fatal("g should handle (rewind)")
	}
	// y with no assistant message: toast
	if _, cmd, ok := m.ocLeaderChord("y"); !ok || cmd == nil || !strings.Contains(m.toast, "No assistant") {
		t.Fatalf("y toast = %q", m.toast)
	}
	// y with an assistant message: copies + toast
	m.blocks = []block{{kind: blockAssistant, text: "answer"}}
	if _, cmd, ok := m.ocLeaderChord("y"); !ok || cmd == nil || !strings.Contains(m.toast, "copied") {
		t.Fatalf("y copy toast = %q", m.toast)
	}
	// the remaining chords dispatch into existing commands/pickers (all
	// nil-store safe: they append an error or open their picker)
	m.cfg = &config.Config{
		Providers: map[string]config.Provider{"inference-net": {BaseURL: "https://x"}},
		Models:    map[string]config.Model{"kimi-k3": {Providers: []string{"inference-net"}}},
	}
	if _, _, ok := m.ocLeaderChord("m"); !ok || m.mpicker == nil {
		t.Fatal("m should open the model picker")
	}
	m.mpicker = nil
	m.agent.Messages = []llm.Message{{Role: "system"}} // /clear keeps the system prompt
	for _, k := range []string{"l", "n", "c", "t"} {
		if _, _, ok := m.ocLeaderChord(k); !ok {
			t.Fatalf("chord %q should handle", k)
		}
		m.palette = nil // t opens the palette; reset between chords
	}
}

func TestToastAndSplice(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	m := &model{termWidth: 80, now: func() time.Time { return now }}
	if cmd := m.showToast("Copied to clipboard"); cmd == nil || m.toast == "" {
		t.Fatal("showToast should set the toast and return a timer")
	}
	if msg, ok := toastClear(now)(time.Time{}).(toastClearMsg); !ok || !msg.at.Equal(now) {
		t.Fatal("toastClear should carry the toast timestamp")
	}
	backdrop := strings.TrimSuffix(strings.Repeat(strings.Repeat("x", 80)+"\n", 10), "\n")
	out := m.ocSpliceToast(backdrop)
	if !strings.Contains(out, "Copied to clipboard") || !strings.Contains(out, "┃") {
		t.Fatalf("toast not spliced:\n%s", out)
	}
	if len(strings.Split(out, "\n")) != 10 {
		t.Fatal("splice must not change line count")
	}
	// short backdrop lines: the left side pads out to the toast column
	narrow := strings.TrimSuffix(strings.Repeat("y\n", 10), "\n")
	if out := m.ocSpliceToast(narrow); !strings.Contains(out, "Copied to clipboard") {
		t.Fatal("toast should splice over short lines")
	}
	// tiny backdrop: loop stops at the frame's end without panicking
	if out := m.ocSpliceToast("a\nb\nc"); len(strings.Split(out, "\n")) != 3 {
		t.Fatal("tiny backdrop line count changed")
	}
}

func TestOcRecalcWidth(t *testing.T) {
	m := &model{uiMode: opencodeMode, termWidth: 200, width: 154, input: newInput()}
	m.sidebarHide = true
	m.ocRecalcWidth() // sidebar hidden: content takes the sidebar's columns
	if m.width != 200-opencodeLeftMargin {
		t.Fatalf("hidden width = %d", m.width)
	}
	m.sidebarHide = false
	m.ocRecalcWidth()
	if m.width != 200-opencodeLeftMargin-sidebarWidth-opencodeRightGap {
		t.Fatalf("shown width = %d", m.width)
	}
	m.termWidth = 0
	m.ocRecalcWidth() // no size yet: no-op
}

func TestOcOnBg(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor) // tests have no TTY: force colors on so Render emits SGRs
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	bg := lipgloss.Color("#141414")
	ln := "a\x1b[0mb"
	got := ocOnBg(ln, bg)
	seq := bgSeqOf(bg)
	if seq == "" {
		t.Fatal("bgSeqOf must produce a sequence for a concrete color")
	}
	if !strings.HasPrefix(got, seq) || !strings.Contains(got, "\x1b[0m"+seq) {
		t.Fatalf("bg not re-opened after reset: %q", got)
	}
	// no-op cases: empty line, colorless bg
	if ocOnBg("", bg) != "" {
		t.Fatal("empty line must pass through")
	}
	if got := ocOnBg("x", lipgloss.NoColor{}); got != "x" {
		t.Fatalf("NoColor must pass through, got %q", got)
	}
}

func TestThemeSync(t *testing.T) {
	mdMu.Lock()
	sl, sk := mdLight, mdKnown
	mdMu.Unlock()
	oldCache := bgCache
	t.Cleanup(func() {
		mdMu.Lock()
		mdLight, mdKnown = sl, sk
		mdMu.Unlock()
		bgCache = oldCache
	})
	mdMu.Lock()
	mdLight, mdKnown = true, true // currently light
	mdMu.Unlock()

	m := &model{cfg: &config.Config{}, input: newInput()}
	m.Update(themeSyncMsg{light: false, ok: true}) // terminal flipped dark
	mdMu.Lock()
	nowLight := mdLight
	mdMu.Unlock()
	if nowLight || len(m.blocks) == 0 || !strings.Contains(m.blocks[len(m.blocks)-1].text, "auto → dark") {
		t.Fatalf("flip not applied: light=%v blocks=%d", nowLight, len(m.blocks))
	}
	// same theme again: no duplicate note
	n := len(m.blocks)
	m.Update(themeSyncMsg{light: false, ok: true})
	if len(m.blocks) != n {
		t.Fatal("unchanged theme must be a no-op")
	}
	// explicit pick: poll result ignored
	m.cfg.Theme = "dark"
	m.Update(themeSyncMsg{light: true, ok: true})
	mdMu.Lock()
	stillDark := !mdLight
	mdMu.Unlock()
	if !stillDark {
		t.Fatal("explicit theme must not be overridden by the poll")
	}
	// failed poll: ignored
	m.cfg.Theme = ""
	m.Update(themeSyncMsg{ok: false})

	// the poll tick handler re-arms (and skips the subprocess on explicit theme)
	m.cfg.Theme = "dark"
	if _, cmd := m.Update(themePollMsg{}); cmd == nil {
		t.Fatal("poll tick must re-arm on explicit theme")
	}
	m.cfg.Theme = ""
	if _, cmd := m.Update(themePollMsg{}); cmd == nil {
		t.Fatal("poll tick must re-arm on auto theme")
	}
	if themePollTick() == nil {
		t.Fatal("tick must be non-nil")
	}
	if _, ok := themePollFire(time.Time{}).(themePollMsg); !ok {
		t.Fatal("tick must fire a themePollMsg")
	}
	if _, ok := pollClientTheme().(themeSyncMsg); !ok { // result depends on the env; only the type is contractual
		t.Fatal("pollClientTheme must return a themeSyncMsg")
	}
}

func TestMenuViewClampedInOpencodeMode(t *testing.T) {
	long := strings.Repeat("very long description ", 20)
	m := &model{width: 60, uiMode: opencodeMode}
	m.menu = &menu{cands: []cand{{Text: "/auth", Desc: long}, {Text: "/cd", Desc: "change dir"}}}
	out := m.menuView()
	for i, l := range strings.Split(out, "\n") {
		if lipgloss.Width(l) > 60 {
			t.Fatalf("oc menu row %d wider than content width: %d", i, lipgloss.Width(l))
		}
	}
	if !strings.Contains(out, "/auth") || !strings.Contains(out, "1/2") {
		t.Fatalf("oc menu missing content:\n%s", out)
	}
	// the long description word-wraps to a second line, capped with an ellipsis
	if got := len(strings.Split(out, "\n")); got != 4 { // /auth (2 wrapped) + /cd + counter
		t.Fatalf("oc menu rows = %d, want 4 (wrapped desc):\n%s", got, out)
	}
	if !strings.Contains(out, "…") {
		t.Fatal("over-long description should end with an ellipsis")
	}
	// default mode: rows clamp too (an untruncated desc widened the frame)
	m.uiMode = ""
	for i, l := range strings.Split(m.menuView(), "\n") {
		if lipgloss.Width(l) > 60 {
			t.Fatalf("default menu row %d wider than width: %d", i, lipgloss.Width(l))
		}
	}
}

func TestOcMenuOverlay(t *testing.T) {
	m := &model{width: 60, termWidth: 60, uiMode: opencodeMode, inputBodyOff: 20}
	m.menu = &menu{cands: []cand{{Text: "/auth", Desc: "connect"}, {Text: "/cd", Desc: "chdir"}}}
	backdrop := strings.TrimSuffix(strings.Repeat(strings.Repeat("x", 60)+"\n", 30), "\n")
	out := m.ocMenuOverlay(backdrop)
	lines := strings.Split(out, "\n")
	if len(lines) != 30 {
		t.Fatalf("overlay changed line count: %d", len(lines))
	}
	// menu rows land immediately ABOVE the input box row (inputBodyOff)
	joined := strings.Join(lines[:20], "\n")
	if !strings.Contains(joined, "/auth") || !strings.Contains(joined, "1/2") {
		t.Fatalf("menu not above the input box:\n%s", out)
	}
	if strings.Contains(strings.Join(lines[20:], "\n"), "/auth") {
		t.Fatal("menu must not render below the input box")
	}
	// bottom row of the menu touches the box top
	if !strings.Contains(lines[19], "1/2") {
		t.Fatalf("menu bottom not anchored to the input box top: %q", lines[19])
	}
	// no room above: clipped from the top, never overflowing
	m.inputBodyOff = 1
	if got := len(strings.Split(m.ocMenuOverlay(backdrop), "\n")); got != 30 {
		t.Fatalf("clipped overlay changed line count: %d", got)
	}
}

func TestOcDimLine(t *testing.T) {
	if got := ocDimLine(""); got != "" {
		t.Fatalf("empty line should stay empty, got %q", got)
	}
	got := ocDimLine("a\x1b[0mb")
	if !strings.HasPrefix(got, "\x1b[2m") || !strings.Contains(got, "\x1b[0m\x1b[2m") {
		t.Fatalf("dim not re-applied after reset: %q", got)
	}
}

func TestOcPadTo(t *testing.T) {
	got := ocPadTo("ab", 6, lipgloss.Color("#ebebeb"))
	if lipgloss.Width(got) != 6 {
		t.Fatalf("padded width = %d, want 6", lipgloss.Width(got))
	}
	if !strings.Contains(got, "    ") {
		t.Fatalf("missing pad spaces: %q", got)
	}
	if got := ocPadTo("abcdef", 4, lipgloss.Color("#ebebeb")); got != "abcdef" {
		t.Fatalf("over-width content must pass through, got %q", got)
	}
}

func TestSanitizeViewKeepsPanelFillInOpencodeMode(t *testing.T) {
	old := ocActive
	t.Cleanup(func() { ocActive = old })
	line := "x\x1b[48;2;235;235;235m    \x1b[0m" // styled trailing spaces = panel fill
	ocActive = true
	if got := sanitizeView(line); !strings.Contains(got, "    ") {
		t.Fatalf("opencode mode must keep styled trailing spaces: %q", got)
	}
	ocActive = false
	if got := sanitizeView(line); strings.Contains(got, "    ") {
		t.Fatalf("default mode must strip styled trailing spaces: %q", got)
	}
}

func TestOcBgShift(t *testing.T) {
	oldCache := bgCache
	mdMu.Lock()
	sl, sk := mdLight, mdKnown
	mdMu.Unlock()
	t.Cleanup(func() {
		bgCache = oldCache
		mdMu.Lock()
		mdLight, mdKnown = sl, sk
		mdMu.Unlock()
	})
	mdMu.Lock()
	mdKnown, mdLight = true, false
	mdMu.Unlock()

	// no RGB captured -> fall back
	bgCache = bgResult{light: false, valid: true}
	if _, ok := ocBgShift(10); ok {
		t.Fatal("no RGB should not derive")
	}
	// dark bg -> lighten by delta
	bgCache = bgResult{light: false, valid: true, r: 0x26, g: 0x28, b: 0x2c, hasRGB: true}
	if c, ok := ocBgShift(10); !ok || c != lipgloss.Color("#303236") {
		t.Fatalf("dark shift = %v %v, want #303236", c, ok)
	}
	// light bg -> darken by 2x delta, clamped at 0..255
	bgCache = bgResult{light: true, valid: true, r: 0xff, g: 0xff, b: 0xf5, hasRGB: true}
	if c, ok := ocBgShift(10); !ok || c != lipgloss.Color("#ebebe1") {
		t.Fatalf("light shift = %v %v, want #ebebe1", c, ok)
	}
	// panels/element derive from the cache when present
	bgCache = bgResult{light: false, valid: true, r: 0x26, g: 0x28, b: 0x2c, hasRGB: true}
	if got := ocPanelBg(); got != lipgloss.Color("#303236") {
		t.Fatalf("panel = %v, want derived", got)
	}
	if got := ocElementBg(); got != lipgloss.Color("#3a3c40") {
		t.Fatalf("element = %v, want derived", got)
	}
	// unknown theme -> no derivation even with RGB
	mdMu.Lock()
	mdKnown = false
	mdMu.Unlock()
	if _, ok := ocBgShift(10); ok {
		t.Fatal("unknown theme should not derive")
	}
}

func TestParseOSCBgRGB(t *testing.T) {
	r, g, b, ok := parseOSCBgRGB("rgb:2626/2828/2c2c")
	if !ok || r != 0x26 || g != 0x28 || b != 0x2c {
		t.Fatalf("rgb parse = %d %d %d %v", r, g, b, ok)
	}
	if _, _, _, ok := parseOSCBgRGB("garbage"); ok {
		t.Fatal("garbage should not parse")
	}
}

func TestOcPick(t *testing.T) {
	mdMu.Lock()
	sl, sk := mdLight, mdKnown
	mdMu.Unlock()
	t.Cleanup(func() { mdMu.Lock(); mdLight, mdKnown = sl, sk; mdMu.Unlock() })

	set := func(light, known bool) { mdMu.Lock(); mdLight, mdKnown = light, known; mdMu.Unlock() }

	set(false, true) // known dark
	if got := ocPick("#111", "#eee", "8"); got != lipgloss.Color("#111") {
		t.Fatalf("dark = %v", got)
	}
	set(true, true) // known light
	if got := ocPick("#111", "#eee", "8"); got != lipgloss.Color("#eee") {
		t.Fatalf("light = %v", got)
	}
	set(false, false) // unknown -> neutral
	if got := ocPick("#111", "#eee", "8"); got != lipgloss.Color("8") {
		t.Fatalf("unknown neutral = %v", got)
	}
	if got := ocPick("#111", "#eee", ""); got != (lipgloss.NoColor{}) { // unknown, no neutral -> transparent
		t.Fatalf("unknown transparent = %v", got)
	}
}

func TestOpencodeMDStyle(t *testing.T) {
	dark := opencodeMDStyle(false)
	if dark.Document.Color == nil || *dark.Document.Color != "#eeeeee" {
		t.Fatalf("dark document color = %v, want #eeeeee", dark.Document.Color)
	}
	light := opencodeMDStyle(true)
	if light.Document.Color == nil || *light.Document.Color != "#1a1a1a" {
		t.Fatalf("light document color = %v, want #1a1a1a", light.Document.Color)
	}
}

func TestOpencodeHome(t *testing.T) {
	out := opencodeHome(80, 20)
	if !strings.Contains(out, "█") {
		t.Fatal("home screen missing logo glyphs")
	}
	if lipgloss.Height(out) != 20 {
		t.Fatalf("home height = %d, want 20", lipgloss.Height(out))
	}
}

func TestUIModeLabel(t *testing.T) {
	if uiModeLabel(opencodeMode) != "opencode" {
		t.Fatal("opencode label")
	}
	if uiModeLabel("") != "default" {
		t.Fatal("default label")
	}
}

func TestApplyUIMode(t *testing.T) {
	m := &model{input: newInput()}
	m.applyUIMode(opencodeMode)
	if m.uiMode != opencodeMode || !ocActive || m.input.Prompt != "" {
		t.Fatalf("opencode: uiMode=%q ocActive=%v prompt=%q", m.uiMode, ocActive, m.input.Prompt)
	}
	m.applyUIMode("bogus")
	if m.uiMode != "" || ocActive || m.input.Prompt != "┃ " {
		t.Fatalf("default: uiMode=%q ocActive=%v prompt=%q", m.uiMode, ocActive, m.input.Prompt)
	}
}

func TestOpencodeUserCard(t *testing.T) {
	if got := opencodeUserCard("hi", 2, false); got != "hi" {
		t.Fatalf("tiny width should pass through, got %q", got)
	}
	if hov := opencodeUserCard("hello", 40, true); hov == "" {
		t.Fatal("hover card should render") // hover state uses the element shade
	}
	got := opencodeUserCard("hello", 40, false)
	if !strings.Contains(got, "┃") || !strings.Contains(got, "hello") {
		t.Fatal("card missing bar/text")
	}
	if n := strings.Count(got, "\n"); n != 2 { // blank above + text + blank below
		t.Fatalf("card rows: %d newlines, want 2", n)
	}
}

func TestOpencodePrompt(t *testing.T) {
	m := &model{agent: &agent.Agent{}}
	if got := m.opencodePrompt("in", 4); got != "in" {
		t.Fatalf("tiny width should pass through, got %q", got)
	}
	mdMu.Lock()
	sl, sk := mdLight, mdKnown
	mdMu.Unlock()
	t.Cleanup(func() { mdMu.Lock(); mdLight, mdKnown = sl, sk; mdMu.Unlock() })

	mdMu.Lock()
	mdLight, mdKnown = false, true // known dark: full chrome with ▀ shadow
	mdMu.Unlock()
	got := m.opencodePrompt("type here", 40)
	if !strings.Contains(got, "┃") || !strings.Contains(got, "╹") || !strings.Contains(got, "▀") {
		t.Fatal("prompt chrome missing ┃/╹/▀")
	}

	mdMu.Lock()
	mdKnown = false // unknown bg: the ▀ shadow must be skipped (it would render as a black bar on a light terminal)
	mdMu.Unlock()
	if unk := m.opencodePrompt("type here", 40); strings.Contains(unk, "▀") || !strings.Contains(unk, "╹") {
		t.Fatalf("unknown-theme prompt should keep ╹ but drop ▀: %q", unk)
	}
	if !strings.Contains(got, "kimi") && !strings.Contains(got, "Off") {
		// meta row present (mode label at minimum)
		if !strings.Contains(got, "Off") {
			t.Fatalf("prompt meta row missing mode label: %q", got)
		}
	}
}

func TestFmtShortDur(t *testing.T) {
	if got := fmtShortDur(150 * time.Millisecond); got != "150ms" {
		t.Fatalf("sub-second = %q, want 150ms", got)
	}
	if got := fmtShortDur(2400 * time.Millisecond); got != "2.4s" {
		t.Fatalf("seconds = %q, want 2.4s", got)
	}
}

func TestOpencodeThoughtAndAttribution(t *testing.T) {
	m := &model{agent: &agent.Agent{}, modelName: "kimi-k3"}
	at := m.opencodeAttribution(1600 * time.Millisecond)
	if !strings.HasPrefix(at, "   ") || !strings.Contains(at, "▣") || !strings.Contains(at, "kimi-k3") || !strings.Contains(at, "1.6s") {
		t.Fatalf("attribution = %q", at)
	}

	// the Thought block: collapsed header line, expandable to the reasoning text
	b := block{kind: blockThought, text: "step one\nstep two", live: "159ms"}
	col := b.render(80)
	if !strings.Contains(col, "+ Thought: 159ms") || strings.Contains(col, "step one") {
		t.Fatalf("collapsed thought = %q", col)
	}
	if !b.toggle() {
		t.Fatal("thought blocks must toggle")
	}
	exp := b.render(80)
	if !strings.Contains(exp, "step one") || !strings.Contains(exp, "step two") {
		t.Fatalf("expanded thought = %q", exp)
	}
}

func TestBlockOCMetaRendersVerbatim(t *testing.T) {
	b := block{kind: blockOCMeta, text: "   + Thought: 1s"}
	if got := b.render(80); got != "   + Thought: 1s" {
		t.Fatalf("blockOCMeta = %q, want verbatim (indent preserved)", got)
	}
}

func TestOpencodeStatus(t *testing.T) {
	// no usage: just cwd + ctrl+p commands
	m := &model{agent: &agent.Agent{}, width: 80}
	out := m.opencodeStatus()
	if !strings.Contains(out, "ctrl+p commands") {
		t.Fatalf("status missing commands hint: %q", out)
	}
	// with usage + context window: tokens and pct shown, uppercased
	m2 := &model{
		agent: &agent.Agent{ContextLimit: 1000},
		width: 80,
	}
	m2.agent.AddUsage(llm.Usage{PromptTokens: 15800})
	out2 := m2.opencodeStatus()
	if !strings.Contains(out2, "ctrl+p commands") {
		t.Fatalf("status2 missing commands: %q", out2)
	}
	// narrow width path: cwd gets truncated but the right side survives
	m.width = 20
	if narrow := m.opencodeStatus(); !strings.Contains(narrow, "ctrl+p commands") {
		t.Fatalf("narrow status dropped commands: %q", narrow)
	}

	// busy: the spinner + esc hint replace the cwd (opencode's bottom bar)
	m.width = 80
	m.busy = true
	busy := m.opencodeStatus()
	if !strings.Contains(busy, "esc") || !strings.Contains(busy, "interrupt") || strings.Contains(busy, "/home") {
		t.Fatalf("busy status = %q", busy)
	}
	m.interrupt1 = true
	if again := m.opencodeStatus(); !strings.Contains(again, "again to interrupt") {
		t.Fatalf("interrupt1 status = %q", again)
	}

	// narrow + busy: the spinner/hint side has no trim of its own — the row
	// must clamp to the terminal width or the alt-screen frame wraps
	m.width = 12
	if nb := m.opencodeStatus(); lipgloss.Width(nb) > 12 {
		t.Fatalf("narrow busy status is %d cells, want <= 12: %q", lipgloss.Width(nb), nb)
	}
}

// TestOpencodePromptClampsWideLines: a full-width input line (bar + gutter +
// content) must truncate, not wrap — a wrapped row grows the frame past
// layout()'s budget and skews every mouse-Y hit-test.
func TestOpencodePromptClampsWideLines(t *testing.T) {
	m := &model{input: newInput(), cfg: &config.Config{}, agent: &agent.Agent{}}
	out := m.opencodePrompt(strings.Repeat("x", 100), 40)
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w > 40 {
			t.Fatalf("prompt row %d is %d cells, want <= 40: %q", i, w, ln)
		}
	}
}

// TestGrowInputKeepsModeChrome: growInput rebuilds the textarea; the rebuild
// must carry the current mode's prompt/placeholder/styles or opencode mode
// reverts to whip's "┃ " prompt (a double bar that widens the box row).
func TestGrowInputKeepsModeChrome(t *testing.T) {
	t.Cleanup(func() { ocActive = false })
	m := &model{input: newInput(), width: 80, height: 30}
	m.input.SetWidth(78)
	m.applyUIMode(opencodeMode)
	ph := m.input.Placeholder
	m.input.SetValue("line one\nline two\nline three")
	m.growInput()
	if m.input.Height() < 2 {
		t.Fatalf("input did not grow: height=%d", m.input.Height())
	}
	if m.input.Prompt != "" || m.input.Placeholder != ph {
		t.Fatalf("grow reverted opencode chrome: prompt=%q placeholder=%q", m.input.Prompt, m.input.Placeholder)
	}
	if m.input.FocusedStyle.Text.GetBackground() != ocElementBg() {
		t.Fatal("grow dropped the element-bg fill")
	}
}

func TestApplyUIModeSwapsSpinner(t *testing.T) {
	m := &model{input: newInput()}
	t.Cleanup(func() { m.applyUIMode("") })
	m.applyUIMode(opencodeMode)
	if m.spin.Spinner.FPS != ocKnightRider.FPS || len(m.spin.Spinner.Frames) != len(ocKnightRider.Frames) {
		t.Fatal("opencode mode should use the knight-rider spinner")
	}
	m.applyUIMode("")
	if m.spin.Spinner.FPS == ocKnightRider.FPS {
		t.Fatal("default mode should restore the Dot spinner")
	}
}

func TestOCModeLabel(t *testing.T) {
	m := &model{agent: &agent.Agent{}}
	if got := m.ocModeLabel(); got != "Off" {
		t.Fatalf("empty effort = %q, want Off", got)
	}
	m.agent.Effort = "high"
	if got := m.ocModeLabel(); got != "High" {
		t.Fatalf("high = %q, want High", got)
	}
}

func TestSidebarVisible(t *testing.T) {
	m := &model{uiMode: opencodeMode}
	m.termWidth = sidebarMinWidth - 1
	if m.sidebarVisible() {
		t.Fatal("narrow terminal should hide the sidebar")
	}
	m.termWidth = sidebarMinWidth
	if !m.sidebarVisible() {
		t.Fatal("wide terminal should show the sidebar")
	}
	m.uiMode = "" // off in default mode regardless of width
	if m.sidebarVisible() {
		t.Fatal("default mode should never show the sidebar")
	}
}

func TestLspSummary(t *testing.T) {
	m := &model{}
	if got := m.lspSummary(); got != "LSPs are disabled" {
		t.Fatalf("no manager = %q", got)
	}
	if got := lspSummaryLine(nil); got != "no servers" {
		t.Fatalf("empty = %q", got)
	}
	got := lspSummaryLine([]lsp.Status{{State: "connected"}, {State: "failed"}})
	if got != "1/2 connected" {
		t.Fatalf("count = %q, want 1/2 connected", got)
	}
	// non-nil manager path (no specs -> no servers)
	m2 := &model{lspMgr: lsp.NewManager(nil)}
	if got := m2.lspSummary(); got != "no servers" {
		t.Fatalf("empty manager = %q", got)
	}
}

// TestSetUIModeFlushesThink: a ctrl+p UI-mode toggle is reachable mid-stream —
// the reasoning accumulated in the OLD mode's fields must land in the
// transcript, not be silently discarded (opencode→default) or fused into the
// next turn's Thought with a bogus duration (stale thinkStart/ocThink).
func TestSetUIModeFlushesThink(t *testing.T) {
	t.Cleanup(func() { ocActive = false })

	// opencode → default: ocThink drains into a blockThought
	m := &model{cfg: &config.Config{}, input: newInput(), width: 80, height: 30, uiMode: opencodeMode}
	ocActive = true
	m.thinkStart = time.Now().Add(-2 * time.Second)
	m.ocThink = "deep reasoning"
	m.inThink = true
	m.setUIMode("")
	if !m.thinkStart.IsZero() || m.ocThink != "" || m.inThink {
		t.Fatalf("stale think state after toggle: start=%v ocThink=%q inThink=%v", m.thinkStart, m.ocThink, m.inThink)
	}
	if m.viewTop != 1<<30 {
		t.Fatalf("mode switch must arm the viewTop re-anchor sentinel, got %d", m.viewTop)
	}
	var kept bool
	for _, b := range m.blocks {
		if b.kind == blockThought && strings.Contains(b.text, "deep reasoning") {
			kept = true
		}
	}
	if !kept {
		t.Fatal("toggle discarded the streamed reasoning")
	}

	// default → opencode: curThink drains through the default-mode flush
	m2 := &model{cfg: &config.Config{}, input: newInput(), width: 80, height: 30}
	m2.curThink = "partial thought"
	m2.inThink = true
	m2.setUIMode(opencodeMode)
	if m2.curThink != "" || m2.inThink {
		t.Fatalf("default-mode think state not flushed: %q", m2.curThink)
	}
	var drained bool
	for _, b := range m2.blocks {
		if strings.Contains(b.text, "partial thought") {
			drained = true
		}
	}
	if !drained {
		t.Fatal("default-mode reasoning lost on toggle")
	}
}

// TestEffortClickGatedInOpencode: opencode mode renders no header, so a click
// on the top row must not invisibly cycle reasoning effort.
func TestEffortClickGatedInOpencode(t *testing.T) {
	t.Cleanup(func() { ocActive = false })
	m := tasksModel("http://unused")
	m.cfg = &config.Config{}
	m.uiMode = opencodeMode
	ocActive = true
	m.effortX = 60
	before := m.agent.Effort
	m.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: 70, Y: m.viewTop})
	if m.agent.Effort != before {
		t.Fatalf("opencode-mode click cycled effort to %q", m.agent.Effort)
	}
}

// TestLayoutBudgetsPermDialog: viewBody renders the permission modal, so
// layout must budget its rows or the frame over-renders and mouse math drifts.
func TestLayoutBudgetsPermDialog(t *testing.T) {
	m := tasksModel("http://unused")
	m.layout()
	h0 := m.vp.Height
	m.permDialog = &permDialog{req: tools.GateRequest{Tool: "bash", Command: "make build", Rule: "make"}}
	m.layout()
	if want := lipgloss.Height(m.permView()) + 1; h0-m.vp.Height != want {
		t.Fatalf("perm dialog chrome = %d rows, want %d", h0-m.vp.Height, want)
	}
}

func TestSetUIModeSaveError(t *testing.T) {
	// Point HOME at a regular file so the config directory can't be created and
	// cfg.Save() fails; setUIMode must surface the error, not panic.
	f := filepath.Join(t.TempDir(), "home-is-a-file")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// WHIP_HOME (pinned by TestMain) wins over HOME; point it under a regular
	// file so MkdirAll fails and Save() returns an error.
	t.Setenv("WHIP_HOME", filepath.Join(f, "cfg"))
	m := &model{cfg: &config.Config{}, agent: &agent.Agent{}, input: newInput()}
	t.Cleanup(func() { m.applyUIMode("") }) // don't leak ocActive into other tests
	m.setUIMode(opencodeMode)               // must not panic even though Save fails
	if m.uiMode != opencodeMode {
		t.Fatalf("uiMode = %q, want opencode", m.uiMode)
	}
}

func TestSidebarView(t *testing.T) {
	// Plain model: no pricing, no context limit -> the fallback branches.
	m := &model{agent: &agent.Agent{}, termWidth: sidebarMinWidth}
	out := m.sidebarView(20)
	if !strings.Contains(out, "Context") || !strings.Contains(out, "LSP") {
		t.Fatal("sidebar missing Context/LSP sections")
	}
	// clip path: request fewer rows than the content produces
	if out := m.sidebarView(1); out == "" {
		t.Fatal("clipped sidebar should still render")
	}
	// height<=0: natural-height path (no padding/clip)
	if out := m.sidebarView(0); !strings.Contains(out, "whip") {
		t.Fatalf("height<=0 sidebar missing footer: %q", out)
	}

	// Priced model with a context window -> the ctx% and spend branches.
	m2 := &model{
		agent:    &agent.Agent{Model: "m", ContextLimit: 1000},
		provName: "p",
		catalogs: map[string]config.Catalog{
			"p": {Models: []config.ModelInfoLite{{ID: "m", InPrice: 1, OutPrice: 1}}},
		},
		termWidth: sidebarMinWidth,
	}
	if out := m2.sidebarView(20); !strings.Contains(out, "% used") || !strings.Contains(out, "spent") {
		t.Fatalf("priced sidebar missing ctx%%/spend: %q", out)
	}
}

func TestSetUIModeRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // keep cfg.Save() off the real config
	m := &model{cfg: &config.Config{}, agent: &agent.Agent{}, input: newInput()}

	m.setUIMode(opencodeMode)
	if m.uiMode != opencodeMode || m.cfg.UIMode != opencodeMode {
		t.Fatalf("enable: uiMode=%q cfg=%q", m.uiMode, m.cfg.UIMode)
	}
	if m.cfgExtra["uiMode"] != opencodeMode {
		t.Fatalf("cfgExtra not pinned: %v", m.cfgExtra)
	}

	m.setUIMode("bogus") // anything not "opencode" reverts to default
	if m.uiMode != "" || m.cfg.UIMode != "" {
		t.Fatalf("revert: uiMode=%q cfg=%q", m.uiMode, m.cfg.UIMode)
	}
	if _, ok := m.cfgExtra["uiMode"]; ok {
		t.Fatalf("cfgExtra still pinned: %v", m.cfgExtra)
	}
}
