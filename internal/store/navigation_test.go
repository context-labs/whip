package store

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestTreeSummariesAggregateDeepOwnersWithoutClaimingOrHydrating(t *testing.T) {
	s := fresh(t)
	root, cell := operationCell(t, s)
	admitOperation(t, s, operationSpec(cell, "permission"))
	beginQuestion(t, s, cell, "question", false)
	child := controlChild(t, s, root.ID, "child")
	grandchild := controlChild(t, s, child.Session.ID, "deep")
	if _, err := s.SetLifecycle(t.Context(), grandchild.Session.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	_, idle := create(t, s, nil)
	_, workspace := create(t, s, nil)
	if _, claimed, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, workspaceRequestTest(workspace.ID, "capture"), workspaceBindingTest(t)); err != nil || !claimed {
		t.Fatal(err)
	}
	before := count(t, s, "turns")
	page, err := s.TreeSummaries(t.Context(), []session.SessionID{root.ID, idle.ID, workspace.ID, child.Session.ID, "missing"})
	if err != nil || len(page.Items) != 3 || !slices.Equal(page.Missing, []session.SessionID{child.Session.ID, "missing"}) || count(t, s, "turns") != before {
		t.Fatal(page, err)
	}
	for _, item := range page.Items {
		switch item.RootID {
		case root.ID:
			if item.ID != root.TreeID || item.WorkingDirectory != root.WorkingDirectory || item.ActiveTurnCount != 1 || item.QueuedInputCount != 2 || item.PendingPermissionCount != 1 || item.PendingQuestionCount != 1 || item.ActiveWorkspaceActionCount != 0 {
				t.Fatal(item)
			}
		case idle.ID:
			if item.ActiveTurnCount+item.QueuedInputCount+item.PendingPermissionCount+item.PendingQuestionCount+item.ActiveWorkspaceActionCount != 0 {
				t.Fatal(item)
			}
		case workspace.ID:
			if item.ActiveWorkspaceActionCount != 1 {
				t.Fatal(item)
			}
		}
	}
	if err := s.DeleteSubtree(t.Context(), grandchild.Session.ID); err != nil {
		t.Fatal(err)
	}
	page, err = s.TreeSummaries(t.Context(), []session.SessionID{root.ID})
	if err != nil || len(page.Items) != 1 || page.Items[0].QueuedInputCount != 1 {
		t.Fatal(page, err)
	}
}

func TestTreeSummariesBoundsMissingAndCancellation(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	for _, ids := range [][]session.SessionID{nil, {}, {root.ID, root.ID}, {"../"}, make([]session.SessionID, 65)} {
		if _, err := s.TreeSummaries(t.Context(), ids); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(ids, err)
		}
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	page, err := s.TreeSummaries(t.Context(), []session.SessionID{root.ID})
	if err != nil || len(page.Items) != 0 || !slices.Equal(page.Missing, []session.SessionID{root.ID}) {
		t.Fatal(page, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.TreeSummaries(ctx, []session.SessionID{"missing"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
