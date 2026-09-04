package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/daemon"
	"github.com/context-labs/whip/internal/session"
)

func TestCodeFromPartialArgs(t *testing.T) {
	cases := map[string]string{
		`{"code": "n = 1\nprint(\"hi\")"}`: "n = 1\nprint(\"hi\")",
		`{"code": "n = 1\nprint(\"hi`:      "n = 1\nprint(\"hi",
		`{"code": "tab\t\u00e9`:            "tab\té",
		`{"code": "trail\`:                 "trail",
		`{"other": 1}`:                     "",
		`{"co`:                             "",
	}
	for args, want := range cases {
		if got := codeFromPartialArgs(args); got != want {
			t.Fatalf("codeFromPartialArgs(%q) = %q, want %q", args, got, want)
		}
	}
}

func replTestModel(t *testing.T, termWidth int) *model {
	t.Helper()
	m := &model{
		cfg: &config.Config{}, input: newInput(), termWidth: termWidth, now: time.Now,
		sessTitle: "Repl session", replPanel: true,
		clientView: clientPresentation{agents: []session.RuntimeAgent{
			{ID: "root-agent", LifecyclePhase: "running"},
			{ID: "child", ParentID: "root-agent", Name: "w3", LifecyclePhase: "running"},
		}},
	}
	return m
}

func TestReplReducerBuildsCellsAndPanelRenders(t *testing.T) {
	m := replTestModel(t, 140)
	if m.panelWidth() != 70 {
		t.Fatalf("panel width = %d", m.panelWidth())
	}
	m.replApply("root-agent", "stream.tool.call", daemon.StreamEvent{ID: "c1", Name: "rlm_exec", Args: `{"code": "for f in files.list(path=\".\"):\n    print(f`})
	m.replApply("root-agent", "stream.tool.call", daemon.StreamEvent{ID: "c1", Name: "rlm_exec", Args: `{"code": "for f in files.list(path=\".\"):\n    print(f)"}`})
	m.replApply("root-agent", "stream.tool.started", daemon.StreamEvent{ID: "c1", Name: "rlm_exec", Args: `{"code": "for f in files.list(path=\".\"):\n    print(f)"}`})
	m.replApply("root-agent", "stream.cell.host", daemon.StreamEvent{ID: "c1", Name: "files.list", Args: "path=.", Text: "12ms"})
	m.replApply("root-agent", "stream.tool.output", daemon.StreamEvent{ID: "c1", Text: "a.go\n"})
	m.replApply("child", "stream.tool.call", daemon.StreamEvent{ID: "k1", Name: "rlm_exec", Args: `{"code": "shell.run(command=\"sleep 5\")"}`})
	m.replApply("child", "stream.tool.started", daemon.StreamEvent{ID: "k1", Name: "rlm_exec", Args: `{"code": "shell.run(command=\"sleep 5\")"}`})
	running := m.replPanelView(30)
	for _, want := range []string{"REPL · root", "In [1]", "files.list", "→ files.list(path=.) 12ms", "a.go", "⚙ w3", "In[1] shell.run"} {
		if !strings.Contains(running, want) {
			t.Fatalf("running panel missing %q:\n%s", want, running)
		}
	}
	m.replApply("root-agent", "stream.tool.completed", daemon.StreamEvent{ID: "c1", Name: "rlm_exec", Result: `{"value":3,"output":"a.go\nb.go\n","steps":42}`})
	m.replRestart("root-agent", 9, 1)
	done := m.replPanelView(30)
	for _, want := range []string{"42 steps", "b.go", "⇒ ", "── restarted · restored 9 · 1 skipped ──"} {
		if !strings.Contains(done, want) {
			t.Fatalf("finished panel missing %q:\n%s", want, done)
		}
	}
	if height := strings.Count(done, "\n") + 1; height != 30 {
		t.Fatalf("panel height = %d", height)
	}
	for _, line := range strings.Split(done, "\n") {
		if w := ansi.StringWidth(line); w != m.panelWidth() {
			t.Fatalf("panel row width %d != %d: %q", w, m.panelWidth(), ansi.Strip(line))
		}
		if strings.ContainsRune(ansi.Strip(line), '\uFFFD') {
			t.Fatalf("panel row contains a replacement character: %q", ansi.Strip(line))
		}
	}
	m.replApply("root-agent", "stream.tool.completed", daemon.StreamEvent{ID: "c2", Name: "rlm_exec", Result: "Error: <rlm-cell>:1:1: undefined: nope"})
	if failed := m.replPanelView(30); !strings.Contains(failed, "✗ <rlm-cell>:1:1: undefined: nope") || !strings.Contains(failed, "In [2]") {
		t.Fatalf("failed cell not rendered:\n%s", failed)
	}
	m.termWidth = sidebarMinWidth
	if m.panelWidth() != sidebarMinWidth/2 {
		t.Fatalf("narrow panel width = %d", m.panelWidth())
	}
	m.replPanel = false
	if m.panelWidth() != sidebarWidth {
		t.Fatalf("context sidebar width = %d", m.panelWidth())
	}
}

func TestReplPanelToggleChordAndCommand(t *testing.T) {
	const term = 160
	m := &model{cfg: &config.Config{}, input: newInput(), termWidth: term, now: time.Now, clientState: ClientDisconnected}
	next, _ := m.thinKey(ctrlKey('x'))
	m = next.(*model)
	next, _ = m.thinKey(keyRunes("r"))
	m = next.(*model)
	if !m.replPanel || m.width != term-opencodeLeftMargin-term/2-opencodeRightGap {
		t.Fatalf("chord toggle replPanel=%v width=%d", m.replPanel, m.width)
	}
	next, _ = m.thinCommand("/repl")
	m = next.(*model)
	if m.replPanel || m.width != term-opencodeLeftMargin-sidebarWidth-opencodeRightGap {
		t.Fatalf("command toggle replPanel=%v width=%d", m.replPanel, m.width)
	}
	if sidebar := m.sidebarView(10); !strings.Contains(sidebar, "Context") {
		t.Fatalf("context sidebar did not return after toggling off: %q", sidebar)
	}
}

func TestReplRebuildsFromStoredPresentation(t *testing.T) {
	m := replTestModel(t, 140)
	encode := func(event daemon.StreamEvent) []byte {
		payload, _ := json.Marshal(event)
		return payload
	}
	m.clientView.agentPresentations = map[string][]session.SnapshotEvent{"child": {
		{Seq: 1, Kind: "stream.tool.started", Payload: encode(daemon.StreamEvent{AgentID: "child", ID: "k1", Name: "rlm_exec", Args: `{"code": "memo = 1"}`})},
		{Seq: 2, Kind: "stream.tool.completed", Payload: encode(daemon.StreamEvent{AgentID: "child", ID: "k1", Name: "rlm_exec", Result: `{"value":null,"steps":3}`})},
	}}
	m.agentOpen = "child"
	m.replRebuild()
	if view := m.replPanelView(12); !strings.Contains(view, "In [1]") || !strings.Contains(view, "memo = 1") || !strings.Contains(view, "3 steps") {
		t.Fatalf("rebuilt child panel:\n%s", view)
	}
}

func TestReplPanelScrollsIndependentlyOfChat(t *testing.T) {
	m := replTestModel(t, 140)
	for i := 1; i <= 30; i++ {
		id := fmt.Sprintf("call-%d", i)
		args := fmt.Sprintf(`{"code":"x = %d"}`, i)
		m.replApply("root-agent", "stream.tool.started", daemon.StreamEvent{ID: id, Name: "rlm_exec", Args: args})
		m.replApply("root-agent", "stream.tool.completed", daemon.StreamEvent{ID: id, Name: "rlm_exec", Result: `{"value":1}`})
	}
	view := m.replPanelView(20)
	if !strings.Contains(view, "In [30]") || strings.Contains(view, "In [1]") {
		t.Fatalf("panel should follow the newest cell:\n%s", view)
	}
	wheel := func(x int, up bool) {
		next, _ := m.thinMouse(wheelMsg(x, 5, up))
		m = next.(*model)
	}
	inPanel := 140 - m.panelWidth() // the panel's first column
	for range 5 {
		wheel(inPanel, true)
	}
	view = m.replPanelView(20)
	if m.replScroll != 15 || strings.Contains(view, "In [30]") || !strings.Contains(view, "↓ 15 more lines") {
		t.Fatalf("scroll=%d view:\n%s", m.replScroll, view)
	}
	wheel(inPanel-1, true) // the divider column belongs to the chat side
	wheel(10, true)        // over the chat: the panel stays put
	if m.replScroll != 15 {
		t.Fatalf("chat wheel moved the panel: %d", m.replScroll)
	}
	for range 1000 {
		wheel(inPanel, true)
	}
	view = m.replPanelView(20)
	if !strings.Contains(view, "In [1]") {
		t.Fatalf("scrolled past the top:\n%s", view)
	}
	for range 2000 {
		wheel(inPanel, false)
	}
	if view = m.replPanelView(20); m.replScroll != 0 || !strings.Contains(view, "In [30]") {
		t.Fatalf("scroll=%d after wheel down:\n%s", m.replScroll, view)
	}
	for _, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w != m.panelWidth() {
			t.Fatalf("row width %d != %d: %q", w, m.panelWidth(), ansi.Strip(line))
		}
	}
}

