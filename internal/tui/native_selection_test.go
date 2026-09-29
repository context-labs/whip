package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeSelectionFixture(t *testing.T) *nativeModel {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	m := &nativeModel{work: nativeWork{ctx: ctx, stop: cancel}, owner: protocol.Session{ID: "selected"}, input: newInput(), width: 80, height: 20, clientDirectory: t.TempDir()}
	t.Cleanup(m.close)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TMUX", "")
	m.refresh()
	return m
}

func nativeSelectionRows(m *nativeModel, rows []string) {
	m.rows = rows
	m.vp.setTotal(len(rows))
	m.vp.rows = func(y int) string { return m.selectionRow(nativeSelectTranscript, y, m.rows[y]) }
}

func nativeSelectionCopied(t *testing.T, m *nativeModel, command tea.Cmd) string {
	t.Helper()
	if command == nil {
		t.Fatal("copy not offered", m.status)
	}
	var copied string
	for _, command := range command().(tea.BatchMsg) {
		switch value := command().(type) {
		case tea.RawMsg:
			sequence := value.Msg.(string)
			body := strings.TrimSuffix(strings.TrimPrefix(sequence, "\x1b]52;c;"), "\x07")
			bytes, err := base64.StdEncoding.DecodeString(body)
			if err != nil {
				t.Fatal(err)
			}
			copied = string(bytes)
		case nativeCopyResult:
			m.Update(value)
		default:
			t.Fatalf("unexpected copy event %T", value)
		}
	}
	return copied
}

func TestNativeSelectionCopiesRenderedUnicodeBlankLinesAndInputWithoutMutation(t *testing.T) {
	m := nativeSelectionFixture(t)
	nativeSelectionRows(m, []string{"\x1b[1m文 horses\x1b[0m", "", "third line  "})
	m.input.SetValue("composer stays")
	m.sizeInput()
	m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 5, Y: 2, Button: tea.MouseLeft})
	if !strings.Contains(m.vp.View(), "\x1b[7m") {
		t.Fatal("selection was not visibly highlighted")
	}
	_, command := m.Update(tea.MouseReleaseMsg{X: 5, Y: 2, Button: tea.MouseLeft})
	if text := nativeSelectionCopied(t, m, command); text != "文 horses\n\nthird" {
		t.Fatalf("rendered selection changed: %q", text)
	}
	if m.input.Value() != "composer stays" {
		t.Fatal("selection changed the composer")
	}
	area := m.selectionArea(nativeSelectInput)
	m.Update(tea.MouseClickMsg{X: area.x, Y: area.y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: area.x + 8, Y: area.y, Button: tea.MouseLeft})
	_, command = m.Update(tea.MouseReleaseMsg{X: area.x + 8, Y: area.y, Button: tea.MouseLeft})
	if text := nativeSelectionCopied(t, m, command); text != "composer" {
		t.Fatalf("input selection changed: %q", text)
	}
}

func TestNativeSelectionRejectsStaleRowsOwnerResizeAndCoveredViews(t *testing.T) {
	for _, change := range []string{"rows", "owner", "history", "resize", "completion", "palette", "shift", "off"} {
		t.Run(change, func(t *testing.T) {
			m := nativeSelectionFixture(t)
			nativeSelectionRows(m, []string{"original row"})
			m.Update(tea.MouseClickMsg{X: 0, Y: 0, Button: tea.MouseLeft})
			m.Update(tea.MouseMotionMsg{X: 4, Y: 0, Button: tea.MouseLeft})
			release := tea.MouseReleaseMsg{X: 4, Y: 0, Button: tea.MouseLeft}
			switch change {
			case "rows":
				m.rows = []string{"replacement row"}
			case "owner":
				m.owner.ID = "another owner"
			case "history":
				m.history.snapshot.Revision++
			case "resize":
				m.Update(tea.WindowSizeMsg{Width: 150, Height: 40})
			case "completion":
				m.completion = &nativeCompletion{cancel: func() {}}
			case "palette":
				m.openNativePalette()
			case "shift":
				release.Mod = tea.ModShift
			case "off":
				m.preferences.Mouse = new(false)
			}
			if _, command := m.Update(release); command != nil || m.selection != nil {
				t.Fatal("stale or covered selection was copied", change, m.status)
			}
		})
	}
}

