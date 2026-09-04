package tui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/context-labs/whip/internal/config"
)

func TestFullScreenLayoutFitsTerminal(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	m := fullModel()
	m.cfg = &config.Config{}
	m.termWidth = 200
	m.applyOpencodeStyles()
	if m.input.Prompt != "" || !strings.Contains(m.input.Placeholder, "Ask whip anything") || !m.sidebarVisible() {
		t.Fatalf("input/sidebar prompt=%q placeholder=%q sidebar=%t", m.input.Prompt, m.input.Placeholder, m.sidebarVisible())
	}
	m.layout()
	if got := lipgloss.Height(viewStr(m)); got != m.height {
		t.Fatalf("view renders %d rows on a %d-row terminal", got, m.height)
	}
}

func TestOpencodeLeaderChordsUseDaemonCommands(t *testing.T) {
	m, _ := liveQueueModel(t)
	m.busy = false
	m.termWidth, m.width = 200, 150
	m.cfg = &config.Config{}
	m.now = time.Now

	for key, operation := range map[string]string{
		"l": "session.list",
		"n": "history.clear",
		"c": "history.compact",
	} {
		_, command, handled := m.ocLeaderChord(key)
		if !handled || command == nil {
			t.Fatalf("leader %q was not handled", key)
		}
		message := command().(clientCommandMsg)
		if message.action.Operation != operation {
			t.Fatalf("leader %q operation=%q, want %q", key, message.action.Operation, operation)
		}
	}
	if _, _, handled := m.ocLeaderChord("unknown"); handled {
		t.Fatal("unknown leader chord was consumed")
	}
	if _, _, handled := m.ocLeaderChord("b"); !handled || !m.sidebarHide {
		t.Fatal("sidebar leader chord did not toggle the local preference")
	}
}

func TestOpencodeDialogsUseRecursiveCommandSurface(t *testing.T) {
	m := &model{cfg: &config.Config{MCPServers: map[string]config.MCPServer{"local": {}}}, input: newInput(), width: 80, height: 30, termWidth: 80}
	m.openThinPalette()
	out := strings.Join(m.ocDialogRows(), "\n")
	for _, want := range []string{"Commands", "Authentication", "Session", "MCPs", "Browser", "Theme"} {
		if !strings.Contains(out, want) {
			t.Fatalf("opencode command dialog missing %q:\n%s", want, out)
		}
	}
	for _, removed := range []string{"Subagent", "Tasks"} {
		if strings.Contains(out, removed) {
			t.Fatalf("opencode command dialog restored Classic surface %q", removed)
		}
	}
	m.openThinMCPPalette()
	if out := strings.Join(m.ocDialogRows(), "\n"); !strings.Contains(out, "MCP import status") || !strings.Contains(out, "Enable Codex imports") || !strings.Contains(out, "Reconnect local") {
		t.Fatalf("MCP subpanel is incomplete:\n%s", out)
	}
}

func TestOpencodeHomePromptAndSidebarRemainUsable(t *testing.T) {
	if home := opencodeHome(80, 20); !strings.Contains(home, "█") || lipgloss.Height(home) != 20 {
		t.Fatalf("opencode home dimensions/content are invalid: height=%d", lipgloss.Height(home))
	}
	m := &model{
		input: newInput(), termWidth: sidebarMinWidth, sessTitle: "Recursive session",
		clientView: clientPresentation{contextLimit: 1000},
	}
	if prompt := m.opencodePrompt("type here", 40); !strings.Contains(prompt, "┃") || !strings.Contains(prompt, "╹") {
		t.Fatalf("opencode prompt chrome=%q", prompt)
	}
	if sidebar := m.sidebarView(20); !strings.Contains(sidebar, "Recursive session") || !strings.Contains(sidebar, "Context") || !strings.Contains(sidebar, "managed by daemon") {
		t.Fatalf("opencode sidebar=%q", sidebar)
	}
	if card := opencodeUserCard("hello", 40, false); !strings.Contains(card, "┃") || !strings.Contains(card, "hello") {
		t.Fatalf("opencode user card=%q", card)
	}

	// The real leader-key path should arm, dispatch, and clear the chord.
	m.cfg = &config.Config{}
	m.clientState = ClientDisconnected
	m.now = time.Now
	next, _ := m.thinKey(ctrlKey('x'))
	m = next.(*model)
	if m.leaderAt.IsZero() {
		t.Fatal("ctrl+x did not arm the OpenCode leader")
	}
	next, _ = m.thinKey(keyRunes("b"))
	m = next.(*model)
	if !m.leaderAt.IsZero() || !m.sidebarHide {
		t.Fatal("OpenCode leader did not dispatch and clear")
	}
}

