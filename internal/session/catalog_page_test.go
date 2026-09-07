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

func TestSessionCatalogSearchAndWorkspaceIdentity(t *testing.T) {
	store, root := collectionStore(t)
	firstPath := "/" + strings.Repeat("same-directory-prefix/", 10) + "one"
	secondPath := "/" + strings.Repeat("same-directory-prefix/", 10) + "two"
	if _, err := store.db.Exec(`UPDATE sessions SET cwd=?,title='First workspace' WHERE id=?`, firstPath, root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create(SessionKindAgent, secondPath, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	page, err := store.SessionCatalog(t.Context(), CatalogPageOptions{Limit: 2, MaxBytes: 4096})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("catalog %+v %v", page, err)
	}
	if page.Items[0].CWD != page.Items[1].CWD || page.Items[0].WorkspaceID == page.Items[1].WorkspaceID || page.Items[0].WorkspaceID == "" {
		t.Fatalf("workspace identity uses truncated path: %+v", page.Items)
	}
	page, err = store.SessionCatalog(t.Context(), CatalogPageOptions{Limit: 2, MaxBytes: 4096, Search: "FIRST"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != root {
		t.Fatalf("title search %+v %v", page, err)
	}
	page, err = store.SessionCatalog(t.Context(), CatalogPageOptions{Limit: 2, MaxBytes: 4096, Search: "two"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID == root {
		t.Fatalf("untruncated path search %+v %v", page, err)
	}
}
