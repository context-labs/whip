package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func collectionStore(t *testing.T) (*Store, string) {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	return store, root
}

func TestRootCollectionPageBoundsAndRevision(t *testing.T) {
	store, root := collectionStore(t)
	for index := range 140 {
		if _, err := store.AddSchedule(root, "@every 1h", fmt.Sprintf("prompt %d", index), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	options := CollectionPageOptions{Limit: 17, MaxBytes: 4096}
	seen := map[int]bool{}
	var first RootCollectionPage
	for {
		page, err := store.RootCollectionPage(t.Context(), root, "schedules", options)
		if err != nil {
			t.Fatal(err)
		}
		if first.RootID == "" {
			first = page
		}
		encoded, _ := json.Marshal(page)
		if len(encoded) > options.MaxBytes || len(page.Items) > options.Limit {
			t.Fatalf("unbounded page: %d bytes %d items", len(encoded), len(page.Items))
		}
		for _, item := range page.Items {
			if item.Schedule == nil || seen[item.Schedule.ID] {
				t.Fatalf("duplicate/missing schedule %+v", item)
			}
			seen[item.Schedule.ID] = true
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == nil {
			t.Fatal("missing continuation")
		}
		options.Cursor = page.NextCursor
	}
	if len(seen) != 140 {
		t.Fatalf("read %d schedules", len(seen))
	}
	// This mutation emits no event; collection revision must still change.
	if _, err := store.db.ExecContext(t.Context(), `UPDATE schedules SET prompt='changed' WHERE session_id=? AND id=1`, root); err != nil {
		t.Fatal(err)
	}
	options.Cursor = first.NextCursor
	if _, err := store.RootCollectionPage(t.Context(), root, "schedules", options); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("stale cursor: %v", err)
	}
	other, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RootCollectionPage(t.Context(), other, "schedules", options); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("cross-root cursor: %v", err)
	}
}

func TestRootCollectionLargeBodyReferenceAndGrant(t *testing.T) {
	store, root := collectionStore(t)
	prompt := strings.Repeat("large prompt", 10000)
	if _, err := store.AddSchedule(root, "@every 1h", prompt, time.Now()); err != nil {
		t.Fatal(err)
	}
	opts := CollectionPageOptions{Limit: 10, MaxBytes: 4096}
	page, err := store.RootCollectionPage(t.Context(), root, "schedules", opts)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Body == nil {
		t.Fatalf("missing body reference: %+v", page)
	}
	body := page.Items[0].Body
	again, err := store.RootCollectionPage(t.Context(), root, "schedules", opts)
	if err != nil {
		t.Fatal(err)
	}
	if again.Items[0].Body.ReferenceID != body.ReferenceID {
		t.Fatal("repeated view created another content grant")
	}
	data := []byte{}
	for offset := int64(0); offset < body.Size; {
		chunk, _, err := store.ReadContent(t.Context(), body.ReferenceID, root, "", offset, 64<<10)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, chunk...)
		offset += int64(len(chunk))
	}
	var entry CollectionEntry
	if err := json.Unmarshal(data, &entry); err != nil || entry.Schedule == nil || entry.Schedule.Prompt != prompt {
		t.Fatalf("body mismatch: %v", err)
	}
	if _, _, err := store.ReadContent(t.Context(), body.ReferenceID, "other-root", "", 0, 100); !errors.Is(err, ErrContentAccess) {
		t.Fatalf("grant bypass: %v", err)
	}
}

func TestRootCollectionTypedItems(t *testing.T) {
	store, root := collectionStore(t)
	agent := RuntimeAgent{ID: root, RootID: root, Status: "idle"}
	if _, err := store.CommitRuntime(t.Context(), RuntimeTransition{Agent: &agent}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnqueueInbox(t.Context(), InboxEnqueue{RootID: root, AgentID: root, Kind: "submit", Payload: RuntimePayload{Data: []byte("queued")}}); err != nil {
		t.Fatal(err)
	}
	exec(t, store, `INSERT INTO blackboard(root_id,key,version,author_agent_id,payload_inline,updated_at) VALUES(?,'key',1,?,'value',?)`, root, root, now())
	exec(t, store, `INSERT INTO budgets(root_id,agent_id,kind,limit_value,used_value,reserved_value,updated_at) VALUES(?,?,'tokens',100,0,0,?)`, root, root, now())
	exec(t, store, `INSERT INTO capabilities(id,root_id,agent_id,issuer_agent_id,operations,scopes,generation,status,created_at,updated_at) VALUES('cap',?,?,?,'[]','{}',1,'active',?,?)`, root, root, root, now(), now())
	exec(t, store, `INSERT INTO operations(id,root_id,agent_id,status,payload_inline,created_at,updated_at) VALUES('op',?,?,'waiting',?, ?,?)`, root, root, `{"Request":{"Operation":"shell.run","Arguments":{"command":"echo approved","secret":"DO_NOT_LEAK"}}}`, now(), now())
	exec(t, store, `INSERT INTO permission_requests(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES('permission',?,?,'op','pending',?,?)`, root, root, now(), now())
	for _, collection := range []string{"agents", "inbox", "blackboard", "budgets", "capabilities", "permissions"} {
		t.Run(collection, func(t *testing.T) {
			page, err := store.RootCollectionPage(t.Context(), root, collection, CollectionPageOptions{Limit: 10, MaxBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 1 {
				t.Fatalf("items %+v", page)
			}
			encoded, _ := json.Marshal(page)
			if strings.Contains(string(encoded), "DO_NOT_LEAK") {
				t.Fatal("raw permission arguments leaked")
			}
		})
	}
}
