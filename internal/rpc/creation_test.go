package rpc_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestCreationRPCExactReceiptCatalogAndDeletion(t *testing.T) {
	r, c := fixture(t)
	before := call[protocol.TreeCatalog](t, c, "trees.catalog", protocol.EmptyParams{})
	request := protocol.CreateTreeParams{CreationID: "MiXeD:Root", Engine: "quickjs", Definition: c.Builtins()[0], WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{Model: &protocol.ModelSelection{Provider: "scripted", Name: "scripted"}}}
	original := call[protocol.CreateTreeResult](t, c, "trees.create", request)
	if original.Creation.ID != request.CreationID || original.Tree == nil || original.Root == nil || original.Deleted {
		t.Fatal(original)
	}
	retry := call[protocol.CreateTreeResult](t, c, "trees.create", request)
	if !reflect.DeepEqual(retry, original) {
		t.Fatal("retry changed original result")
	}
	head := call[protocol.TreeCatalog](t, c, "trees.catalog", protocol.EmptyParams{})
	if head.Revision != before.Revision+1 {
		t.Fatal("retry duplicated catalog invalidation", head)
	}
	request.Engine = "starlark"
	requireHistoryError(t, c, "trees.create", request, "CONFLICT")
	request.Engine = "quickjs"
	old := call[protocol.ListTreesResult](t, c, "trees.list", protocol.ListTreesParams{Limit: 1, ExpectedRevision: &head.Revision})
	if old.Revision != head.Revision || len(old.Items) != 1 {
		t.Fatal(old)
	}
	updated := call[protocol.Tree](t, c, "trees.update", protocol.UpdateTreeParams{TreeID: original.Tree.ID, ExpectedRevision: original.Tree.Revision, Metadata: protocol.TreeMetadata{Title: new("Off-page title"), Pinned: true}})
	requireHistoryError(t, c, "trees.list", protocol.ListTreesParams{Limit: 1, ExpectedRevision: &old.Revision}, "CONFLICT")
	current := call[protocol.CreateTreeResult](t, c, "trees.creation", protocol.TreeCreationParams{CreationID: request.CreationID})
	if current.Creation != original.Creation || !reflect.DeepEqual(*current.Tree, updated) {
		t.Fatal("receipt read returned stale tree", current)
	}
	// Seed only the global counter; subsequent changes traverse production code.
	db, err := sql.Open("sqlite", filepath.Join(filepath.Dir(r.SocketPath()), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(t.Context(), "DROP TRIGGER tree_catalog_revision"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE tree_catalog SET revision=9007199254740993"); err != nil {
		t.Fatal(err)
	}
	exact := call[protocol.TreeCatalog](t, c, "trees.catalog", protocol.EmptyParams{})
	if exact.Revision != 9007199254740993 {
		t.Fatal("catalog revision rounded")
	}
	page := call[protocol.ListTreesResult](t, c, "trees.list", protocol.ListTreesParams{Limit: 1, ExpectedRevision: &exact.Revision})
	if page.Revision != exact.Revision {
		t.Fatal("page counter rounded")
	}
	call[protocol.DeleteResult](t, c, "sessions.delete", protocol.SessionParams{SessionID: original.Root.ID})
	deleted := call[protocol.CreateTreeResult](t, c, "trees.create", request)
	if !deleted.Deleted || deleted.Root != nil || deleted.Tree != nil || deleted.Creation != original.Creation {
		t.Fatal("deleted root recreated", deleted)
	}
	final := call[protocol.TreeCatalog](t, c, "trees.catalog", protocol.EmptyParams{})
	if final.Revision != exact.Revision+1 {
		t.Fatal("deleted retry invalidated catalog", final)
	}
	requireHistoryError(t, c, "trees.creation", protocol.TreeCreationParams{CreationID: "missing"}, "NOT_FOUND")
}
