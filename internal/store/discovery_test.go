package store

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestTreeDiscoveryPagesFiltersAndDeletedCursor(t *testing.T) {
	s := fresh(t)
	roots := map[session.TreeID]session.SessionID{}
	var ids []session.TreeID
	for range 3 {
		tree, root := create(t, s, nil)
		ids = append(ids, tree.ID)
		roots[tree.ID] = root.ID
	}
	slices.Sort(ids)
	execTest(t, s, "UPDATE session_trees SET revision=? WHERE id=?", int64(9007199254740993), ids[0])
	first, err := s.Trees(t.Context(), TreeList{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Next == nil || *first.Next != ids[0] || first.Items[0].RootID != roots[ids[0]] || first.Items[0].Revision != 9007199254740993 {
		t.Fatal(first, err)
	}
	if _, err := s.SetLifecycle(t.Context(), roots[ids[0]], session.Stopped); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), roots[ids[0]]); err != nil {
		t.Fatal(err)
	}
	second, err := s.Trees(t.Context(), TreeList{After: *first.Next, Limit: 1})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != ids[1] || second.Next == nil {
		t.Fatal(second, err)
	}
	last, err := s.Trees(t.Context(), TreeList{After: *second.Next, Limit: 1})
	if err != nil || len(last.Items) != 1 || last.Items[0].ID != ids[2] || last.Next != nil {
		t.Fatal(last, err)
	}
	if _, err := s.UpdateTree(t.Context(), ids[1], second.Items[0].Revision, session.TreeMetadata{Title: new("Pinned"), Pinned: true, Archived: true}); err != nil {
		t.Fatal(err)
	}
	filtered, err := s.Trees(t.Context(), TreeList{Archived: new(false), Pinned: new(false), Limit: 100})
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].ID != ids[2] {
		t.Fatal(filtered, err)
	}
	filtered, err = s.Trees(t.Context(), TreeList{Archived: new(true), Pinned: new(true), Limit: 100})
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].Metadata.Title == nil || *filtered.Items[0].Metadata.Title != "Pinned" {
		t.Fatal(filtered, err)
	}
	for _, request := range []TreeList{{Limit: 0}, {Limit: 101}, {Limit: 1, After: "invalid cursor"}} {
		if _, err := s.Trees(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(request, err)
		}
	}
}

func TestDefinitionDiscoveryRetainsEveryRevisionWithoutBodies(t *testing.T) {
	s := fresh(t)
	for _, name := range []string{"First revision", "Second revision"} {
		if _, err := s.RegisterDefinition(t.Context(), session.DefinitionDocument{ID: "custom", Name: name, Defaults: session.ConfigPatch{Instructions: &session.Instructions{Text: strings.Repeat("private body ", 20000)}}}); err != nil {
			t.Fatal(err)
		}
	}
	full, err := s.DefinitionSummaries(t.Context(), nil, 100)
	if err != nil || len(full.Items) != len(session.Builtins())+2 || full.Next != nil {
		t.Fatal(full, err)
	}
	var collected []session.DefinitionSummary
	var after *session.DefinitionRef
	for {
		page, err := s.DefinitionSummaries(t.Context(), after, 1)
		if err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
		collected = append(collected, page.Items...)
		if page.Next == nil {
			break
		}
		after = page.Next
	}
	var custom []session.DefinitionSummary
	for _, item := range collected {
		if item.Ref.ID == "custom" {
			custom = append(custom, item)
		}
	}
	if !reflect.DeepEqual(collected, full.Items) || len(custom) != 2 || custom[0].Ref.Revision >= custom[1].Ref.Revision {
		t.Fatal(collected)
	}
	for _, limit := range []int{0, 101} {
		if _, err := s.DefinitionSummaries(t.Context(), nil, limit); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := s.DefinitionSummaries(t.Context(), &session.DefinitionRef{ID: "custom", Revision: "latest"}, 1); !errors.Is(err, session.ErrInvalid) {
		t.Fatal(err)
	}
}

func TestTreeCatalogLiteralSearchAndWorkspaceRevision(t *testing.T) {
	s := fresh(t)
	tree, root := create(t, s, nil)
	_, other := create(t, s, nil)
	if _, err := s.UpdateTree(t.Context(), tree.ID, tree.Revision, session.TreeMetadata{Title: new("Release 100%_Ready")}); err != nil {
		t.Fatal(err)
	}
	for _, search := range []string{"100%_ready", string(root.ID), string(tree.ID), root.WorkingDirectory} {
		page, err := s.Trees(t.Context(), TreeList{Search: search, Limit: 1})
		if err != nil || len(page.Items) != 1 || page.Items[0].RootID != root.ID || page.Items[0].WorkingDirectory != root.WorkingDirectory || page.Next != nil {
			t.Fatal(search, page, err)
		}
	}
	page, err := s.Trees(t.Context(), TreeList{Search: "100XXready", Limit: 1})
	if err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
	for _, search := range []string{strings.Repeat("x", 257), "bad\x00search", string([]byte{255})} {
		if _, err := s.Trees(t.Context(), TreeList{Search: search, Limit: 1}); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid search", err)
		}
	}
	before := catalogTest(t, s)
	request := session.WorkspaceSetRequest{ID: "catalog-directory", SessionID: root.ID, ExpectedRevision: root.ConfigRevision, Path: "/new-project"}
	if _, err := setDirectory(t, s, request, "/new-project"); err != nil {
		t.Fatal(err)
	}
	if catalogTest(t, s) != before+1 {
		t.Fatal("root path failed to invalidate catalog")
	}
	if _, err := s.Trees(t.Context(), TreeList{Search: "new-project", Limit: 1, ExpectedRevision: &before}); !errors.Is(err, ErrConflict) {
		t.Fatal("search mixed catalog generations", err)
	}
	page, err = s.Trees(t.Context(), TreeList{Search: "new-project", Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].RootID != root.ID || page.Items[0].WorkingDirectory != "/new-project" {
		t.Fatal(page, err)
	}
	if _, err := setDirectory(t, s, request, "/ignored-retry"); err != nil || catalogTest(t, s) != before+1 {
		t.Fatal("retry changed catalog", err)
	}
	// Catalog failure must roll back both workspace configuration and receipt.
	execTest(t, s, "CREATE TRIGGER catalog_fail BEFORE UPDATE ON tree_catalog BEGIN SELECT RAISE(ABORT,'injected'); END")
	failed := session.WorkspaceSetRequest{ID: "catalog-fail", SessionID: other.ID, ExpectedRevision: other.ConfigRevision, Path: "/rollback"}
	if _, err := setDirectory(t, s, failed, "/rollback"); err == nil {
		t.Fatal("catalog failure ignored")
	}
	unchanged, err := s.Session(t.Context(), other.ID)
	if err != nil || unchanged.WorkingDirectory != other.WorkingDirectory || unchanged.ConfigRevision != other.ConfigRevision {
		t.Fatal(unchanged, err)
	}
	if _, err := s.WorkspaceSetRetry(t.Context(), failed); !errors.Is(err, ErrNotFound) {
		t.Fatal("failed receipt escaped", err)
	}
}
