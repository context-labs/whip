package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCollectionPagesRejectCorruptRowsInsteadOfSkippingThem(t *testing.T) {
	for _, test := range []struct{ name, collection, sql string }{
		{"invalid budget", "budgets", `UPDATE budgets SET used_value=-1`},
		{"malformed budget", "budgets", `UPDATE budgets SET reserved_value='not a number'`},
		{"malformed inbox", "inbox", `UPDATE inbox SET seq='not a number'`},
		{"oversized inline input", "inbox", `UPDATE inbox SET payload_inline=zeroblob(9000)`},
		{"schedule anchor", "schedules", `UPDATE schedules SET anchor='not a time'`},
		{"schedule last fire", "schedules", `UPDATE schedules SET last_fire='not a time'`},
		{"capability operations", "capabilities", `UPDATE capabilities SET operations='broken'`},
		{"capability expiry", "capabilities", `UPDATE capabilities SET scopes='{"expires_at":"broken"}'`},
		{"blackboard version", "blackboard", `UPDATE blackboard SET version='broken'`},
		{"permission payload", "permissions", `UPDATE operations SET payload_inline='broken' WHERE id='pending-operation'`},
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
			exec(t, store, `INSERT INTO operations(id,root_id,agent_id,status,payload_inline,created_at,updated_at) VALUES('pending-operation',?,?,'waiting','{}',?,?)`, root, agent, now(), now())
			exec(t, store, `INSERT INTO permission_requests(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES('pending',?,?,'pending-operation','pending',?,?)`, root, agent, now(), now())
			opts := CollectionPageOptions{Limit: 128, MaxBytes: 65536}
			if _, err := store.RootCollectionPage(t.Context(), root, test.collection, opts); err != nil {
				t.Fatal(err)
			}
			exec(t, store, test.sql)
			if _, err := store.RootCollectionPage(t.Context(), root, test.collection, opts); err == nil {
				t.Fatal("corrupt collection returned as complete")
			}
		})
	}
}

func TestCollectionPageBoundsAllowProgressWithoutLosingRows(t *testing.T) {
	for _, size := range []struct {
		name  string
		bytes int
	}{{"many medium entries", 1200}, {"large entry after small one", 6000}} {
		t.Run(size.name, func(t *testing.T) {
			store, root := collectionStore(t)
			if _, err := store.AddSchedule(root, "@every 1h", "small", time.Now()); err != nil {
				t.Fatal(err)
			}
			for range 5 {
				if _, err := store.AddSchedule(root, "@every 1h", strings.Repeat("x", size.bytes), time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			opts := CollectionPageOptions{Limit: 128, MaxBytes: 4096}
			seen := map[int]bool{}
			pages := 0
			for {
				page, err := store.RootCollectionPage(t.Context(), root, "schedules", opts)
				if err != nil {
					t.Fatal(err)
				}
				data, err := json.Marshal(page)
				if err != nil || len(data) > opts.MaxBytes || len(page.Items) == 0 {
					t.Fatalf("non-progressing page %+v bytes=%d %v", page, len(data), err)
				}
				for _, entry := range page.Items {
					if entry.Body != nil {
						raw, err := store.ResolveRuntimeValue(t.Context(), root, *entry.Body)
						if err != nil {
							t.Fatal(err)
						}
						if err := json.Unmarshal(raw, &entry); err != nil {
							t.Fatal(err)
						}
					}
					if entry.Schedule == nil || seen[entry.Schedule.ID] {
						t.Fatalf("missing or duplicate schedule: %+v", entry)
					}
					seen[entry.Schedule.ID] = true
				}
				pages++
				if !page.HasMore {
					break
				}
				if page.NextCursor == nil || pages > 6 {
					t.Fatalf("invalid continuation %+v", page)
				}
				opts.Cursor = page.NextCursor
			}
			if len(seen) != 6 || pages < 2 {
				t.Fatalf("rows=%d pages=%d", len(seen), pages)
			}
		})
	}
}

func TestCollectionBodyGrantFailureLeavesNoUsableHandle(t *testing.T) {
	for _, table := range []string{"content_objects", "content_references", "content_grants"} {
		t.Run(table, func(t *testing.T) {
			store, root := collectionStore(t)
			if _, err := store.AddSchedule(root, "@every 1h", strings.Repeat("data", 2000), time.Now()); err != nil {
				t.Fatal(err)
			}
			rejectSessionWrite(t, store, "INSERT", table, "")
			opts := CollectionPageOptions{Limit: 10, MaxBytes: 4096}
			_, err := store.RootCollectionPage(t.Context(), root, "schedules", opts)
			requireSessionWriteFailure(t, err)
			var handles int
			if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM content_grants WHERE root_id=?`, root).Scan(&handles); err != nil || handles != 0 {
				t.Fatalf("failed read leaked handles=%d %v", handles, err)
			}
			exec(t, store, "DROP TRIGGER reject_session_write")
			page, err := store.RootCollectionPage(t.Context(), root, "schedules", opts)
			if err != nil || len(page.Items) != 1 || page.Items[0].Body == nil {
				t.Fatalf("retry page %+v %v", page, err)
			}
		})
	}
}

func TestCollectionAndCatalogRejectInvalidContinuationAndClosedStore(t *testing.T) {
	store, root := collectionStore(t)
	for _, test := range []struct {
		name, collection string
		opts             CollectionPageOptions
	}{
		{"unknown collection", "unknown", CollectionPageOptions{Limit: 10, MaxBytes: 4096}},
		{"zero limit", "schedules", CollectionPageOptions{MaxBytes: 4096}},
		{"byte budget", "schedules", CollectionPageOptions{Limit: 10, MaxBytes: 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.RootCollectionPage(t.Context(), root, test.collection, test.opts); err == nil {
				t.Fatal("invalid query accepted")
			}
		})
	}
	page, err := store.RootCollectionPage(t.Context(), root, "schedules", CollectionPageOptions{Limit: 10, MaxBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RootCollectionPage(t.Context(), root, "schedules", CollectionPageOptions{Limit: 10, MaxBytes: 4096, Cursor: &CollectionCursor{RootID: root, Collection: "schedules", Revision: page.Revision, Offset: 1}}); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("past-end cursor=%v", err)
	}
	if _, err := store.RootCollectionPage(t.Context(), "missing", "schedules", CollectionPageOptions{Limit: 10, MaxBytes: 4096}); err == nil {
		t.Fatal("missing root accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func(context.Context) error{
		"collection": func(ctx context.Context) error {
			_, err := store.RootCollectionPage(ctx, root, "schedules", CollectionPageOptions{Limit: 10, MaxBytes: 4096})
			return err
		},
		"catalog": func(ctx context.Context) error {
			_, err := store.SessionCatalog(ctx, CatalogPageOptions{Limit: 10, MaxBytes: 4096})
			return err
		},
		"accounting": func(ctx context.Context) error { _, err := store.ModelAccounting(ctx, root, "", true); return err },
	} {
		t.Run(name+" closed", func(t *testing.T) {
			if err := call(t.Context()); err == nil || !strings.Contains(err.Error(), "closed") {
				t.Fatalf("closed store result=%v", err)
			}
		})
	}
}
