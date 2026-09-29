package store

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestTreeCatalogSnapshotBoundsMutationAndOverflow(t *testing.T) {
	s := fresh(t)
	empty, err := s.Trees(t.Context(), TreeList{Limit: 1})
	if err != nil || empty.Revision != 1 || len(empty.Items) != 0 || empty.Next != nil {
		t.Fatal(empty, err)
	}
	tree, root := create(t, s, nil)
	second, _ := create(t, s, nil)
	first, err := s.Trees(t.Context(), TreeList{Limit: 1})
	if err != nil || first.Revision != 3 || first.Next == nil {
		t.Fatal(first, err)
	}
	last, err := s.Trees(t.Context(), TreeList{After: *first.Next, Limit: 1, ExpectedRevision: &first.Revision})
	if err != nil || last.Revision != first.Revision || len(last.Items) != 1 || last.Next != nil {
		t.Fatal(last, err)
	}
	// An off-page title, filter-membership change, and explicit same-value intent
	// each invalidate the whole catalog, including the metadata CAS revision.
	for _, metadata := range []session.TreeMetadata{{Title: new("Changed"), Archived: true, Pinned: true}, {Title: new("Changed"), Archived: true, Pinned: true}} {
		before := catalogTest(t, s)
		tree, err = s.UpdateTree(t.Context(), tree.ID, tree.Revision, metadata)
		if err != nil || catalogTest(t, s) != before+1 {
			t.Fatal(tree, err)
		}
		if _, err := s.Trees(t.Context(), TreeList{After: *first.Next, Limit: 1, ExpectedRevision: &first.Revision}); !errors.Is(err, ErrConflict) {
			t.Fatal("mixed revision pages", err)
		}
	}
	before := catalogTest(t, s)
	if _, err := s.UpdateTree(t.Context(), tree.ID, 1, tree.Metadata); !errors.Is(err, ErrConflict) || catalogTest(t, s) != before {
		t.Fatal("failed CAS invalidated catalog", err)
	}
	if _, err := s.SetLifecycle(t.Context(), root.ID, session.Stopped); err != nil || catalogTest(t, s) != before {
		t.Fatal("noncatalog lifecycle invalidated", err)
	}
	filtered, err := s.Trees(t.Context(), TreeList{Archived: new(false), Limit: 100, ExpectedRevision: &before})
	if err != nil || len(filtered.Items) != 1 || filtered.Items[0].ID != second.ID {
		t.Fatal(filtered, err)
	}
	// Catalog exhaustion must roll back the metadata change as well.
	execTest(t, s, "DROP TRIGGER tree_catalog_revision")
	execTest(t, s, "UPDATE tree_catalog SET revision=9223372036854775807")
	if _, err := s.UpdateTree(t.Context(), tree.ID, tree.Revision, session.TreeMetadata{}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	current, err := s.Tree(t.Context(), tree.ID)
	if err != nil || !reflect.DeepEqual(current, tree) {
		t.Fatal("overflow partially changed metadata", current, err)
	}
	for _, request := range []TreeList{{Limit: 0}, {Limit: 101}, {Limit: 1, ExpectedRevision: new(session.Revision(0))}, {Limit: 1, ExpectedRevision: new(session.Revision(-1))}} {
		if _, err := s.Trees(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(request, err)
		}
	}
}

func TestTreeCatalogCreationForkDeletionAndTitleTransactionOwnership(t *testing.T) {
	s := fresh(t)
	root := titleOwner(t, s, true)
	before := catalogTest(t, s)
	titleAdmission(t, s, root, "first", "Please build a reliable recursive task backend")
	if catalogTest(t, s) != before+1 {
		t.Fatal("fallback not invalidated")
	}
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "title-test", RequestID: "first"}, Submission{SessionID: root.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "Please build a reliable recursive task backend"}}}); err != nil || catalogTest(t, s) != before+1 {
		t.Fatal("authored retry invalidated", err)
	}
	finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
	claimed := claimTitle(t, s, root)
	attempt := titleAttempt(t, s, claimed, "title")
	before = catalogTest(t, s)
	execTest(t, s, "CREATE TRIGGER catalog_failure BEFORE UPDATE ON tree_catalog BEGIN SELECT RAISE(ABORT,'injected'); END")
	if _, err := s.SettleAutomaticTitle(t.Context(), attempt.ID, compactionOutcomeTest(), &session.AutomaticTitleDraft{Text: "Generated title"}); err == nil {
		t.Fatal("title rollback missing")
	}
	if catalogTest(t, s) != before || count(t, s, "automatic_title_results") != 0 {
		t.Fatal("candidate or invalidation escaped rollback")
	}
	execTest(t, s, "DROP TRIGGER catalog_failure")
	for range 2 {
		if _, err := s.SettleAutomaticTitle(t.Context(), attempt.ID, compactionOutcomeTest(), &session.AutomaticTitleDraft{Text: "Generated title"}); err != nil || catalogTest(t, s) != before+1 {
			t.Fatal("title settlement retry invalidated", err)
		}
	}
	finishMailTest(t, s, claimed.Turn.ID, session.Succeeded)
	before = catalogTest(t, s)
	request := forkRequestTest(t, s, root.ID, "fork", 0)
	beforeTrees := count(t, s, "session_trees")
	execTest(t, s, "CREATE TRIGGER catalog_failure BEFORE UPDATE ON tree_catalog BEGIN SELECT RAISE(ABORT,'injected'); END")
	if _, err := s.Fork(t.Context(), request, forkDefaultsTest()); err == nil || count(t, s, "session_trees") != beforeTrees || catalogTest(t, s) != before {
		t.Fatal("fork and head did not roll back", err)
	}
	execTest(t, s, "DROP TRIGGER catalog_failure")

	fork := forkTest(t, s, request)
	forkTest(t, s, request)
	if catalogTest(t, s) != before+1 {
		t.Fatal("fork retry invalidated")
	}
	execTest(t, s, "CREATE TRIGGER catalog_failure BEFORE UPDATE ON tree_catalog BEGIN SELECT RAISE(ABORT,'injected'); END")
	if err := s.DeleteSubtree(t.Context(), fork.Root.ID); err == nil || catalogTest(t, s) != before+1 {
		t.Fatal("deletion and head did not roll back", err)
	}
	if _, err := s.Session(t.Context(), fork.Root.ID); err != nil {
		t.Fatal("root deletion escaped failed head write", err)
	}
	execTest(t, s, "DROP TRIGGER catalog_failure")

	if err := s.DeleteSubtree(t.Context(), fork.Root.ID); err != nil || catalogTest(t, s) != before+2 {
		t.Fatal("delete did not invalidate", err)
	}
	if err := s.DeleteSubtree(t.Context(), fork.Root.ID); !errors.Is(err, ErrNotFound) || catalogTest(t, s) != before+2 {
		t.Fatal("failed repeated delete invalidated", err)
	}
}

