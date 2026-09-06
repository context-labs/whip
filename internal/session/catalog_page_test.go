package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSessionCatalogBoundsAndInvalidation(t *testing.T) {
	store, root := collectionStore(t)
	for range 18 {
		if _, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec(`UPDATE sessions SET title=? WHERE id=?`, strings.Repeat("界", 2048), root); err != nil {
		t.Fatal(err)
	}
	opts := CatalogPageOptions{Limit: 5, MaxBytes: 4096}
	seen := map[string]bool{}
	var first SessionCatalogPage
	for {
		page, err := store.SessionCatalog(t.Context(), opts)
		if err != nil {
			t.Fatal(err)
		}
		if opts.Cursor == nil {
			first = page
		}
		raw, _ := json.Marshal(page)
		if len(raw) > 4096 || len(page.Items) > 5 {
			t.Fatal("unbounded page")
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate")
			}
			seen[item.ID] = true
			if item.ID == root && (!item.Truncated || len([]rune(item.Title)) != 128) {
				t.Fatal("missing truncation marker")
			}
		}
		if !page.HasMore {
			break
		}
		opts.Cursor = page.NextCursor
	}
	if len(seen) != 19 {
		t.Fatalf("got %d sessions", len(seen))
	}
	if _, err := store.db.Exec(`UPDATE sessions SET title='changed' WHERE id=?`, root); err != nil {
		t.Fatal(err)
	}
	_, err := store.SessionCatalog(t.Context(), CatalogPageOptions{Cursor: first.NextCursor, Limit: 5, MaxBytes: 4096})
	if !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("stale cursor: %v", err)
	}
	revision, err := store.SessionCatalogRevision(t.Context())
	if err != nil || revision.Revision <= first.Revision {
		t.Fatalf("revision %v %v", revision, err)
	}
}