func TestReplPanelPressDoesNotSelectChat(t *testing.T) {
	m := compactCmdModel()
	m.applyOpencodeStyles()
	m.replPanel = true
	m.Update(mkWinSize(160, 30))
	if !m.sidebarVisible() || m.panelWidth() != 80 {
		t.Fatalf("setup: sidebarVisible=%v panelWidth=%d", m.sidebarVisible(), m.panelWidth())
	}
	m.append("hello world")
	m.append("second block here")
	next, _ := m.Update(keyRunes(" ")) // settle layout
	m = next.(*model)
	m.input.SetValue("")
	viewStr(m)
	rowY := func(r int) int { return m.vpTopRows() + (r + m.contentPad() - m.vp.YOffset()) - m.vpLead }
	y0, y1 := rowY(m.blocks[0].y0), rowY(m.blocks[1].y0)
	panelX := m.termWidth - 10
	next, _ = m.Update(clickMsg(panelX, y0))
	m = next.(*model)
	next, _ = m.Update(dragMsg(panelX, y1))
	m = next.(*model)
	if m.sel != nil {
		t.Fatalf("a press in the REPL panel started a chat selection: %+v", m.sel)
	}
	// A drag that starts in the chat still completes when it ends over the panel.
	next, _ = m.Update(clickMsg(3, y0))
	m = next.(*model)
	next, _ = m.Update(dragMsg(panelX, y1))
	m = next.(*model)
	if m.sel == nil || m.sel.anchor == m.sel.cur {
		t.Fatalf("chat drag ending over the panel lost its selection: %+v", m.sel)
	}
}

