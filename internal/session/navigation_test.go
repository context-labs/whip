package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSessionSummariesObserveDescendantsWithoutCatalogPaging(t *testing.T) {
	store, rootID, _ := newMailboxFixture(t)
	for range 129 {
		if _, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider"); err != nil {
			t.Fatal(err)
		}
	}
	exec(t, store, `UPDATE sessions SET updated_at='2000-01-01T00:00:00Z' WHERE id=?`, rootID)
	catalog, err := store.SessionCatalog(t.Context(), CatalogPageOptions{Limit: 128, MaxBytes: 512 << 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range catalog.Items {
		if item.ID == rootID {
			t.Fatal("fixture root must be outside the first catalog page")
		}
	}
	for range 3 {
		if _, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: rootID, AgentID: "child", Kind: "submit", Payload: RuntimePayload{Data: []byte("private prompt")}}); err != nil {
			t.Fatal(err)
		}
	}
	started, err := store.StartAgentTurn(t.Context(), rootID, "child", "working-child")
	if err != nil {
		t.Fatal(err)
	}
	exec(t, store, `INSERT INTO operations(id,root_id,agent_id,status,created_at,updated_at) VALUES('summary-op',?,'child','waiting',?,?)`, rootID, now(), now())
	exec(t, store, `INSERT INTO permission_requests(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES('summary-permission',?,'child','summary-op','pending',?,?)`, rootID, now(), now())
	ids := []string{"missing-first", rootID, "child", catalog.Items[0].ID}
	items, err := store.SessionSummaries(t.Context(), ids)
	if err != nil || len(items) != len(ids) {
		t.Fatalf("summaries %+v: %v", items, err)
	}
	for index, item := range items {
		if item.RootID != ids[index] || item.Missing != (index == 0 || index == 2) {
			t.Fatalf("request identity/order or root association lost: %+v", items)
		}
	}
	if active := items[1]; active.RunningAgents != 1 || active.QueuedAgents != 1 || active.PendingPermissions != 1 {
		t.Fatalf("child activity with quiet root: %+v", active)
	}
	if idle := items[3]; idle.RunningAgents != 0 || idle.QueuedAgents != 0 || idle.PendingPermissions != 0 {
		t.Fatalf("activity leaked to another root: %+v", idle)
	}
	if err := store.FinishAgentTurn(t.Context(), rootID, "child", AgentTurnCommit{TurnID: started.TurnID, Status: "succeeded", AcknowledgedInbox: []int64{started.Items[0].Seq}}); err != nil {
		t.Fatal(err)
	}
	exec(t, store, `UPDATE permission_requests SET status='denied' WHERE id='summary-permission'`)
	items, err = store.SessionSummaries(t.Context(), []string{rootID})
	if err != nil || items[0].RunningAgents != 0 || items[0].QueuedAgents != 1 || items[0].PendingPermissions != 0 {
		t.Fatalf("settled turn/permission with queued follow-ups: %+v %v", items, err)
	}
}

func TestSessionSummariesBoundMetadataAndPreserveWorkspaceIdentity(t *testing.T) {
	store, rootID, _ := newMailboxFixture(t)
	for _, test := range []struct {
		name, path string
		workspace  bool
	}{
		{"long-path", "/" + strings.Repeat("界/", 200), true},
		{"unavailable-full-path", "/" + strings.Repeat("界", 4096), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			exec(t, store, `UPDATE sessions SET title=?,cwd=? WHERE id=?`, strings.Repeat("界", 10000), test.path, rootID)
			items, err := store.SessionSummaries(t.Context(), []string{rootID})
			if err != nil {
				t.Fatal(err)
			}
			item := items[0]
			if !item.Truncated || utf8.RuneCountInString(item.Title) != 128 || utf8.RuneCountInString(item.CWD) != 128 || !utf8.ValidString(item.CWD) {
				t.Fatalf("unbounded or invalid metadata: %+v", item)
			}
			want := ""
			if test.workspace {
				want = fmt.Sprintf("%x", sha256.Sum256([]byte(test.path)))
			}
			if item.WorkspaceID != want {
				t.Fatalf("workspace identity %q, want %q", item.WorkspaceID, want)
			}
		})
	}
}

func TestSessionSummariesRejectInvalidBoundsAndNeverInventMissingOnFailure(t *testing.T) {
	store, rootID, _ := newMailboxFixture(t)
	for _, test := range []struct {
		name string
		ids  []string
	}{
		{"too-many", make([]string, MaxSessionSummaries+1)},
		{"empty-id", []string{""}},
		{"duplicate", []string{rootID, rootID}},
		{"long-id", []string{strings.Repeat("a", 257)}},
		{"long-utf8-id", []string{strings.Repeat("界", 128)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if items, err := store.SessionSummaries(t.Context(), test.ids); err == nil || items != nil {
				t.Fatalf("invalid IDs returned summaries: %+v %v", items, err)
			}
		})
	}
	if empty, err := store.SessionSummaries(t.Context(), []string{}); err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty working set: %+v %v", empty, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if items, err := store.SessionSummaries(ctx, []string{rootID, "missing"}); !errors.Is(err, context.Canceled) || items != nil {
		t.Fatalf("cancelled query fabricated presence/absence: %+v %v", items, err)
	}
	// Infrastructure failures must not turn requested identities into missing
	// items, including an ID that really is absent alongside the existing root.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if items, err := store.SessionSummaries(t.Context(), []string{"missing", rootID}); err == nil || items != nil {
		t.Fatalf("database failure fabricated presence/absence: %+v %v", items, err)
	}
}
