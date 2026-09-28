package runtime

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestObservationRevisionRejectsOldCursorAfterRewind(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	owner := createTest(t, r)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "before")
	waitTest(t, r, "before", terminal)
	before := observeTest(t, r, owner.ID)
	if before.Snapshot.Revision != 1 || before.Snapshot.MessageCount != 2 {
		t.Fatal(before)
	}
	if _, err := r.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	edit, err := r.Rewind(t.Context(), rewindRuntimeRequest(t, r, owner.ID, 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ObserveRevision(t.Context(), owner.ID, before.Snapshot.ThroughSequence, 100, &before.Snapshot.Revision); !errors.Is(err, store.ErrConflict) {
		t.Fatal("old observation cursor survived", err)
	}
	after, err := r.ObserveRevision(t.Context(), owner.ID, 0, 100, nil)
	if err != nil || after.Snapshot.Revision != edit.Revision || after.Snapshot.MessageCount != 0 || len(after.Messages) != 0 || after.Preview != nil {
		t.Fatal(after, err)
	}
	if _, err := r.SetLifecycle(t.Context(), owner.ID, session.Active); err != nil {
		t.Fatal(err)
	}
	submitTest(t, r, owner.ID, "after")
	waitTest(t, r, "after", terminal)
	after, err = r.ObserveRevision(t.Context(), owner.ID, 0, 100, &edit.Revision)
	if err != nil || len(after.Messages) != 2 || after.Messages[0].Sequence != 3 || after.Snapshot.ThroughSequence != 4 {
		t.Fatal(after, err)
	}
}