func TestReplHistorySurvivesSnapshotsAndKeepsScroll(t *testing.T) {
	m := replTestModel(t, 140)
	clock := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return clock }
	encode := func(event daemon.StreamEvent) []byte {
		payload, _ := json.Marshal(event)
		return payload
	}
	var seq int64
	cellEvents := func(i int) []session.SnapshotEvent {
		id := fmt.Sprintf("call-%d", i)
		started := daemon.StreamEvent{ID: id, Name: "rlm_exec", Args: fmt.Sprintf(`{"code":"x = %d"}`, i)}
		completed := daemon.StreamEvent{ID: id, Name: "rlm_exec", Result: `{"value":1,"steps":2}`}
		seq += 2
		return []session.SnapshotEvent{
			{Seq: seq - 1, Kind: "stream.tool.started", Payload: encode(started)},
			{Seq: seq, Kind: "stream.tool.completed", Payload: encode(completed)},
		}
	}
	// Cells that were already in the daemon's snapshot when the TUI connected: no clock.
	for i := 1; i <= 30; i++ {
		m.clientView.presentation = append(m.clientView.presentation, cellEvents(i)...)
	}
	m.replRebuild()
	view := m.replPanelView(20)
	if strings.Contains(view, "0ms") || !strings.Contains(view, "In [30]") {
		t.Fatalf("replayed cells must render without a fabricated duration:\n%s", view)
	}
	// A live cell (recordClientStream path) keeps its measured duration across later snapshots.
	live := func(kind string, event daemon.StreamEvent) {
		seq++
		payload := encode(event)
		m.recordClientStream(daemon.ProtocolEvent{Seq: seq, Kind: kind, Payload: payload})
	}
	live("stream.tool.started", daemon.StreamEvent{ID: "call-31", Name: "rlm_exec", Args: `{"code":"x = 31"}`})
	clock = clock.Add(2 * time.Second)
	live("stream.tool.completed", daemon.StreamEvent{ID: "call-31", Name: "rlm_exec", Result: `{"value":1}`})
	if view = m.replPanelView(20); !strings.Contains(view, "In [31]  2.0s") {
		t.Fatalf("live cell duration missing:\n%s", view)
	}
	inPanel := 140 - m.panelWidth()
	for range 5 {
		next, _ := m.thinMouse(wheelMsg(inPanel, 5, true))
		m = next.(*model)
	}
	before := m.replPanelView(20)
	// The turn ends: the daemon's next snapshot carries no presentation at all.
	m.clientView.presentation = nil
	clock = clock.Add(time.Minute)
	m.replRebuild()
	if after := m.replPanelView(20); m.replScroll != 15 || after != before {
		t.Fatalf("snapshot after turn end lost history or moved the panel (scroll=%d)\nbefore:\n%s\nafter:\n%s", m.replScroll, before, after)
	}
	// Replaying the same events again (another snapshot) adds nothing.
	m.clientView.presentation = cellEvents(31)
	m.clientView.presentation[0].Seq, m.clientView.presentation[1].Seq = seq-1, seq
	m.replRebuild()
	if got := len(m.repl["root-agent"].cells); got != 31 {
		t.Fatalf("replayed snapshot duplicated cells: %d", got)
	}
	// New rows arriving below keep the scrolled-up content anchored.
	live("stream.tool.started", daemon.StreamEvent{ID: "call-32", Name: "rlm_exec", Args: `{"code":"x = 32"}`})
	after := m.replPanelView(20)
	beforeRows, afterRows := strings.Split(before, "\n"), strings.Split(after, "\n")
	if m.replScroll <= 15 || beforeRows[6] != afterRows[6] || beforeRows[10] != afterRows[10] {
		t.Fatalf("new rows shifted the scrolled view (scroll=%d)\nbefore:\n%s\nafter:\n%s", m.replScroll, before, after)
	}
	// An idle child's cells stay visible even though snapshots drop them.
	m.recordClientStream(daemon.ProtocolEvent{Seq: seq + 1, Kind: "stream.tool.started", Payload: encode(daemon.StreamEvent{AgentID: "child", ID: "c-1", Name: "rlm_exec", Args: `{"code":"y = 1"}`})})
	m.clientView.agentPresentations = nil
	m.replRebuild()
	m.agentOpen = "child"
	if view = m.replPanelView(20); !strings.Contains(view, "REPL · w3") || !strings.Contains(view, "y = 1") {
		t.Fatalf("child history lost after snapshot:\n%s", view)
	}
}

