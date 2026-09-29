package store

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestAttentionExactOwnersIncludeDeepStoppedQueuesAndJoinedWaits(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	admitOperation(t, s, operationSpec(cell, "permission"))
	beginQuestion(t, s, cell, "question", false)
	child := controlChild(t, s, owner.ID, "child")
	grandchild := controlChild(t, s, child.Session.ID, "grandchild")
	if _, err := s.SetLifecycle(t.Context(), grandchild.Session.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	_, idle := create(t, s, nil)
	_, workspace := create(t, s, nil)
	if _, claimed, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, workspaceRequestTest(workspace.ID, "capture"), workspaceBindingTest(t)); err != nil || !claimed {
		t.Fatal(err)
	}
	if _, err := s.SetLifecycle(t.Context(), workspace.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	before := count(t, s, "turns")
	seen := map[session.SessionID]session.AttentionItem{}
	var cursor *session.AttentionCursor
	for range 5 {
		page, err := s.Attention(t.Context(), cursor, 1, 4096)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if _, ok := seen[item.SessionID]; ok {
				t.Fatal("duplicate owner")
			}
			seen[item.SessionID] = item
			actual, err := s.Activity(t.Context(), item.SessionID)
			if err != nil || !reflect.DeepEqual(actual, item.Activity) {
				t.Fatal(item, actual, err)
			}
		}
		cursor = page.NextCursor
		if cursor == nil {
			break
		}
	}
	if len(seen) != 4 || count(t, s, "turns") != before {
		t.Fatal(seen)
	}
	if _, ok := seen[idle.ID]; ok {
		t.Fatal("idle owner shown")
	}
	if value := seen[owner.ID]; value.Activity.PendingPermissionCount != 1 || value.Activity.PendingQuestionCount != 1 {
		t.Fatal(value)
	}
	if value := seen[grandchild.Session.ID]; value.RootID != owner.ID || value.Activity.Lifecycle != session.Stopped || value.Activity.QueuedInputCount != 1 {
		t.Fatal(value)
	}
	if value := seen[workspace.ID]; value.Activity.ActiveWorkspaceActionID == nil || value.Activity.Lifecycle != session.Stopped {
		t.Fatal(value)
	}
	if err := s.DeleteSubtree(t.Context(), grandchild.Session.ID); err != nil {
		t.Fatal(err)
	}
	page, err := s.Attention(t.Context(), nil, 100, 512<<10)
	if err != nil || len(page.Items) != 3 {
		t.Fatal(page, err)
	}
}

func TestAttentionValidationCancellationAndByteResume(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	for _, name := range []string{"one", "two", "three", "four", "five", "six"} {
		controlChild(t, s, root.ID, name)
	}
	first, err := s.Attention(t.Context(), nil, 100, 4096)
	if err != nil || first.NextCursor == nil || len(first.Items) == 0 || len(first.Items) >= 6 {
		t.Fatal(first, err)
	}
	second, err := s.Attention(t.Context(), first.NextCursor, 100, 4096)
	if err != nil || len(second.Items) == 0 || second.Items[0].SessionID <= first.NextCursor.SessionID {
		t.Fatal(second, err)
	}
	for _, bad := range []*session.AttentionCursor{{TreeID: "missing"}, {SessionID: "owner"}, {TreeID: "../", SessionID: "owner"}} {
		if _, err := s.Attention(t.Context(), bad, 1, 4096); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := s.Attention(t.Context(), nil, 101, 4096); err == nil {
		t.Fatal("oversized count")
	}
	if _, err := s.Attention(t.Context(), nil, 1, 4095); err == nil {
		t.Fatal("invalid bytes")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Attention(ctx, nil, 1, 4096); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
