package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func nativeNavigationRoot(t *testing.T, m *nativeModel, id, title string) protocol.Session {
	t.Helper()
	var value protocol.CreateTreeResult
	err := m.connection.Call(t.Context(), "trees.create", protocol.CreateTreeParams{CreationID: protocol.ID(id), Definition: m.owner.Definition, WorkingDirectory: m.owner.WorkingDirectory, Engine: "starlark", Metadata: protocol.TreeMetadata{Title: &title}, Overrides: protocol.ConfigPatch{AutomaticTitle: new(false), GoalsEnabled: new(false), Model: &m.owner.Configuration.Model}}, &value)
	if err != nil || value.Root == nil {
		t.Fatal(err, value)
	}
	return *value.Root
}

func nativeNavigate(t *testing.T, m *nativeModel, id string) {
	t.Helper()
	command := m.resumeSession(id)
	if command == nil {
		t.Fatal(m.status)
	}
	_, read := m.Update(command())
	if read == nil || m.ready {
		t.Fatal("attachment did not request new canonical history", m.status)
	}
	m.Update(read())
	if !m.ready {
		t.Fatal(m.status)
	}
}

func TestNativeNavigationKeepsHostWorkAndRejectsLateOwnerEvidence(t *testing.T) {
	m, provider := nativeUIFixture(t)
	first := m.owner
	second := nativeNavigationRoot(t, m, "second", "Second root")
	input := nativeUISubmit(t, m, "hold")
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("host did not enter held turn")
	}
	nativeUIRead(t, m)
	previous := nativeRead{generation: m.generation, owner: &first, activity: m.activity}
	oldControl := nativeControlResult{generation: m.generation, label: "old owner", owner: &first}
	oldRead := m.read()
	oldCancel := m.readCancel
	m.noteRevisions = [2]string{"one", "two"}
	nativeNavigate(t, m, string(second.ID))
	if m.owner.ID != second.ID || m.history.owner != second.ID || len(m.history.messages) != 0 || m.noteRevisions != [2]string{} {
		t.Fatal("old owner projection remained", m.owner, m.history)
	}
	// The old command had not started yet. Its cancelled scope must still
	// complete safely and cannot clear the new owner's in-flight-read guard.
	oldCancel()
	newRead := m.read()
	m.Update(oldRead())
	m.Update(previous)
	m.Update(oldControl)
	if !m.reading || m.owner.ID != second.ID || m.activity.SessionID != second.ID {
		t.Fatal("late result changed the selected owner or read guard")
	}
	m.Update(newRead())
	if !m.ready {
		t.Fatal(m.status)
	}
	status, found, err := input.command.Check(t.Context())
	if err != nil || !found || status.Turn == nil || status.Turn.State != "running" {
		t.Fatal("navigation stopped or lost the old host turn", status, err)
	}
	close(provider.release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished, err := input.command.Wait(ctx)
	if err != nil || finished.Turn == nil || finished.Turn.State != "succeeded" {
		t.Fatal("old host work did not complete after terminal detach", finished, err)
	}
}

func TestNativeResumePickerAndExactChildNavigation(t *testing.T) {
	m, _ := nativeUIFixture(t)
	root := m.owner
	other := nativeNavigationRoot(t, m, "other", "Other root")
	command := m.command("/resume")
	if command == nil {
		t.Fatal(m.status)
	}
	result := command().(nativeNavigationResult)
	m.Update(result)
	if result.value.err != nil || m.picker == nil || len(m.picker.items) != 2 || !strings.Contains(m.View().Content, "Other root") {
		t.Fatal(result.value.err, m.status, m.picker)
	}
	for i, item := range m.picker.items {
		if item.RootID == other.ID {
			m.picker.sel = i
		}
	}
	_, command = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal(m.status)
	}
	_, read := m.Update(command())
	m.Update(read())
	if m.owner.ID != other.ID || m.picker != nil {
		t.Fatal("picker did not attach exact root", m.owner)
	}
	var spawned protocol.SpawnSessionResult
	err := m.connection.Call(t.Context(), "sessions.spawn", protocol.SpawnSessionParams{ParentID: root.ID, Identity: protocol.RequestIdentity{ClientID: "navigation", RequestID: "child"}, GrantIDs: []protocol.ID{}, Parts: []protocol.Part{{Type: "text", Text: "child input"}}}, &spawned)
	if err != nil || spawned.Session == nil {
		t.Fatal(err, spawned)
	}
	nativeNavigate(t, m, string(spawned.Session.ID))
	if m.owner.ParentID == nil || *m.owner.ParentID != root.ID || m.owner.TreeID != root.TreeID {
		t.Fatal("child navigation changed lineage", m.owner)
	}
	nativeNavigate(t, m, string(root.ID))
	if m.owner.ID != root.ID {
		t.Fatal("root navigation failed")
	}
	command = m.resumeSession("")
	m.Update(command())
	_, command = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.picker != nil || m.controlling {
		t.Fatal("picker did not close while navigation read was pending")
	}
	m.Update(command())
	if m.owner.ID != root.ID || m.picker != nil {
		t.Fatal("cancelled picker selection later changed owner")
	}
}

func TestNativeResumePrefixesAndPendingActionGuards(t *testing.T) {
	m, _ := nativeUIFixture(t)
	other := nativeNavigationRoot(t, m, "other", "Other")
	for _, owner := range []protocol.Session{m.owner, other} {
		prefix := string(owner.ID[:len(owner.ID)-1])
		got, err := resolveNativeSession(t.Context(), m.connection, prefix)
		if err != nil || got.ID != owner.ID {
			t.Fatal("unique root prefix failed", got, err)
		}
	}
	if _, err := resolveNativeSession(t.Context(), m.connection, string(m.owner.ID[:5])); err == nil {
		t.Fatal("ambiguous root prefix accepted")
	}
	if _, err := resolveNativeSession(t.Context(), m.connection, "missing"); err == nil {
		t.Fatal("missing root accepted")
	}
	m.standingDraft = &protocol.WriteHostStandingInstructionsParams{Text: "retained draft"}
	if command := m.resumeSession(string(other.ID)); command != nil || m.standingDraft.Text != "retained draft" {
		t.Fatal("navigation discarded unsaved standing draft")
	}
	m.standingDraft = nil
	m.uncertain = &client.InputCommand{}
	if command := m.resumeSession(string(other.ID)); command != nil || m.uncertain == nil {
		t.Fatal("navigation lost an uncertain input identity")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := resolveNativeSession(ctx, m.connection, string(other.ID)); err == nil {
		t.Fatal("cancelled navigation read completed")
	}
}