func TestAgentTreeReturnsToRoot(t *testing.T) {
	m := replTestModel(t, 140)
	m.agentOpen = "child"
	rows := m.runtimeAgentRows()
	if len(rows) != 2 || rows[0].agent.ID != "root-agent" || rows[1].depth != 1 {
		t.Fatalf("tree rows = %+v", rows)
	}
	if view := m.replPanelView(20); !strings.Contains(view, "⚙ root (root-agent) — running") || !strings.Contains(view, "⚙ w3 (child) — running · open") {
		t.Fatalf("tree rendering:\n%s", view)
	}
	// ctrl+t starts on the first child; ↑ reaches the root; enter goes back to it.
	next, _ := m.thinKey(ctrlKey('t'))
	m = next.(*model)
	if !m.agentsFocus || m.agentSel != 1 {
		t.Fatalf("focus=%v sel=%d", m.agentsFocus, m.agentSel)
	}
	next, _ = m.thinKey(keyMsg(tea.KeyUp))
	m = next.(*model)
	next, _ = m.thinKey(keyMsg(tea.KeyEnter))
	m = next.(*model)
	if m.agentOpen != "" || m.agentsFocus {
		t.Fatalf("enter on the root row did not return to root: open=%q focus=%v", m.agentOpen, m.agentsFocus)
	}
	// esc while the tree is focused also leaves an open child.
	m.agentOpen = "child"
	next, _ = m.thinKey(keyMsg(tea.KeyDown))
	m = next.(*model)
	if !m.agentsFocus {
		t.Fatal("↓ on an empty input should focus the tree")
	}
	next, _ = m.thinKey(keyMsg(tea.KeyEsc))
	m = next.(*model)
	if m.agentOpen != "" || m.agentsFocus {
		t.Fatalf("esc with the tree focused: open=%q focus=%v", m.agentOpen, m.agentsFocus)
	}
	// ctrl+x on the root row is a no-op rather than a stop request.
	next, _ = m.thinKey(ctrlKey('t'))
	m = next.(*model)
	next, _ = m.thinKey(keyMsg(tea.KeyUp))
	m = next.(*model)
	if _, command := m.thinKey(ctrlKey('x')); command != nil {
		t.Fatal("ctrl+x on the root row should not submit a stop")
	}
}

