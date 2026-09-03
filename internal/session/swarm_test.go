package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

func newSwarmFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	return store, rootID, authority.AgentID
}

func admitTestChild(t *testing.T, store *Store, rootID, parentID, childID string) {
	t.Helper()
	if _, err := store.AdmitAgent(context.Background(), AgentAdmission{
		RootID: rootID, ParentAgentID: parentID, ChildAgentID: childID, Name: childID,
		Model: "model", Provider: "provider", CWD: t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentAdmissionAndRelativeDiscovery(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "parent")
	admitTestChild(t, store, rootID, "parent", "child")
	admitTestChild(t, store, rootID, "parent", "sibling")

	relatives, err := store.ListAgentRelatives(t.Context(), rootID, "child")
	if err != nil {
		t.Fatal(err)
	}
	if relatives.Parent == nil || relatives.Parent.ID != "parent" || len(relatives.Siblings) != 1 || relatives.Siblings[0].ID != "sibling" {
		t.Fatalf("relatives=%+v", relatives)
	}
	if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
		RootID: rootID, ParentAgentID: "parent", ChildAgentID: "duplicate", Name: "child",
		Model: "model", Provider: "provider", CWD: t.TempDir(),
	}); err == nil {
		t.Fatal("duplicate sibling name was accepted")
	}
}

func TestAgentAdmissionPersistsPromptAtomically(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
		RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "queued", Name: "queued",
		Model: "model", Provider: "provider", CWD: t.TempDir(), Prompt: RuntimePayload{Data: []byte("begin")},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.LoadQueuedInbox(t.Context(), rootID, "queued", 0, 10)
	if err != nil || len(items) != 1 || items[0].Kind != "submit" || string(items[0].Payload.Inline) != "begin" {
		t.Fatalf("queued prompt=%+v err=%v", items, err)
	}

	if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
		RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "denied", Name: "denied",
		Capabilities: []CapabilityDelegation{{AgentID: "other"}}, Prompt: RuntimePayload{Data: []byte("must not survive")},
	}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("denied admission error=%v", err)
	}
	var agents, inbox int
	_ = store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM agents WHERE id='denied'`).Scan(&agents)
	_ = store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM inbox WHERE agent_id='denied'`).Scan(&inbox)
	if agents != 0 || inbox != 0 {
		t.Fatalf("denied admission persisted agents=%d inbox=%d", agents, inbox)
	}
}

func TestRecoveryKeepsQueuedAgentPromptButInterruptsRunningInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.EnsureAuthority(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	for _, child := range []string{"queued", "running"} {
		if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
			RootID: rootID, ParentAgentID: root.AgentID, ChildAgentID: child, Name: child,
			Prompt: RuntimePayload{Data: []byte(child + " prompt")},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.StartAgentTurn(t.Context(), rootID, "running", "running-turn"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	queued, err := store.LoadQueuedInbox(t.Context(), rootID, "queued", 0, 10)
	if err != nil || len(queued) != 1 || string(queued[0].Payload.Inline) != "queued prompt" {
		t.Fatalf("queued prompt after recovery=%+v err=%v", queued, err)
	}
	var inputStatus, turnStatus string
	if err := store.db.QueryRowContext(t.Context(), `SELECT status FROM inbox WHERE root_id=? AND agent_id='running'`, rootID).Scan(&inputStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT status FROM turns WHERE id='running-turn'`).Scan(&turnStatus); err != nil {
		t.Fatal(err)
	}
	if inputStatus != "interrupted" || turnStatus != "interrupted" {
		t.Fatalf("uncertain running work input=%q turn=%q", inputStatus, turnStatus)
	}
}

func TestAgentTerminalizationPreservesLineage(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "parent")
	admitTestChild(t, store, rootID, "parent", "child")
	admitTestChild(t, store, rootID, rootAgentID, "sibling")

	if _, err := store.TerminalizeSubtree(t.Context(), rootID, "parent", "child", "deleted"); err != nil {
		t.Fatal(err)
	}
	var parent, status string
	if err := store.db.QueryRowContext(t.Context(), `SELECT parent_id,status FROM agents WHERE id='child'`).Scan(&parent, &status); err != nil {
		t.Fatal(err)
	}
	if parent != "parent" || status != "deleted" {
		t.Fatalf("child parent=%q status=%q", parent, status)
	}
	if _, err := store.TerminalizeSubtree(t.Context(), rootID, "sibling", "parent", "stopped"); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("unrelated stop error=%v", err)
	}
}

func TestAgentAdmissionValidation(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	for _, admission := range []AgentAdmission{
		{},
		{RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: rootAgentID},
		{RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "bad-budget", Budgets: []BudgetLimit{{Limit: 1}}},
		{RootID: rootID, ParentAgentID: rootAgentID, ChildAgentID: "negative", Budgets: []BudgetLimit{{Kind: BudgetTokens, Limit: -1}}},
	} {
		if _, err := store.AdmitAgent(t.Context(), admission); err == nil {
			t.Fatalf("invalid admission accepted: %+v", admission)
		}
	}
}
