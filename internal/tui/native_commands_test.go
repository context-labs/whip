package tui

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func nativeUIControl(t *testing.T, m *nativeModel, text string) nativeControlResult {
	t.Helper()
	m.input.SetValue(text)
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("control not prepared", m.status)
	}
	value, ok := command().(nativeControlResult)
	if !ok {
		t.Fatal("wrong command result")
	}
	m.Update(value)
	return value
}

func TestNativeUIWorkspaceNamingClearAndStaleConfiguration(t *testing.T) {
	m, _ := nativeUIFixture(t)
	working := m.owner.WorkingDirectory
	child := filepath.Join(working, "new directory")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	var err error
	child, err = filepath.EvalSymlinks(child)
	if err != nil {
		t.Fatal(err)
	}
	before := m.owner
	if result := nativeUIControl(t, m, "/cd new directory"); result.err != nil || m.owner.WorkingDirectory != child {
		t.Fatal(result.err, m.status)
	}
	changed := m.owner
	m.Update(nativeRead{generation: m.generation, owner: &before, activity: m.activity, observer: m.observer})
	m.Update(nativeControlResult{owner: &before})
	if m.owner.ConfigRevision != changed.ConfigRevision {
		t.Fatal("late control/read replaced newer configuration")
	}
	m.owner = before // Another terminal can change config after this UI's last read.
	if result := nativeUIControl(t, m, "/cd .."); result.err == nil {
		t.Fatal("stale config silently overwritten")
	}
	if m.retryControl != nil {
		t.Fatal("definite CAS conflict became an uncertain retry")
	}
	if value, err := m.handle.Get(t.Context()); err != nil || value.WorkingDirectory != child {
		t.Fatal(value, err)
	}
	if result := nativeUIControl(t, m, "/pwd"); result.err != nil || m.owner.ConfigRevision != changed.ConfigRevision {
		t.Fatal(result.err, m.owner)
	}
	if result := nativeUIControl(t, m, "/rename Evidence preserved"); result.err != nil {
		t.Fatal(result.err)
	}
	var tree protocol.Tree
	if err := m.connection.Call(t.Context(), "trees.get", protocol.TreeParams{TreeID: m.owner.TreeID}, &tree); err != nil || tree.Metadata.Title == nil || *tree.Metadata.Title != "Evidence preserved" {
		t.Fatal(tree, err)
	}
	input := nativeUISubmit(t, m, "before clear")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := input.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	nativeUIRead(t, m)
	oldGeneration := m.generation
	oldPage, err := m.handle.History(ctx, protocol.HistoryPageParams{Direction: "backward", Limit: 64})
	if err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("/clear")
	if command := m.submit(); command != nil || !strings.Contains(m.status, "requires a stopped") {
		t.Fatal("clear silently stopped an active owner", m.status)
	}
	if result := nativeUIControl(t, m, "/stop"); result.err != nil || m.owner.Lifecycle != "stopped" {
		t.Fatal(result.err, m.owner.Lifecycle)
	}
	if result := nativeUIControl(t, m, "/clear"); result.err != nil {
		t.Fatal(result.err)
	}
	m.Update(nativeRead{generation: oldGeneration, page: &oldPage})
	if m.ready || len(m.history.messages) != 0 {
		t.Fatal("old read restored cleared history")
	}
	nativeUIRead(t, m)
	if len(m.history.messages) != 0 || m.owner.WorkingDirectory != child {
		t.Fatal("clear changed workspace or kept live history")
	}
	if result := nativeUIControl(t, m, "/start"); result.err != nil || m.owner.Lifecycle != "active" {
		t.Fatal(result.err, m.owner.Lifecycle)
	}
	original, err := m.handle.Input(ctx, input.admission.Input.ID)
	if err != nil || original.ID != input.admission.Input.ID {
		t.Fatal("clear deleted input receipt", original, err)
	}
}

func TestNativeUIExplicitQueueAndSteerKeepOriginalInput(t *testing.T) {
	m, p := nativeUIFixture(t)
	nativeUISubmit(t, m, "hold")
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("turn not active")
	}
	nativeUIRead(t, m)
	target := m.activity.ActiveTurn.ID
	queued := nativeUISubmit(t, m, "/queue after the active turn")
	if queued.admission.Input.State != "queued" || queued.admission.Input.Steering != nil {
		t.Fatal(queued.admission.Input)
	}
	steered := nativeUISubmit(t, m, "/steer keep the active target")
	if steered.admission.Input.Steering == nil || steered.admission.Input.Steering.TurnID != target || steered.admission.Input.Parts[0].Text != "keep the active target" {
		t.Fatal(steered.admission.Input)
	}
	close(p.release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := queued.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestNativeUIUnknownControlRetryIsExplicitAndSurvivesReads(t *testing.T) {
	m, _ := nativeUIFixture(t)
	calls := 0
	original := m.control("original control", true, func(context.Context) nativeControlResult {
		calls++
		return nativeControlResult{err: errors.New("lost acknowledgement")}
	})
	m.Update(original())
	if m.retryControl == nil || calls != 1 {
		t.Fatal(calls, m.status)
	}
	if result := nativeUIControl(t, m, "/pwd"); result.err != nil || m.retryControl == nil {
		t.Fatal("read discarded unknown mutation", result.err)
	}
	m.input.SetValue("must not create another input")
	if command := m.submit(); command != nil || m.input.Value() == "" {
		t.Fatal("new input admitted over an unknown control")
	}
	if result := nativeUIControl(t, m, "/retry"); result.err == nil || calls != 2 || m.retryControl == nil {
		t.Fatal(calls, result.err)
	}
}

func TestNativeUICheckUsesOriginalReceiptWithoutResubmitting(t *testing.T) {
	m, _ := nativeUIFixture(t)
	first := nativeUISubmit(t, m, "only once")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := first.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	m.Update(nativeSubmission{command: first.command, err: errors.New("lost ack"), uncertain: true})
	m.input.SetValue("/check")
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal(m.status)
	}
	result := command().(nativeSubmission)
	m.Update(result)
	if result.err != nil || result.admission.Input.ID != first.admission.Input.ID || m.uncertain != nil {
		t.Fatal(result, m.status)
	}
	page, err := m.handle.Inputs(ctx, "all", nil, 20)
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	m.input.SetValue("/steer no active prompt")
	nativeUIRead(t, m)
	if command := m.submit(); command != nil || !strings.Contains(m.status, "no active prompt") {
		t.Fatal(m.status)
	}
	// A definitive host rejection never offers a blind mutation retry.
	m.Update(nativeControlResult{mutation: true, label: "edit", err: &client.Error{Kind: "CONFLICT"}, retry: func() tea.Msg { return nil }})
	if m.retryControl != nil {
		t.Fatal("rejection offered uncertain retry")
	}
}

func TestNativeUILifecycleFailureCannotBeReplayedAsOldIntent(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.Update(nativeControlResult{label: "Stop", mutation: true, inspectOnError: true, err: errors.New("lost ack")})
	if m.retryControl != nil || !strings.Contains(m.status, "/status") {
		t.Fatal("lifecycle failure created unsafe replay", m.status)
	}
	if result := nativeUIControl(t, m, "/status"); result.err != nil || !strings.Contains(m.status, "active") {
		t.Fatal(result.err, m.status)
	}
}
