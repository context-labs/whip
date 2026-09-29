package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeREPLReadsRealCellsAndNavigatesWithoutExecuting(t *testing.T) {
	m, provider := nativeUIFixture(t)
	m.preferencesDirectory = t.TempDir()
	m.preferences.Repl = new(true)
	provider.codes = map[string]string{}
	for i := range 9 {
		provider.codes[fmt.Sprintf("cell-%d", i)] = "print(\"notebook output\")\n42"
	}
	m.Update(tea.WindowSizeMsg{Width: 160, Height: 35})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var earliest protocol.ID
	for i := range 9 {
		input := nativeUISubmit(t, m, fmt.Sprintf("cell-%d", i))
		completed, err := input.command.Wait(ctx)
		if err != nil || completed.Turn == nil || completed.Turn.State != "succeeded" {
			t.Fatal(completed, err)
		}
		if i == 0 {
			earliest = completed.Turn.ID
		}
	}
	m.polls = 0
	nativeUIRead(t, m)
	m.polls = 0
	nativeUIRead(t, m)
	if m.execution == nil || m.execution.err != nil || len(m.execution.cells) != 8 || m.execution.older == nil {
		t.Fatal(m.execution, m.status)
	}
	rows := strings.Join(m.replRows(60), "\n")
	if !strings.Contains(rows, "notebook output") || !strings.Contains(rows, "succeeded") || !strings.Contains(rows, "Checkpoint") {
		t.Fatal(rows)
	}
	before := nativeMenuRPC[protocol.Usage](t, m.connection, "usage.get", protocol.SessionParams{SessionID: m.owner.ID})
	m.replCommand("older")
	nativeUIRead(t, m)
	if m.execution == nil || len(m.execution.cells) != 1 || m.execution.cells[0].TurnID != earliest {
		t.Fatal("older cells missing", m.execution, m.status)
	}
	m.replCommand("turn " + string(earliest))
	nativeUIRead(t, m)
	after := nativeMenuRPC[protocol.Usage](t, m.connection, "usage.get", protocol.SessionParams{SessionID: m.owner.ID})
	if after.Attempts != before.Attempts {
		t.Fatal("REPL observation executed model work", before.Attempts, after.Attempts)
	}
	if m.execution == nil || len(m.execution.cells) != 1 {
		t.Fatal(m.execution, m.status)
	}
}

func TestNativeREPLExactJoinsAndPreviewLifecycle(t *testing.T) {
	call := nativeMessage(1, "assistant", "")
	call.TurnID = new(protocol.ID("turn"))
	call.Parts = []protocol.Part{{Type: "tool_call", Call: &protocol.ToolCall{ID: "reused", Name: "execute", Arguments: json.RawMessage(`{"code":"print(123)"}`)}}}
	result := nativeMessage(2, "tool", "")
	result.TurnID = call.TurnID
	result.Parts = []protocol.Part{{Type: "tool_result", Result: &protocol.ToolResult{CallID: "reused", Output: "settled output"}}}
	cell := protocol.Cell{ID: "cell", SessionID: "owner", TurnID: "turn", CallMessageID: call.ID, CallID: "reused", ResultMessageID: &result.ID, State: "running"}
	v := &nativeExecution{owner: "owner", revision: 1, epoch: "epoch", cells: []protocol.Cell{cell}, turns: []protocol.Turn{{ID: "turn", SessionID: "owner", HistoryRevision: 1}}, operations: []protocol.HostOperation{{ID: "op", SessionID: "owner", TurnID: "turn", CellID: &cell.ID, Origin: "cell", Capability: "files.read", State: "succeeded"}, {ID: "other", SessionID: "other", TurnID: "turn", CellID: &cell.ID, Origin: "cell", Capability: "MUST_NOT_SHOW", State: "succeeded"}}}
	m := &nativeModel{owner: protocol.Session{ID: "owner"}, execution: v, history: nativeTranscript{owner: "owner", epoch: "epoch", snapshot: protocol.HistorySnapshot{SessionID: "owner", Revision: 1}, messages: []protocol.Message{call, result}, cellOutput: &protocol.CellOutputPreview{SessionID: "owner", TurnID: "turn", CellID: "cell", CallMessageID: call.ID, CallID: "reused", HistoryRevision: 1, Text: "provisional stdout"}}}
	rows := strings.Join(m.replRows(60), "\n")
	if !strings.Contains(rows, "print(123)") || !strings.Contains(rows, "files.read") || !strings.Contains(rows, "provisional stdout") || strings.Contains(rows, "MUST_NOT_SHOW") {
		t.Fatal(rows)
	}
	imported := call
	imported.TurnID = nil
	m.history.messages = []protocol.Message{imported}
	rows = strings.Join(m.replRows(60), "\n")
	if strings.Contains(rows, "print(123)") || !strings.Contains(rows, "outside this transcript page") {
		t.Fatal("import acquired local execution", rows)
	}
	m.history.messages = []protocol.Message{call, result}
	v.cells[0].State = "succeeded"
	rows = strings.Join(m.replRows(60), "\n")
	if strings.Contains(rows, "provisional stdout") {
		t.Fatal("settled cell kept preview")
	}
	v.epoch = "old"
	if rows := strings.Join(m.replRows(60), "\n"); strings.Contains(rows, "settled output") {
		t.Fatal("stale process evidence remained", rows)
	}
}

func TestNativeREPLLayoutAndFocusedForeignTurnStayScoped(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.preferencesDirectory = t.TempDir()
	m.preferences.Repl = new(true)
	if err := saveNativePreferences(m.preferencesDirectory, m.preferences); err != nil {
		t.Fatal(err)
	}
	for _, size := range [][2]int{{79, 24}, {120, 30}, {149, 32}, {150, 32}, {180, 40}} {
		m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		if m.sidebarVisible() != (size[0] >= 150) || m.replVisible() != (size[0] >= 120) {
			t.Fatal("retained column thresholds changed", size)
		}
		for row := range strings.SplitSeq(m.View().Content, "\n") {
			if ansi.StringWidth(row) > size[0] {
				t.Fatal("oversized frame", size, ansi.StringWidth(row))
			}
		}
	}
	m.Update(tea.WindowSizeMsg{Width: 130, Height: 30})
	m.layoutCommand("sidebar")
	if m.preferences.Sidebar == nil || *m.preferences.Sidebar {
		t.Fatal("suppressed sidebar could not be toggled off")
	}
	m.input.SetValue("unsent while toggling display")
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if m.input.Value() != "unsent while toggling display" || m.replVisible() {
		t.Fatal("display toggle lost draft or ignored preference", m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	foreign := nativeNavigationRoot(t, m, "repl-foreign", "Foreign")
	command, err := m.connection.PrepareInput("sessions.submit", protocol.SubmitParams{SessionID: foreign.ID, Identity: protocol.RequestIdentity{ClientID: "repl", RequestID: "foreign"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "foreign"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := command.Send(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done, err := command.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m.replCommand("turn " + string(done.Turn.ID))
	nativeUIRead(t, m)
	if m.execution == nil || m.execution.err == nil || !strings.Contains(m.execution.err.Error(), "ownership") {
		t.Fatal("foreign turn projected", m.execution, m.status)
	}
	if len(m.execution.cells) != 0 || len(m.execution.operations) != 0 {
		t.Fatal("failed read retained foreign evidence")
	}
}
