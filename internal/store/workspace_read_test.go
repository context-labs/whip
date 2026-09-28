package store

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestWorkspaceActionReadIsOwnerScopedAndObservesUncertainty(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	request := workspaceRequestTest(owner.ID, "capture")
	if _, _, err := s.ClaimWorkspace(t.Context(), session.WorkspaceCapture, request, workspaceBindingTest(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkspaceAction(t.Context(), "other", request.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("wrong owner saw action", err)
	}
	for _, state := range []session.WorkspaceActionState{session.WorkspaceClaimed, session.WorkspaceUncertain} {
		if state == session.WorkspaceUncertain {
			if _, err := s.SettleWorkspace(t.Context(), request.ID, false); err != nil {
				t.Fatal(err)
			}
		}
		for range 2 {
			value, err := s.WorkspaceAction(t.Context(), owner.ID, request.ID)
			if err != nil || value.State != state || value.ID != request.ID || value.SnapshotID != request.SnapshotID {
				t.Fatal(value, err)
			}
		}
	}
	if count(t, s, "workspace_actions") != 1 || count(t, s, "turns") != 0 {
		t.Fatal("read admitted execution")
	}
}
