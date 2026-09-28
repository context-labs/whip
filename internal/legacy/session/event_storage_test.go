package session

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

func TestRootSnapshotsRejectUnavailableCollections(t *testing.T) {
	for _, table := range []string{"events", "messages", "agents", "agent_messages", "inbox", "content_references", "blackboard", "budgets", "model_calls", "capabilities", "schedules", "permission_requests", "operations", "turns"} {
		t.Run(table, func(t *testing.T) {
			store, root, _ := newSwarmFixture(t)
			if _, err := store.SnapshotRoot(t.Context(), root); err != nil {
				t.Fatal(err)
			}
			exec(t, store, "PRAGMA foreign_keys=OFF")
			exec(t, store, "DROP TABLE "+table)
			for _, bounded := range []bool{false, true} {
				var snapshot RootSnapshot
				var err error
				if bounded {
					snapshot, err = store.SnapshotRootView(t.Context(), root, SnapshotViewOptions{RecentMessages: 4, CollectionLimit: 4, MaxBytes: 4096})
				} else {
					snapshot, err = store.SnapshotRoot(t.Context(), root)
				}
				if err == nil || !strings.Contains(err.Error(), "no such table") {
					t.Fatalf("bounded=%t silently omitted unavailable %s: %v", bounded, table, err)
				}
				if !reflect.DeepEqual(snapshot, RootSnapshot{}) {
					t.Fatalf("bounded=%t returned a partial snapshot: %+v", bounded, snapshot)
				}
			}
		})
	}
}

func TestRootSnapshotsRejectCorruptDurableMetadata(t *testing.T) {
	for _, test := range []struct{ name, sql string }{
		{"capability operations", `UPDATE capabilities SET operations='invalid json'`},
		{"capability scopes", `UPDATE capabilities SET scopes='invalid json'`},
		{"capability expiry", `UPDATE capabilities SET scopes='{"expires_at":"invalid time"}'`},
		{"capability creation", `UPDATE capabilities SET created_at='invalid time'`},
		{"capability update", `UPDATE capabilities SET updated_at='invalid time'`},
		{"capability generation", `UPDATE capabilities SET generation='invalid integer'`},
		{"budget amount", `UPDATE budgets SET used_value='invalid integer'`},
		{"negative budget", `UPDATE budgets SET used_value=-1`},
		{"schedule anchor", `UPDATE schedules SET anchor='invalid time'`},
		{"schedule fire", `UPDATE schedules SET last_fire='invalid time'`},
		{"inbox sequence", `UPDATE inbox SET seq='invalid integer'`},
		{"blackboard version", `UPDATE blackboard SET version='invalid integer'`},
		{"permission admission", `UPDATE operations SET payload_inline='invalid json' WHERE id='pending-operation'`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, root, agent := newSwarmFixture(t)
			if _, err := store.AddSchedule(root, "@every 1h", "check", time.Now()); err != nil {
				t.Fatal(err)
			}
			if _, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: agent, Kind: "submit", Payload: RuntimePayload{Data: []byte("work")}}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetBlackboard(t.Context(), root, agent, "plan", RuntimePayload{Data: []byte("ready")}); err != nil {
				t.Fatal(err)
			}
			exec(t, store, `INSERT INTO operations(id,root_id,agent_id,status,payload_inline,created_at,updated_at) VALUES('pending-operation',?,?,'waiting','{"Request":{"Operation":"bash","Arguments":{"command":"echo ok"}}}',?,?)`, root, agent, now(), now())
			exec(t, store, `INSERT INTO permission_requests(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES('pending',?,?,'pending-operation','pending',?,?)`, root, agent, now(), now())
			if _, err := store.SnapshotRoot(t.Context(), root); err != nil {
				t.Fatal(err)
			}
			exec(t, store, test.sql)
			for _, bounded := range []bool{false, true} {
				var snapshot RootSnapshot
				var err error
				if bounded {
					snapshot, err = store.SnapshotRootView(t.Context(), root, SnapshotViewOptions{RecentMessages: 16, CollectionLimit: 128, MaxBytes: 65536})
				} else {
					snapshot, err = store.SnapshotRoot(t.Context(), root)
				}
				if err == nil {
					t.Fatalf("bounded=%t accepted corrupt metadata", bounded)
				}
				if !reflect.DeepEqual(snapshot, RootSnapshot{}) {
					t.Fatalf("bounded=%t returned partial metadata: %+v", bounded, snapshot)
				}
			}
		})
	}
}