func TestReplPanelKeepsRowsOnOneLineAndWrapsCodeLosslessly(t *testing.T) {
	m := replTestModel(t, 140)
	code := "x = \"" + strings.Repeat("a", 200) + "\"\n\tif x:\n\t\tprint(x)"
	args, _ := json.Marshal(map[string]string{"code": code})
	m.replApply("root-agent", "stream.tool.started", daemon.StreamEvent{ID: "c1", Name: "rlm_exec", Args: string(args)})
	m.replApply("root-agent", "stream.cell.host", daemon.StreamEvent{ID: "c1", Name: "shell.run", Args: "command=a\tb\nc\r", Text: "1ms", Result: "boom\nline two"})
	m.replApply("root-agent", "stream.tool.completed", daemon.StreamEvent{ID: "c1", Name: "rlm_exec", Result: `{"value":"v\tw\nz","output":"col1\tcol2\r\nnext"}`})
	view := m.replPanelView(40)
	rows := strings.Split(view, "\n")
	if len(rows) != 40 {
		t.Fatalf("panel height %d != 40", len(rows))
	}
	for _, line := range rows {
		if w := ansi.StringWidth(line); w != m.panelWidth() {
			t.Fatalf("row width %d != %d: %q", w, m.panelWidth(), ansi.Strip(line))
		}
	}
	if plain := ansi.Strip(view); strings.Count(plain, "a") < 200 || strings.Contains(plain, "\t") {
		t.Fatalf("wrapped code lost characters or kept tabs:\n%s", plain)
	}
}

func TestAgentsDockReturnsWhenThePanelCannotShowTheTree(t *testing.T) {
	m := replTestModel(t, sidebarMinWidth-1)
	if m.agentsDock() == "" {
		t.Fatal("narrow opencode terminal has no agent tree anywhere")
	}
	m.termWidth = 140
	if m.agentsDock() != "" {
		t.Fatal("wide opencode terminal should render the tree in the panel only")
	}
	m.sidebarHide = true
	if m.agentsDock() == "" {
		t.Fatal("hidden sidebar has no agent tree anywhere")
	}
}

func TestAgentDetailsFitTheChatColumn(t *testing.T) {
	m := replTestModel(t, 140)
	m.width = 40
	m.agentOpen = "child"
	m.clientView.agents[1].CWD = strings.Repeat("/very-long-directory", 6)
	for _, line := range strings.Split(m.agentDetails(), "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Fatalf("details row %d cols wide: %q", w, ansi.Strip(line))
		}
	}
}