func TestTreeCatalogPageHasOneSnapshotDuringConcurrentMetadataWrites(t *testing.T) {
	s := fresh(t)
	tree, _ := create(t, s, nil)
	before := catalogTest(t, s)
	var wg sync.WaitGroup
	failures := make(chan error, 1)
	wg.Go(func() {
		for range 40 {
			var err error
			tree, err = s.UpdateTree(t.Context(), tree.ID, tree.Revision, tree.Metadata)
			if err != nil {
				failures <- err
				return
			}
		}
	})
	for range 80 {
		page, err := s.Trees(t.Context(), TreeList{Limit: 1})
		if err != nil || len(page.Items) != 1 || page.Revision-before != page.Items[0].Revision-1 {
			t.Fatal("head and metadata came from different snapshots", page, err)
		}
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
}

func TestTreeCatalogTitleSupersessionAndExhaustionStillSettleBilling(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual-same-value", true: "exhausted"}[overflow], func(t *testing.T) {
			s := fresh(t)
			root := titleOwner(t, s, true)
			titleAdmission(t, s, root, "source", "Please build a reliable recursive task backend")
			finishMailTest(t, s, claim(t, s, root.ID).Turn.ID, session.Succeeded)
			claimed := claimTitle(t, s, root)
			attempt := titleAttempt(t, s, claimed, "title")
			tree, err := s.Tree(t.Context(), root.TreeID)
			if err != nil {
				t.Fatal(err)
			}
			if overflow {
				execTest(t, s, "DROP TRIGGER tree_catalog_revision")
				execTest(t, s, "UPDATE tree_catalog SET revision=9223372036854775807")
			} else {
				before := catalogTest(t, s)
				tree, err = s.UpdateTree(t.Context(), tree.ID, tree.Revision, tree.Metadata)
				if err != nil || catalogTest(t, s) != before+1 {
					t.Fatal("manual intent did not invalidate catalog", err)
				}
			}
			head := catalogTest(t, s)
			result, err := s.SettleAutomaticTitle(t.Context(), attempt.ID, compactionOutcomeTest(), &session.AutomaticTitleDraft{Text: "Late generated title"})
			if err != nil || result.Candidate == nil || result.Candidate.Applied || result.Attempt.CostNanoUSD == nil || *result.Attempt.CostNanoUSD != 1200 || catalogTest(t, s) != head {
				t.Fatal("unapplied candidate lost billing or invalidated catalog", result, err)
			}
			current, err := s.Tree(t.Context(), tree.ID)
			if err != nil || !reflect.DeepEqual(current, tree) {
				t.Fatal("unapplied title changed metadata", current, err)
			}
		})
	}
}