func TestRootAgentViewsReflectRunningBlockedAndTerminalAgents(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	for _, child := range []string{"running", "blocked", "stopped", "deleted"} {
		admitTestChild(t, store, root, agent, child)
	}
	exec(t, store, `UPDATE agents SET status='running' WHERE id='running'`)
	exec(t, store, `UPDATE agents SET status='stopped' WHERE id='stopped'`)
	exec(t, store, `UPDATE agents SET status='deleted' WHERE id='deleted'`)
	exec(t, store, `INSERT INTO operations(id,root_id,agent_id,status,payload_inline,created_at,updated_at) VALUES('pending-operation',?,'blocked','waiting','{}',?,?)`, root, now(), now())
	exec(t, store, `INSERT INTO permission_requests(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES('pending',?,'blocked','pending-operation','pending',?,?)`, root, now(), now())
	queued, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: agent, Kind: "submit", Payload: RuntimePayload{Data: []byte("work")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StartRootTurn(t.Context(), root, agent, queued.InboxSeq); err != nil {
		t.Fatal(err)
	}
	views, err := store.RootAgentViews(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]RuntimeAgent{}
	for _, view := range views {
		got[view.ID] = view
	}
	if got[agent].LifecyclePhase != "running" || !slices.Contains(got[agent].AllowedControls, "cancel") {
		t.Fatalf("root lost active input: %+v", got[agent])
	}
	if got["running"].LifecyclePhase != "running" || got["blocked"].LifecyclePhase != "blocked" || got["blocked"].BlockingReason != "permission" {
		t.Fatalf("active child state %+v", got)
	}
	if got["stopped"].LifecyclePhase != "terminal" || got["stopped"].TerminalCause != "stopped" || !reflect.DeepEqual(got["stopped"].AllowedControls, []string{"agent.delete"}) {
		t.Fatalf("stopped child %+v", got["stopped"])
	}
	if got["deleted"].LifecyclePhase != "terminal" || len(got["deleted"].AllowedControls) != 0 {
		t.Fatalf("deleted child %+v", got["deleted"])
	}
}

func TestRootAgentViewsNeverHideStorageErrors(t *testing.T) {
	for _, table := range []string{"agents", "inbox", "permission_requests"} {
		t.Run(table, func(t *testing.T) {
			store, root, _ := newSwarmFixture(t)
			exec(t, store, "PRAGMA foreign_keys=OFF")
			exec(t, store, "DROP TABLE "+table)
			if _, err := store.RootAgentViews(t.Context(), root); err == nil {
				t.Fatal("missing table presented as idle tree")
			}
		})
	}
	store, root, _ := newSwarmFixture(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RootAgentViews(context.Background(), root); err == nil {
		t.Fatal("closed store presented as empty tree")
	}
}

func TestBoundedSnapshotReportsInvalidMessagesAndPresentation(t *testing.T) {
	for _, test := range []struct {
		name           string
		message, event string
	}{
		{name: "message JSON", message: `{"role":`},
		{name: "message role", message: `{"content":"orphan"}`},
		{name: "event JSON", event: `{"agent_id":`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, root := collectionStore(t)
			if test.message != "" {
				exec(t, store, `INSERT INTO messages(session_id,seq,role,content) VALUES(?,1,'user',?)`, root, test.message)
			}
			if test.event != "" {
				if _, err := store.AppendRootEvent(t.Context(), root, "stream.text", RuntimePayload{Data: []byte(test.event)}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.SnapshotRootView(t.Context(), root, SnapshotViewOptions{RecentMessages: 2, CollectionLimit: 2, MaxBytes: 4096}); err == nil {
				t.Fatal("invalid presentation accepted")
			}
		})
	}
}

func TestBoundedSnapshotDropsOversizedMetadataAndPreservesPaging(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	for i := range 6 {
		id := strings.Repeat("child", 30) + string(rune('a'+i))
		admitTestChild(t, store, root, agent, id)
	}
	if err := store.Save(root, 0, []llm.Message{{Role: "user", Content: strings.Repeat("text", 300)}, {Role: "assistant", Content: "latest"}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		if _, err := store.SetBlackboard(t.Context(), root, agent, string(rune('a'+i)), RuntimePayload{Data: []byte(strings.Repeat("value", 400))}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: agent, Kind: "submit", Payload: RuntimePayload{Data: []byte(strings.Repeat("input", 300))}}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AddSchedule(root, "@every 1h", strings.Repeat("scheduled", 300), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		payload, _ := json.Marshal(map[string]string{"text": strings.Repeat("stream", 100)})
		if _, err := store.AppendRootEvent(t.Context(), root, "stream.text", RuntimePayload{Data: payload}); err != nil {
			t.Fatal(err)
		}
	}
	opts := SnapshotViewOptions{RecentMessages: 2, CollectionLimit: 128, MaxBytes: 4096}
	view, err := store.SnapshotRootView(t.Context(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(view)
	if err != nil || len(encoded) > opts.MaxBytes {
		t.Fatalf("unbounded snapshot: %d %v", len(encoded), err)
	}
	for _, collection := range []string{"messages", "blackboard", "inbox", "schedules", "capabilities", "budgets"} {
		if !view.Omitted[collection] {
			t.Errorf("missing omission flag for %s", collection)
		}
	}
	full, err := store.SnapshotRoot(t.Context(), root)
	if err != nil || len(full.Blackboard) != 4 || len(full.Inbox) != 4 || len(full.Schedules) != 4 || len(full.Messages) != 2 {
		t.Fatalf("bounded read changed stored data: %+v %v", full, err)
	}
	if full.Cursor != view.Cursor {
		t.Fatalf("cursor drift full=%d bounded=%d", full.Cursor, view.Cursor)
	}
}
