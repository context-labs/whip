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

func TestRecentTreePagesFilterPinAndAdvanceAdvisoryActivity(t *testing.T) {
	s := fresh(t)
	var ids []session.TreeID
	for range 5 {
		tree, _ := create(t, s, nil)
		ids = append(ids, tree.ID)
	}
	slices.Sort(ids)
	slices.Reverse(ids)
	at := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	execTest(t, s, "UPDATE session_trees SET created_at=?", at.UnixMicro())
	execTest(t, s, "UPDATE session_trees SET metadata=json_set(metadata,'$.pinned',json('true')) WHERE id=?", ids[4])
	execTest(t, s, "UPDATE session_trees SET metadata=json_set(metadata,'$.archived',json('true')) WHERE id=?", ids[0])
	request := session.RecentTreeList{Limit: 2, Archived: new(false), PinnedFirst: true}
	first, err := s.RecentTreesPage(t.Context(), request)
	if err != nil || len(first.Items) != 2 || first.Items[0].ID != ids[4] || first.Items[1].ID != ids[1] || first.Next == nil || !first.HasMore {
		t.Fatal(first, err)
	}
	request.After = first.Next
	second, err := s.RecentTreesPage(t.Context(), request)
	if err != nil || len(second.Items) != 2 || second.Items[0].ID != ids[2] || second.Items[1].ID != ids[3] || second.HasMore || second.Next != nil {
		t.Fatal(second, err)
	}
	pinned, err := s.RecentTreesPage(t.Context(), session.RecentTreeList{Limit: 100, Pinned: new(true)})
	if err != nil || len(pinned.Items) != 1 || pinned.Items[0].ID != ids[4] {
		t.Fatal(pinned, err)
	}
	archived, err := s.RecentTreesPage(t.Context(), session.RecentTreeList{Limit: 100, Archived: new(true)})
	if err != nil || len(archived.Items) != 1 || archived.Items[0].ID != ids[0] {
		t.Fatal(archived, err)
	}
	// A newer durable clock moves this root before the captured advisory cursor.
	// Refreshing discovers it; the stable-ID metadata catalog remains unchanged.
	revision, err := s.TreeCatalog(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	execTest(t, s, "UPDATE session_trees SET created_at=? WHERE id=?", at.Add(time.Second).UnixMicro(), ids[3])
	older, err := s.RecentTreesPage(t.Context(), request)
	if err != nil || len(older.Items) != 1 || older.Items[0].ID != ids[2] {
		t.Fatal(older, err)
	}
	request.After = nil
	latest, err := s.RecentTreesPage(t.Context(), request)
	if err != nil || latest.Items[1].ID != ids[3] || latest.CatalogRevision != revision {
		t.Fatal(latest, err)
	}
	for _, cursor := range []session.RecentTreeCursor{{TreeID: "", LastActivityAt: at}, {TreeID: ids[0]}, {TreeID: ids[0], LastActivityAt: at.Add(time.Nanosecond)}} {
		request.After = &cursor
		if _, err := s.RecentTreesPage(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("accepted cursor %+v: %v", cursor, err)
		}
	}
}
