package session

import (
	"errors"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

func TestLifecycleForkSnapshotsReadableContent(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	childID := rootID + ":child"
	admitTestChild(t, store, rootID, rootAgentID, childID)
	grants := []struct {
		name     string
		scope    ContentGrantScope
		agentID  string
		readable bool
	}{
		{"root", ContentGrantRoot, "", true},
		{"root-private", ContentGrantAgent, rootAgentID, true},
		{"root-subtree", ContentGrantSubtree, rootAgentID, true},
		{"child-private", ContentGrantAgent, childID, false},
		{"child-subtree", ContentGrantSubtree, childID, false},
		{"revoked", ContentGrantRoot, "", false},
	}
	refs := make(map[string]string, len(grants))
	for _, grant := range grants {
		value, err := store.StoreContent(t.Context(), ContentGrant{RootID: rootID, AgentID: grant.agentID, Scope: grant.scope}, RuntimePayload{
			Data: []byte(grant.name), MediaType: "text/plain", Source: "fork fixture",
		})
		if err != nil {
			t.Fatal(err)
		}
		refs[grant.name] = value.ReferenceID
	}
	if err := store.RevokeContentGrant(t.Context(), refs["revoked"], rootID, ""); err != nil {
		t.Fatal(err)
	}
	messages := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "inspect this artifact"},
		{Role: "assistant", Content: refs["root-private"]},
		{Role: "user", Content: "excluded tail"},
	}
	if err := store.Save(rootID, 1, messages, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	forkID, err := store.Fork(rootID, 2, "fork")
	if err != nil {
		t.Fatal(err)
	}
	// An unstarted fork has no live root agent, but its grants can be forked again.
	secondID, err := store.Fork(forkID, 2, "second fork")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{forkID, secondID} {
		_, history, err := store.Load(id)
		if err != nil || len(history) != 2 || history[1].Content != refs["root-private"] {
			t.Fatalf("fork history lost the original handle: %+v, %v", history, err)
		}
		if _, err := store.EnsureAuthority(t.Context(), id); err != nil {
			t.Fatal(err)
		}
		admitTestChild(t, store, id, id, id+":child")
		for _, grant := range grants {
			data, _, err := store.ReadContent(t.Context(), refs[grant.name], id, id, 0, 1024)
			if grant.readable {
				if err != nil || string(data) != grant.name {
					t.Fatalf("root cannot read inherited %s: %q, %v", grant.name, data, err)
				}
				var scope ContentGrantScope
				var owner string
				if err := store.db.QueryRowContext(t.Context(), `SELECT scope,agent_id FROM content_grants WHERE reference_id=? AND root_id=?`, refs[grant.name], id).Scan(&scope, &owner); err != nil {
					t.Fatal(err)
				}
				wantOwner := id
				if grant.scope == ContentGrantRoot {
					wantOwner = ""
				}
				if scope != grant.scope || owner != wantOwner {
					t.Fatalf("inherited grant changed scope or identity: %s/%s", scope, owner)
				}
			} else if !errors.Is(err, ErrContentAccess) {
				t.Fatalf("fork gained inaccessible %s: %q, %v", grant.name, data, err)
			}
			data, _, err = store.ReadContent(t.Context(), refs[grant.name], id, id+":child", 0, 1024)
			if grant.readable && grant.scope != ContentGrantAgent {
				if err != nil || string(data) != grant.name {
					t.Fatalf("child cannot read inherited %s: %q, %v", grant.name, data, err)
				}
			} else if !errors.Is(err, ErrContentAccess) {
				t.Fatalf("fork child gained private %s: %q, %v", grant.name, data, err)
			}
		}
	}
	for _, grant := range grants {
		if grant.readable {
			if err := store.RevokeContentGrant(t.Context(), refs[grant.name], rootID, grant.agentID); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, action := range []string{"revocation", "deletion"} {
		if action == "deletion" {
			if err := store.DeleteSession(t.Context(), rootID); err != nil {
				t.Fatal(err)
			}
		}
		for _, id := range []string{forkID, secondID} {
			for _, grant := range grants {
				if !grant.readable {
					continue
				}
				data, _, err := store.ReadContent(t.Context(), refs[grant.name], id, id, 0, 1024)
				if err != nil || string(data) != grant.name {
					t.Fatalf("source %s broke fork content %s: %q, %v", action, grant.name, data, err)
				}
			}
		}
	}
}

func lifecyclePopulateRoot(t *testing.T, store *Store) string {
	t.Helper()
	rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), rootID); err != nil {
		t.Fatal(err)
	}
	parentID := rootID
	for _, suffix := range []string{":child", ":grandchild"} {
		childID := rootID + suffix
		if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
			RootID: rootID, ParentAgentID: parentID, ChildAgentID: childID,
			Prompt: RuntimePayload{Data: []byte("queued work")},
		}); err != nil {
			t.Fatal(err)
		}
		turnID := childID + ":turn"
		start, err := store.StartAgentTurn(t.Context(), rootID, childID, turnID)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.FinishAgentTurn(t.Context(), rootID, childID, AgentTurnCommit{
			TurnID: turnID, Status: "succeeded", AcknowledgedInbox: []int64{start.Items[0].Seq},
			Transcript: []llm.Message{{Role: "user", Content: "work"}, {Role: "assistant", Content: "result"}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SendMailboxMessage(t.Context(), rootID, childID, parentID, MailboxSend{Body: "result"}); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveAgentScratch(t.Context(), rootID, childID, "n = 1", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SetPrivateState(t.Context(), rootID, childID, "note", RuntimePayload{Data: []byte("private")}); err != nil {
			t.Fatal(err)
		}
		parentID = childID
	}
	if _, err := store.CreateBlackboardSubscription(t.Context(), rootID, parentID, "plan"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetBlackboard(t.Context(), rootID, rootID, "plan", RuntimePayload{Data: []byte("ready")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddSchedule(rootID, "every 1h", "check", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(rootID, 1, []llm.Message{{Role: "system"}, {Role: "user", Content: "start"}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSnapshot(rootID, 1, "workspace-snapshot"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordCompaction(rootID, 1, "prior context"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitCommand(t.Context(), CommandAdmission{
		ClientID: rootID + ":client", CommandID: "queued", Scope: CommandScopeRoot,
		RootID: rootID, AgentID: rootID, Kind: "user", RequestDigest: "queued",
		Payload: RuntimePayload{Data: []byte("later input")},
	}); err != nil {
		t.Fatal(err)
	}
	// Persist an unfinished operation without launching an external effect.
	if _, err := store.db.ExecContext(t.Context(), `INSERT INTO operations(id,root_id,agent_id,status,created_at,updated_at)
		VALUES(?,?,?,'running',?,?)`, rootID+":operation", rootID, rootID, now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `INSERT INTO leases(id,root_id,agent_id,operation_id,status,created_at,updated_at)
		VALUES(?,?,?,?,'running',?,?)`, rootID+":lease", rootID, rootID, rootID+":operation", now(), now()); err != nil {
		t.Fatal(err)
	}
	return rootID
}

func lifecycleRows(t *testing.T, store *Store, rootID string) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	for _, table := range []string{
		"agents", "turns", "inbox", "agent_messages", "transcript_messages", "agent_state", "agent_scratch",
		"blackboard", "blackboard_history", "subscriptions", "capabilities", "budgets", "events", "content_grants",
		"commands", "operations", "leases", "permission_requests", "permission_rules", "usage_charges",
		"messages", "schedules", "compactions", "snapshots", "sessions",
	} {
		key := "root_id"
		switch table {
		case "messages", "schedules", "compactions", "snapshots":
			key = "session_id"
		case "sessions":
			key = "id"
		}
		var count int
		if err := store.db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table+" WHERE "+key+"=?", rootID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		counts[table] = count
	}
	return counts
}

func TestLifecycleForkExcludesRuntimeAndDeletePreservesOtherRoots(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID := lifecyclePopulateRoot(t, store)
	otherID := lifecyclePopulateRoot(t, store)
	otherBefore := lifecycleRows(t, store, otherID)
	forkID, err := store.Fork(rootID, 1, "history only")
	if err != nil {
		t.Fatal(err)
	}
	forkBefore := lifecycleRows(t, store, forkID)
	for table, count := range forkBefore {
		want := 0
		if table == "sessions" || table == "messages" {
			want = 1
		}
		if count != want {
			t.Fatalf("fork inherited live %s rows: %d", table, count)
		}
	}
	for range 2 {
		if err := store.DeleteSession(t.Context(), rootID); err != nil {
			t.Fatal(err)
		}
		for table, count := range lifecycleRows(t, store, rootID) {
			if count != 0 {
				t.Fatalf("deleted root retained %s rows: %d", table, count)
			}
		}
		if after := lifecycleRows(t, store, otherID); !maps.Equal(after, otherBefore) {
			t.Fatalf("deletion changed unrelated root: before=%v, after=%v", otherBefore, after)
		}
		if after := lifecycleRows(t, store, forkID); !maps.Equal(after, forkBefore) {
			t.Fatalf("deletion changed fork rows: before=%v, after=%v", forkBefore, after)
		}
	}
	meta, _, err := store.Load(forkID)
	if err != nil || meta.ForkedFrom != "" {
		t.Fatalf("surviving fork has deleted parent: %+v, %v", meta, err)
	}
}

func TestLifecycleForkRejectsMissingAndToolHostSources(t *testing.T) {
	store, rootID, _ := newSwarmFixture(t)
	toolHostID, err := store.Create(SessionKindToolHost, t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"missing", toolHostID} {
		if id, err := store.Fork(source, 0, "invalid"); err == nil || id != "" {
			t.Fatalf("fork accepted invalid source %q: %q, %v", source, id, err)
		}
	}
	var sessions int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM sessions`).Scan(&sessions); err != nil || sessions != 2 {
		t.Fatalf("invalid fork inserted a session: %d, %v (root %s)", sessions, err, rootID)
	}
}

func TestLifecycleForkAndDeleteRollBack(t *testing.T) {
	t.Run("fork grant copy", func(t *testing.T) {
		store, rootID, _ := newSwarmFixture(t)
		if err := store.Save(rootID, 1, []llm.Message{{Role: "system"}, {Role: "user", Content: "start"}}, "model", "provider"); err != nil {
			t.Fatal(err)
		}
		if _, err := store.StoreContent(t.Context(), ContentGrant{RootID: rootID, Scope: ContentGrantRoot}, RuntimePayload{Data: []byte("artifact")}); err != nil {
			t.Fatal(err)
		}
		before := lifecycleRows(t, store, rootID)
		if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER lifecycle_fail_grant BEFORE INSERT ON content_grants BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
			t.Fatal(err)
		}
		if id, err := store.Fork(rootID, 1, "fail"); err == nil || id != "" {
			t.Fatalf("fork ignored refused grant copy: %q, %v", id, err)
		}
		if forks, err := store.ForksOf(rootID); err != nil || len(forks) != 0 {
			t.Fatalf("failed fork left a session: %+v, %v", forks, err)
		}
		if after := lifecycleRows(t, store, rootID); !maps.Equal(after, before) {
			t.Fatalf("failed fork changed source: before=%v, after=%v", before, after)
		}
	})
	t.Run("delete complete tree", func(t *testing.T) {
		store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		rootID := lifecyclePopulateRoot(t, store)
		forkID, err := store.Fork(rootID, 1, "surviving fork")
		if err != nil {
			t.Fatal(err)
		}
		before := lifecycleRows(t, store, rootID)
		if _, err := store.db.ExecContext(t.Context(), `CREATE TRIGGER lifecycle_fail_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
			t.Fatal(err)
		}
		if err := store.DeleteSession(t.Context(), rootID); err == nil {
			t.Fatal("delete ignored refused session deletion")
		}
		if after := lifecycleRows(t, store, rootID); !maps.Equal(after, before) {
			t.Fatalf("failed delete changed tree: before=%v, after=%v", before, after)
		}
		if meta, _, err := store.Load(forkID); err != nil || meta.ForkedFrom != rootID {
			t.Fatalf("failed delete changed fork linkage: %+v, %v", meta, err)
		}
	})
}
