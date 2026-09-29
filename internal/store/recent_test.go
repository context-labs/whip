package store

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestRecentTreesUsesDeepActivityAndCurrentRootModelWithoutClaiming(t *testing.T) {
	s := fresh(t)
	older, root := create(t, s, nil)
	newer, _ := create(t, s, nil)
	before, err := s.RecentTrees(t.Context(), 1)
	if err != nil || len(before.Items) != 1 || before.Items[0].ID != newer.ID || !before.HasMore {
		t.Fatal(before, err)
	}
	child := controlChild(t, s, root.ID, "deep-activity")
	grandchild := controlChild(t, s, child.Session.ID, "deeper-activity")
	page, err := s.RecentTrees(t.Context(), 100)
	if err != nil || len(page.Items) != 2 || page.Items[0].ID != older.ID || page.HasMore || page.CatalogRevision != before.CatalogRevision {
		t.Fatal(page, err)
	}
	if _, err := s.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "test", Name: "new-root-model"}}); err != nil {
		t.Fatal(err)
	}
	claimed := claim(t, s, grandchild.Session.ID)
	turns := count(t, s, "turns")
	page, err = s.RecentTrees(t.Context(), 1)
	if err != nil || len(page.Items) != 1 || count(t, s, "turns") != turns {
		t.Fatal(page, err)
	}
	item := page.Items[0]
	if item.RootID != root.ID || item.WorkingDirectory != root.WorkingDirectory || item.Model.Name != "new-root-model" || item.LastActivityAt.Before(claimed.Turn.StartedAt) {
		t.Fatal(item, claimed)
	}
	// Observing recent order neither drains child inputs nor changes ID pagination.
	catalog, err := s.Trees(t.Context(), TreeList{Limit: 100})
	if err != nil || len(catalog.Items) != 2 || catalog.Items[0].ID >= catalog.Items[1].ID {
		t.Fatal(catalog, err)
	}
}

func TestRecentTreesTiesBoundsCancellationAndDeletion(t *testing.T) {
	s := fresh(t)
	empty, err := s.RecentTrees(t.Context(), 100)
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.HasMore || empty.CatalogRevision == 0 {
		t.Fatal(empty, err)
	}
	roots := map[session.TreeID]session.SessionID{}
	var ids []session.TreeID
	for range 3 {
		tree, root := create(t, s, nil)
		ids = append(ids, tree.ID)
		roots[tree.ID] = root.ID
	}
	// Equal fixture timestamps exercise the deterministic tie break without
	// manufacturing inputs, turns or messages.
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	execTest(t, s, "UPDATE session_trees SET created_at=?", at.UnixMicro())
	slices.Sort(ids)
	slices.Reverse(ids)
	page, err := s.RecentTrees(t.Context(), 2)
	if err != nil || len(page.Items) != 2 || !page.HasMore || page.Items[0].ID != ids[0] || page.Items[1].ID != ids[1] || !page.Items[0].LastActivityAt.Equal(at) {
		t.Fatal(page, err)
	}
	if err := s.DeleteSubtree(t.Context(), roots[ids[0]]); err != nil {
		t.Fatal(err)
	}
	after, err := s.RecentTrees(t.Context(), 2)
	if err != nil || len(after.Items) != 2 || after.HasMore || after.Items[0].ID != ids[1] || after.CatalogRevision <= page.CatalogRevision {
		t.Fatal(after, err)
	}
	for _, limit := range []int{-1, 0, 101} {
		if _, err := s.RecentTrees(t.Context(), limit); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(limit, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.RecentTrees(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