func TestNativeSelectionWordRowEdgeScrollAndBoundedCopy(t *testing.T) {
	m := nativeSelectionFixture(t)
	nativeSelectionRows(m, []string{"first 文horse last"})
	m.Update(tea.MouseClickMsg{X: 8, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 8, Y: 0, Button: tea.MouseLeft})
	_, command := m.Update(tea.MouseClickMsg{X: 8, Y: 0, Button: tea.MouseLeft})
	if text := nativeSelectionCopied(t, m, command); text != "文horse" {
		t.Fatalf("word selection %q", text)
	}
	_, command = m.Update(tea.MouseClickMsg{X: 8, Y: 0, Button: tea.MouseLeft})
	if text := nativeSelectionCopied(t, m, command); text != "first 文horse last" {
		t.Fatalf("row selection %q", text)
	}
	rows := make([]string, 60)
	for i := range rows {
		rows[i] = fmt.Sprintf("row %d", i)
	}
	nativeSelectionRows(m, rows)
	m.Update(tea.MouseClickMsg{X: 0, Y: 1, Button: tea.MouseLeft})
	_, command = m.Update(tea.MouseMotionMsg{X: 6, Y: m.vp.Height() + 2, Button: tea.MouseLeft})
	if command == nil || m.selection == nil {
		t.Fatal("edge scroll was not scheduled")
	}
	selected := m.selection
	for range 3 {
		if m.selectionScroll(nativeSelectionTick{selection: selected}) == nil {
			t.Fatal("edge scroll did not continue")
		}
	}
	if m.vp.YOffset() != 3 || m.follow {
		t.Fatal("selection did not scroll its captured viewport", m.vp.YOffset())
	}
	_, command = m.Update(tea.MouseReleaseMsg{X: 6, Y: m.vp.Height() + 2, Button: tea.MouseLeft})
	text := nativeSelectionCopied(t, m, command)
	if !strings.HasPrefix(text, "row 1\nrow 2\n") || !strings.Contains(text, "row 15") {
		t.Fatal("offscreen selection did not preserve row range", text)
	}
	if m.selectionScroll(nativeSelectionTick{selection: selected}) != nil {
		t.Fatal("released selection kept scrolling")
	}
	m.preferences.Sidebar = new(false)
	m.width = nativeCopyLimit + 2
	rows = []string{strings.Repeat("x", nativeCopyLimit+1)}
	m.rows = rows
	m.vp.SetWidth(m.width)
	m.vp.SetYOffset(0)
	m.vp.setTotal(1)
	m.selection = &nativeSelection{cur: selPos{col: nativeCopyLimit + 1}, pane: nativeSelectTranscript, owner: m.owner.ID, generation: m.generation, area: m.selectionArea(nativeSelectTranscript)}
	if m.copySelection() != nil || !strings.Contains(m.status, "1 MiB") {
		t.Fatal("oversized selection was sent", m.status)
	}
}

func TestNativeSelectionGeometryAndLatestToolExpansion(t *testing.T) {
	m := nativeSelectionFixture(t)
	m.preferences.Sidebar, m.preferences.Repl = new(true), new(true)
	m.history.messages = []protocol.Message{
		nativeMessage(1, "tool", ""), nativeMessage(2, "assistant", "between tools"), nativeMessage(3, "tool", ""),
	}
	for _, i := range []int{0, 2} {
		m.history.messages[i].Parts = []protocol.Part{{Type: "tool_result", Result: &protocol.ToolResult{CallID: "same-call-id", Output: strings.Repeat(fmt.Sprintf("tool %d row\n", i), 15)}}}
	}
	for _, width := range []int{80, 120, 160} {
		m.Update(tea.WindowSizeMsg{Width: width, Height: 35})
		area := m.selectionArea(nativeSelectTranscript)
		if (width == 160 && area.x != 44) || (width < 160 && area.x != 0) {
			t.Fatal("transcript selection did not follow actual columns", width, area)
		}
		m.follow = false
		m.vp.SetYOffset(0)
		block := m.messageRows[0]
		m.Update(tea.MouseClickMsg{X: area.x + 1, Y: block.start, Button: tea.MouseLeft})
		m.Update(tea.MouseReleaseMsg{X: area.x + 1, Y: block.start, Button: tea.MouseLeft})
		if !m.toolExpanded(m.history.messages[0].ID) || m.toolExpanded(m.history.messages[2].ID) {
			t.Fatal("click expanded an unrelated block")
		}
		m.Update(tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
		if !m.toolExpanded(m.history.messages[2].ID) || !m.toolExpanded(m.history.messages[0].ID) {
			t.Fatal("latest-tool shortcut lost independent expansions")
		}
		m.command("/tools collapse")
		if len(m.toolExpansion) != 0 {
			t.Fatal("global collapse retained per-message overrides")
		}
		m.selectionClick = nativeSelectionClick{}
	}
	m.replDisplay = []string{"copied REPL text"}
	m.replVP.setTotal(1)
	a := m.selectionArea(nativeSelectREPL)
	if a.x <= m.selectionArea(nativeSelectTranscript).x+m.transcriptWidth() {
		t.Fatal("REPL selection overlaps main column")
	}
	m.Update(tea.MouseClickMsg{X: a.x, Y: a.y, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: a.x + 6, Y: a.y, Button: tea.MouseLeft})
	_, command := m.Update(tea.MouseReleaseMsg{X: a.x + 6, Y: a.y, Button: tea.MouseLeft})
	if text := nativeSelectionCopied(t, m, command); text != "copied" {
		t.Fatal("REPL selection did not match rendered body", text)
	}
	for row := range strings.SplitSeq(m.View().Content, "\n") {
		if ansi.StringWidth(row) > m.width {
			t.Fatal("selection overflowed the frame")
		}
	}
}
