package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func queueOutcomeTurn(t *testing.T, store *Store, root, child, turn string) {
	t.Helper()
	if _, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: child, Kind: "submit", Payload: RuntimePayload{Data: []byte("work")}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartAgentTurn(t.Context(), root, child, turn); err != nil {
		t.Fatal(err)
	}
}

func savedOutcome(t *testing.T, store *Store, root, child string) *TurnOutcome {
	t.Helper()
	var outcome *TurnOutcome
	if err := store.db.QueryRowContext(t.Context(), `SELECT last_turn FROM agents WHERE root_id=? AND id=?`, root, child).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	return outcome
}

func TestTurnOutcomeSurvivesWithoutExecutions(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	child := NewAgentID()
	admitTestChild(t, store, root, root, child)
	queueOutcomeTurn(t, store, root, child, "failed-turn")
	message := strings.Repeat("é", 4000)
	if err := store.FinishAgentTurn(t.Context(), root, child, AgentTurnCommit{TurnID: "failed-turn", Status: "failed", Error: message}); err != nil {
		t.Fatal(err)
	}
	outcome := savedOutcome(t, store, root, child)
	if outcome == nil || outcome.Status != "failed" || outcome.TurnID != "failed-turn" || outcome.StartedAt == "" || outcome.FinishedAt == "" || !outcome.ErrorTruncated || outcome.ErrorDetails == nil || len(outcome.Error) > 4096 || !utf8.ValidString(outcome.Error) {
		t.Fatalf("outcome=%+v", outcome)
	}
	body, _, err := store.ReadContent(t.Context(), outcome.ErrorDetails.ReferenceID, root, child, 0, MaxContentRead)
	if err != nil || string(body) != message {
		t.Fatalf("details=%d err=%v", len(body), err)
	}
	snapshot, err := store.SnapshotRoot(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range snapshot.Agents {
		if agent.ID == child && (agent.Status != "idle" || agent.LastTurn == nil || agent.LastTurn.Status != "failed") {
			t.Fatalf("agent=%+v", agent)
		}
	}
	page, err := store.RootCollectionPage(t.Context(), root, "agents", CollectionPageOptions{Limit: 128, MaxBytes: 64 << 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		if item.Agent != nil && item.Agent.ID == child && item.Agent.LastTurn == nil {
			t.Fatal("paged agent lost outcome")
		}
	}
	events, _, err := store.ReplayEvents(t.Context(), root, 0, 128)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "agent.turn.failed" {
			body, err := store.ResolveRuntimeValue(t.Context(), root, event.Payload)
			if err != nil {
				t.Fatal(err)
			}
			var value LifecycleEvent
			if err := json.Unmarshal(body, &value); err != nil {
				t.Fatal(err)
			}
			if value.TurnID != "failed-turn" {
				t.Fatal("terminal event omitted turn ID")
			}
		}
	}
	// Clearing the root's history and pruning event presentation cannot erase a child's result.
	if err := store.ClearMessages(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendRootEvent(t.Context(), root, "stream.text", RuntimePayload{Data: []byte(`{"text":"parent continues"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM events WHERE root_id=? AND seq<(SELECT MAX(seq) FROM events WHERE root_id=?)`, root, root); err != nil {
		t.Fatal(err)
	}
	if savedOutcome(t, store, root, child).Status != "failed" {
		t.Fatal("lost child outcome")
	}
	queueOutcomeTurn(t, store, root, child, "successful-turn")
	if savedOutcome(t, store, root, child).Error != "" {
		t.Fatal("new turn retained old error")
	}
	if err := store.FinishAgentTurn(t.Context(), root, child, AgentTurnCommit{TurnID: "successful-turn", Status: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	if savedOutcome(t, store, root, child).Status != "succeeded" {
		t.Fatal("successful follow-up lost")
	}
}

func TestTurnOutcomeSubtreeInterruption(t *testing.T) {
	store, root, _ := newSwarmFixture(t)
	admitTestChild(t, store, root, root, "child")
	queueOutcomeTurn(t, store, root, "child", "running-turn")
	if _, err := store.TerminalizeSubtree(t.Context(), root, root, "child", "stopped"); err != nil {
		t.Fatal(err)
	}
	if got := savedOutcome(t, store, root, "child"); got.Status != "interrupted" || got.TurnID != "running-turn" {
		t.Fatalf("outcome=%+v", got)
	}
}

func TestLegacyTurnOutcomeMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	admitTestChild(t, store, root, root, "legacy:child")
	admitTestChild(t, store, root, root, "unknown")
	admitTestChild(t, store, root, root, "stopped")
	queueOutcomeTurn(t, store, root, "stopped", "stopped-turn")
	if _, err := store.TerminalizeSubtree(t.Context(), root, root, "stopped", "stopped"); err != nil {
		t.Fatal(err)
	}
	// Before this change subtree stopping updated turns without this event.
	if _, err := store.db.ExecContext(t.Context(), `DELETE FROM events WHERE kind='agent.turn.interrupted' AND json_extract(payload_inline,'$.agent_id')='stopped'`); err != nil {
		t.Fatal(err)
	}
	for _, event := range []struct {
		kind    string
		payload LifecycleEvent
	}{
		{"agent.turn.started", LifecycleEvent{AgentID: "legacy:child", TurnID: "first"}},
		{"agent.turn.failed", LifecycleEvent{AgentID: "legacy:child", Error: "first failure"}},
		{"agent.turn.started", LifecycleEvent{AgentID: "legacy:child", TurnID: "second"}},
		{"agent.turn.failed", LifecycleEvent{AgentID: "legacy:child", Error: strings.Repeat("long failure ", 1000)}},
		{"agent.turn.failed", LifecycleEvent{AgentID: "unknown", Error: "start was pruned"}},
	} {
		body, _ := json.Marshal(event.payload)
		if _, err := store.AppendRootEvent(t.Context(), root, event.kind, RuntimePayload{Data: body}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(t.Context(), `ALTER TABLE agents DROP COLUMN last_turn; UPDATE runtime_schema SET identity='whip-recursive-runtime-v11'; PRAGMA user_version=11`); err != nil {
		t.Fatal(err)
	}
	conn, err := store.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// An opener that first observed v10 may acquire the lock at this v11 boundary.
	err = upgradeV10(t.Context(), conn)
	if closeErr := conn.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	if err != nil {
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
	outcome := savedOutcome(t, store, root, "legacy:child")
	if outcome == nil || outcome.Status != "failed" || outcome.TurnID != "second" || !outcome.ErrorTruncated || outcome.ErrorDetails == nil {
		t.Fatalf("restored=%+v", outcome)
	}
	if got := savedOutcome(t, store, root, "unknown"); got == nil || got.TurnID != "" || got.Error != "start was pruned" {
		t.Fatalf("unknown=%+v", got)
	}
	if got := savedOutcome(t, store, root, "stopped"); got == nil || got.Status != "interrupted" || got.TurnID != "stopped-turn" || got.Error != "" {
		t.Fatalf("stopped=%+v", got)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := savedOutcome(t, store, root, "legacy:child"); got.EventSeq != outcome.EventSeq {
		t.Fatal("reopen changed outcome")
	}
}
