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
	if err != nil || len(full.Items) != 3 || full.Next != nil {
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
	if !reflect.DeepEqual(collected, full.Items) || collected[1].Ref.ID != "custom" || collected[2].Ref.ID != "custom" || collected[1].Ref.Revision >= collected[2].Ref.Revision {
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
