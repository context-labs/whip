package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func nativeShellFixture(t *testing.T, m *nativeModel) *client.InputCommand {
	t.Helper()
	nativeMenuRPC[protocol.Grant](t, m.connection, "grants.create", protocol.CreateGrantParams{ID: "shell-keys", SessionID: m.owner.ID, Capability: "shell.run", Resource: m.owner.WorkingDirectory})
	command, err := m.connection.PrepareInput("shell.run", protocol.RunShellParams{Identity: protocol.RequestIdentity{ClientID: "shell-test", RequestID: "run"}, SessionID: m.owner.ID, Interactive: true, Timeout: new(10.0), Command: "stty -echo; printf ready; read value; printf '%s' \"$value\" > received.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := command.Send(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		nativeUIRead(t, m)
		if m.shell != nil && strings.Contains(m.shell.text, "ready") {
			return command
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("foreground shell never appeared", m.status)
	return nil
}

func nativeShellFocusFixture(t *testing.T, m *nativeModel) {
	t.Helper()
	command := m.commandKeepingDraft("/shell focus")
	if command == nil {
		t.Fatal("focus was not prepared", m.status)
	}
	m.Update(command())
	if m.shellFocus == nil {
		t.Fatal("focus was not acknowledged", m.status)
	}
}

func TestNativeShellRealForegroundRequiresFocusAndPreservesDraft(t *testing.T) {
	m, _ := nativeUIFixture(t)
	if m.shell != nil {
		t.Fatal("idle observation created a shell")
	}
	accepted := nativeShellFixture(t, m)
	m.input.SetValue("unsubmitted composer")
	saved := m.captureDraft()
	if !strings.Contains(nativeDisplayText(m.View().Content), "Interactive shell") {
		t.Fatal("passive shell preview absent")
	}
	m.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	if m.input.Value() != saved.text+"!" || m.shellFocus != nil {
		t.Fatal("passive observation stole typing")
	}
	m.applyDraft(saved)
	nativeShellFocusFixture(t, m)
	_, command := m.Update(tea.PasteMsg{Content: "private-text"})
	if command == nil || !reflect.DeepEqual(saved, m.captureDraft()) {
		t.Fatal("focused shell changed composer")
	}
	// Enter waits behind the one in-flight bounded write. Its acknowledgment
	// admits a new sequence, rather than replaying the prior one.
	if _, next := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); next != nil {
		t.Fatal("two shell writes dispatched concurrently")
	}
	_, next := m.Update(command())
	if next == nil {
		t.Fatal("acknowledged input did not flush pending enter", m.status)
	}
	m.Update(next())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished, err := accepted.Wait(ctx)
	if err != nil || finished.Turn == nil || finished.Turn.State != "succeeded" {
		t.Fatal(finished.Turn, err)
	}
	text, err := os.ReadFile(filepath.Join(m.owner.WorkingDirectory, "received.txt"))
	if err != nil || string(text) != "private-text" {
		t.Fatal(string(text), err)
	}
	nativeUIRead(t, m)
	if m.shell != nil || m.shellFocus != nil || !reflect.DeepEqual(saved, m.captureDraft()) {
		t.Fatal("settled shell retained focus or lost composer")
	}
	page, err := m.handle.History(t.Context(), protocol.HistoryPageParams{Direction: "backward", Limit: 64})
	if err != nil || len(page.Messages) != 0 {
		t.Fatal("human shell keys entered conversation history", page.Messages, err)
	}
}

func TestNativeShellBoundsUnknownWriteAndStaleAcknowledgement(t *testing.T) {
	m := nativeSelectionFixture(t)
	f := &nativeShellFocus{owner: "owner", epoch: "boot", operation: "op", next: 7, inflight: true}
	m.shellFocus = f
	m.shell = &nativeShellView{owner: f.owner, epoch: f.epoch, operation: f.operation, next: f.next, text: strings.Repeat("tail\n", 30)}
	m.queueShellInput(strings.Repeat("x", 16<<10))
	m.queueShellInput("extra")
	if len(f.queued) != 16<<10 || !strings.Contains(m.status, "refused") {
		t.Fatal("buffer overflow was not refused")
	}
	buffer := f.queued
	if command := m.shellSent(nativeShellSent{focus: f, sequence: 7, err: errors.New("ack lost")}); command != nil || m.shellFocus != nil {
		t.Fatal("unknown shell input was replayed")
	}
	if strings.Trim(string(buffer), "\x00") != "" {
		t.Fatal("unsent keys retained after uncertain outcome")
	}
	fresh := &nativeShellFocus{owner: "owner", epoch: "boot2", operation: "op2", next: 1, inflight: true, queued: []byte("new")}
	m.shellFocus = fresh
	if command := m.shellSent(nativeShellSent{focus: f, sequence: 7}); command != nil || !fresh.inflight || string(fresh.queued) != "new" {
		t.Fatal("old acknowledgement advanced a new operation")
	}
	m.observeShell(&nativeShellView{owner: "owner", epoch: "boot3", operation: "op2", next: 1}, nil)
	if m.shellFocus != nil {
		t.Fatal("process replacement retained key authority")
	}
}

func TestNativeShellDetachLeavesAcceptedHostOperationRunning(t *testing.T) {
	m, _ := nativeUIFixture(t)
	accepted := nativeShellFixture(t, m)
	nativeShellFocusFixture(t, m)
	focus := m.shellFocus
	m.close()
	if m.shellFocus != nil {
		t.Fatal("detach retained focus")
	}
	result, found, err := accepted.Check(t.Context())
	if err != nil || !found || result.Turn == nil || result.Turn.FinishedAt != nil {
		t.Fatal("detaching cancelled accepted host work", result.Turn, err)
	}
	var acknowledged protocol.ShellInputResult
	if err := m.connection.CallAtEpoch(t.Context(), focus.epoch, "shell.input", protocol.ShellInputParams{SessionID: focus.owner, OperationID: focus.operation, Sequence: focus.next, DataBase64: base64.StdEncoding.EncodeToString([]byte("finish\r"))}, &acknowledged); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := accepted.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNativeShellKeyFocusIsExplicitAndControlsHaveExactBytes(t *testing.T) {
	m := nativeSelectionFixture(t)
	f := &nativeShellFocus{owner: "owner", epoch: "boot", operation: "op", next: 1, inflight: true}
	m.shellFocus = f
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyUp}, {Code: tea.KeyEnter}, {Code: tea.KeyBackspace}, {Code: tea.KeyTab}, {Code: tea.KeyEscape}, {Code: 'd', Mod: tea.ModCtrl}, {Code: 'x', Text: "x", Mod: tea.ModAlt}} {
		if _, handled := m.shellKey(key); !handled {
			t.Fatal("focused key escaped to composer")
		}
	}
	if string(f.queued) != "\x1b[A\r\x7f\t\x1b\x04\x1bx" {
		t.Fatalf("terminal keys changed: %q", f.queued)
	}
	m.shellKey(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if m.shellFocus != nil || strings.Trim(string(f.queued), "\x00") != "" {
		t.Fatal("return-to-draft kept unsent keys")
	}
}

func TestNativeShellLateFocusNeverStealsNewTyping(t *testing.T) {
	m := nativeSelectionFixture(t)
	request := &nativeShellFocus{owner: m.owner.ID, epoch: "boot"}
	m.shellPending = request
	m.history.epoch = "boot"
	m.input.SetValue("draft")
	m.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	m.Update(nativeShellFocused{request: request, generation: m.generation, view: &nativeShellView{owner: m.owner.ID, epoch: "boot", operation: "op", next: 1}})
	if m.shellFocus != nil || m.input.Value() != "draft!" {
		t.Fatal("late focus took over composer input")
	}
}
