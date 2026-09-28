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
	if _, err := store.db.ExecContext(t.Context(), `UPDATE sessions SET title=? WHERE id=?`, strings.Repeat("界", 2048), root); err != nil {
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
	if _, err := store.db.ExecContext(t.Context(), `UPDATE sessions SET title='changed' WHERE id=?`, root); err != nil {
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
	if _, err := store.db.ExecContext(t.Context(), `UPDATE sessions SET cwd=?,title='First workspace' WHERE id=?`, firstPath, root); err != nil {
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

func TestSessionCatalogArchiveFilteringAndCursorScope(t *testing.T) {
	store, root := collectionStore(t)
	for range 4 {
		if _, err := store.Create(SessionKindAgent, "/project", "model", "provider"); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetTitle(root, "needle"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetArchived(t.Context(), root, true); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, status, search string
		count                int
	}{
		{"default active", "", "", 4},
		{"active", "active", "", 4},
		{"archived", "archived", "", 1},
		{"all", "all", "", 5},
		{"active search excludes archived", "active", "needle", 0},
		{"archive search", "archived", "NEEDLE", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			opts := CatalogPageOptions{Limit: 1, MaxBytes: 4096, Status: test.status, Search: test.search}
			count := 0
			for {
				page, err := store.SessionCatalog(t.Context(), opts)
				if err != nil {
					t.Fatal(err)
				}
				count += len(page.Items)
				for _, item := range page.Items {
					if item.Archived != (item.ID == root) {
						t.Fatalf("wrong archive metadata: %+v", item)
					}
				}
				if !page.HasMore {
					break
				}
				opts.Cursor = page.NextCursor
			}
			if count != test.count {
				t.Fatalf("got %d rows, want %d", count, test.count)
			}
		})
	}
	page, err := store.SessionCatalog(t.Context(), CatalogPageOptions{Limit: 1, MaxBytes: 4096, Status: "all"})
	if err != nil || page.NextCursor == nil {
		t.Fatalf("first all page %+v %v", page, err)
	}
	if _, err := store.SessionCatalog(t.Context(), CatalogPageOptions{Limit: 1, MaxBytes: 4096, Status: "active", Cursor: page.NextCursor}); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("cursor reused with different status: %v", err)
	}
	if err := store.SetArchived(t.Context(), root, false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SessionCatalog(t.Context(), CatalogPageOptions{Limit: 1, MaxBytes: 4096, Status: "all", Cursor: page.NextCursor}); !errors.Is(err, ErrCollectionChanged) {
		t.Fatalf("restore did not invalidate catalog cursor: %v", err)
	}
	if _, err := store.SessionCatalog(t.Context(), CatalogPageOptions{Limit: 1, MaxBytes: 4096, Status: "deleted"}); err == nil {
		t.Fatal("invalid status accepted")
	}
}
