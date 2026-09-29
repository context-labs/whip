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

	"github.com/context-labs/whip/internal/protocol"
)

func awaitNativeDecisions(t *testing.T, m *nativeModel) nativeDecision {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	for {
		page, err := readNativeDecisions(ctx, m.connection, m.owner)
		if err != nil {
			t.Fatal(err)
		}
		m.applyDecisions(page)
		if len(page.items) > 0 {
			return page.items[0]
		}
		select {
		case <-ctx.Done():
			t.Fatal("no pending native decision", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func nativeDecisionKey(t *testing.T, m *nativeModel, value string) tea.Cmd {
	t.Helper()
	key := tea.KeyPressMsg{Text: value, Code: []rune(value)[0]}
	switch value {
	case "enter":
		key = tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		key = tea.KeyPressMsg{Code: tea.KeyEsc}
	case "tab":
		key = tea.KeyPressMsg{Code: tea.KeyTab}
	case "down":
		key = tea.KeyPressMsg{Code: tea.KeyDown}
	case "space":
		key = tea.KeyPressMsg{Code: tea.KeySpace}
	}
	_, command := m.Update(key)
	return command
}

func finishNativeDecision(t *testing.T, m *nativeModel, command tea.Cmd) nativeControlResult {
	t.Helper()
	if command == nil {
		t.Fatal("decision command missing", m.status)
	}
	result, ok := command().(nativeControlResult)
	if !ok {
		t.Fatal("wrong decision result")
	}
	m.Update(result)
	return result
}

func TestNativePermissionDialogOwnsExactScopeAndExplicitRetry(t *testing.T) {
	m, provider := nativeUIFixture(t)
	provider.codes = map[string]string{"write": `files.write(path="approved.txt",content="only this file")`}
	input := nativeUISubmit(t, m, "write")
	decision := awaitNativeDecisions(t, m)
	if decision.question != nil || !decision.root || decision.capability != "files.write" || !strings.Contains(m.View().Content, "approved.txt") {
		t.Fatal(decision, m.View().Content)
	}
	m.input.SetValue("my next prompt draft")
	command := nativeDecisionKey(t, m, "s")
	if result := finishNativeDecision(t, m, command); result.err != nil || m.input.Value() != "my next prompt draft" {
		t.Fatal(result.err, m.input.Value())
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := input.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(m.owner.WorkingDirectory, "approved.txt")); err != nil || string(raw) != "only this file" {
		t.Fatal(string(raw), err)
	}
	var before protocol.GrantsResult
	if err := m.connection.Call(ctx, "grants.list", protocol.GrantsParams{SessionID: m.owner.ID, Limit: 100}, &before); err != nil {
		t.Fatal(err)
	}
	// Replaying only this frozen command after an acknowledgement loss must
	// use the same standing-grant ID and original permission operation.
	m.Update(nativeControlResult{mutation: true, err: errors.New("lost acknowledgement"), retry: command})
	m.decision = newNativeDecision(decision, m.width)
	if result := finishNativeDecision(t, m, nativeDecisionKey(t, m, "r")); result.err != nil {
		t.Fatal(result.err)
	}
	var after protocol.GrantsResult
	if err := m.connection.Call(ctx, "grants.list", protocol.GrantsParams{SessionID: m.owner.ID, Limit: 100}, &after); err != nil || len(after.Items) != len(before.Items) {
		t.Fatal("decision retry created another grant", after, err)
	}
	standing := 0
	for _, grant := range after.Items {
		if grant.OperationID == nil {
			standing++
			if grant.Capability != decision.capability || grant.Resource != decision.resource || grant.SessionID != decision.owner {
				t.Fatal("standing grant broadened captured scope", grant)
			}
		}
	}
	if standing != 1 {
		t.Fatal("wrong standing grant count", standing)
	}
}

func TestNativePermissionHideAndExternalDenialDoNotGrant(t *testing.T) {
	m, provider := nativeUIFixture(t)
	provider.codes = map[string]string{"write": `files.write(path="denied.txt",content="must not happen")`}
	nativeUISubmit(t, m, "write")
	decision := awaitNativeDecisions(t, m)
	nativeDecisionKey(t, m, "esc")
	if m.decision != nil {
		t.Fatal("dialog did not hide")
	}
	var pending protocol.PermissionsResult
	if err := m.connection.Call(t.Context(), "permissions.list", protocol.PermissionsParams{SessionID: m.owner.ID, PendingOnly: true, Limit: 100}, &pending); err != nil || len(pending.Items) != 1 {
		t.Fatal("hiding resolved the permission", pending, err)
	}
	nativeDecisionKey(t, m, "tab")
	if m.decision == nil || m.decision.value.id != decision.id {
		t.Fatal("pending dialog could not reopen")
	}
	var denied protocol.Permission
	if err := m.connection.Call(t.Context(), "permissions.resolve", protocol.ResolvePermissionParams{OperationID: decision.id, Approved: false}, &denied); err != nil {
		t.Fatal(err)
	}
	page, err := readNativeDecisions(t.Context(), m.connection, m.owner)
	if err != nil {
		t.Fatal(err)
	}
	m.applyDecisions(page)
	if m.decision != nil || len(m.decisions) != 0 {
		t.Fatal("settled dialog remained actionable")
	}
	if _, err := os.Stat(filepath.Join(m.owner.WorkingDirectory, "denied.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("denied effect happened", err)
	}
}

func TestNativeChildDecisionCannotCreateRootAuthority(t *testing.T) {
	m, _ := nativeUIFixture(t)
	child := nativeDecision{id: "operation", owner: "child", capability: "files.write", resource: "captured-child-scope"}
	m.applyDecisions(&nativeDecisionPage{items: []nativeDecision{child}})
	if strings.Contains(m.View().Content, "allow once") || !strings.Contains(m.View().Content, "delegated authority") {
		t.Fatal("child offered root approvals", m.View().Content)
	}
	for _, key := range []string{"a", "s"} {
		if command := nativeDecisionKey(t, m, key); command != nil || !strings.Contains(m.status, "delegated grant") {
			t.Fatal("child approval constructed a mutation", m.status)
		}
	}
	// Adapter-only coverage: no child pending operation is fabricated in SQL.
	m.input.SetValue("draft must not be consumed")
	m.decision = nil
	m.decisionsHidden = false
	m.applyDecisions(&nativeDecisionPage{items: []nativeDecision{child}})
	if m.decision != nil || m.input.Value() != "draft must not be consumed" {
		t.Fatal("new decision stole a partially typed prompt")
	}
}