func TestOpencodeOverlayAndDialogsStayWithinNarrowFrames(t *testing.T) {
	m := &model{width: 20, termWidth: 20, height: 12, cfg: &config.Config{}}
	m.openThinPalette()
	rows := m.ocDialogRows()
	if out := strings.Join(rows, "\n"); !strings.Contains(out, "Commands") {
		t.Fatalf("narrow command dialog lost its heading:\n%s", out)
	}
	for index, row := range rows {
		if width := lipgloss.Width(row); width > 64 {
			t.Fatalf("dialog row %d widened unexpectedly to %d cells", index, width)
		}
	}
	backdrop := strings.TrimSuffix(strings.Repeat("session line\n", 12), "\n")
	overlay := m.ocOverlay(backdrop)
	if !strings.Contains(overlay, "Commands") || len(strings.Split(overlay, "\n")) != 12 {
		t.Fatalf("overlay changed frame shape:\n%s", overlay)
	}
}

func TestOpencodeMessageActionsHoverAndToolPresentation(t *testing.T) {
	m := &model{input: newInput(), width: 80, height: 20, viewH: 20, hoverIdx: -1}
	m.vp.SetWidth(80)
	m.vp.SetHeight(10)
	m.blocks = []block{{kind: blockUser, text: "hello"}, {kind: blockAssistant, text: "answer"}}
	m.refreshVP()
	m.updateHover(5, m.contentPad())
	if m.hoverIdx < 0 || !m.blocks[m.hoverIdx].hover {
		t.Fatalf("user hover was not tracked: index=%d", m.hoverIdx)
	}
	clicked := m.hoverIdx
	m.clickAt(5, m.blocks[clicked].y0+m.contentPad())
	if m.msgActions == nil || m.msgActions.block != clicked {
		t.Fatalf("message click did not open actions: %+v", m.msgActions)
	}
	if rows := strings.Join(m.ocMsgActionRows(), "\n"); !strings.Contains(rows, "Revert") || !strings.Contains(rows, "Copy") || !strings.Contains(rows, "Fork") {
		t.Fatalf("message actions are incomplete:\n%s", rows)
	}
	if row := ocToolRow("read", `{"path":"main.go"}`, false); !strings.Contains(row, "Read") || !strings.Contains(row, "main.go") {
		t.Fatalf("tool row=%q", row)
	}
	if result := ocToolResult([]string{"one", "two", "three"}, false, false, false, 80); !strings.Contains(result, "3 lines") {
		t.Fatalf("collapsed tool result=%q", result)
	}
}

func TestOpencodeResizeAndSidebarThresholds(t *testing.T) {
	m := &model{cfg: &config.Config{}, input: newInput()}
	m.applyOpencodeStyles()
	next, _ := m.Update(tea.WindowSizeMsg{Width: sidebarMinWidth - 1, Height: 24})
	m = next.(*model)
	if m.sidebarVisible() || m.width != sidebarMinWidth-1-opencodeLeftMargin {
		t.Fatalf("narrow resize sidebar=%t width=%d", m.sidebarVisible(), m.width)
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: sidebarMinWidth, Height: 24})
	m = next.(*model)
	want := sidebarMinWidth - opencodeLeftMargin - sidebarWidth - opencodeRightGap
	if !m.sidebarVisible() || m.width != want || lipgloss.Height(viewStr(m)) != 24 {
		t.Fatalf("wide resize sidebar=%t width=%d want=%d height=%d", m.sidebarVisible(), m.width, want, lipgloss.Height(viewStr(m)))
	}
}
