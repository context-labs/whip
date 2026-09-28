package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestHistoryPagesRejectRetiredRevisionAndKeepAppendCursors(t *testing.T) {
	for _, kind := range []string{"root", "child"} {
		t.Run(kind, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			if kind == "child" {
				child := controlChild(t, s, owner.ID, "child")
				owner = *child.Session
				if _, err := s.CancelInput(t.Context(), child.Admission.Input.ID); err != nil {
					t.Fatal(err)
				}
			}
			empty, items, err := s.HistoryPage(t.Context(), owner.ID, 0, 100, nil)
			if err != nil || empty.Revision != 1 || empty.MessageCount != 0 || items == nil || len(items) != 0 {
				t.Fatal(empty, items, err)
			}
			first := compactionHistoryTest(t, s, owner.ID, "first")
			snapshot, page, err := s.HistoryPage(t.Context(), owner.ID, 0, 2, &empty.Revision)
			if err != nil || snapshot.ThroughSequence != 4 || snapshot.MessageCount != 4 || !reflect.DeepEqual(page, first[:2]) {
				t.Fatal(snapshot, page, err)
			}
			compactionHistoryTest(t, s, owner.ID, "second")
			appended, page, err := s.HistoryPage(t.Context(), owner.ID, 2, 100, &snapshot.Revision)
			if err != nil || appended.Revision != snapshot.Revision || appended.MessageCount != 8 || len(page) != 6 {
				t.Fatal(appended, page, err)
			}
			rewindStopped(t, s, owner.ID)
			edit, err := s.Rewind(t.Context(), rewindRequest(t, s, owner.ID, "edit", 4))
			if err != nil {
				t.Fatal(err)
			}
			for _, after := range []int64{0, 4, 8} {
				if _, _, err := s.HistoryPage(t.Context(), owner.ID, after, 100, &snapshot.Revision); !errors.Is(err, ErrConflict) {
					t.Fatal("stale page", after, err)
				}
				if _, err := s.HistoryMetadataAtRevision(t.Context(), owner.ID, after, 8, 100, &snapshot.Revision); !errors.Is(err, ErrConflict) {
					t.Fatal("stale metadata", after, err)
				}
				if _, err := s.SearchHistoryAtRevision(t.Context(), owner.ID, after, 8, "first", 100, &snapshot.Revision); !errors.Is(err, ErrConflict) {
					t.Fatal("stale search", after, err)
				}
			}
			current, page, err := s.HistoryPage(t.Context(), owner.ID, 0, 100, &edit.Revision)
			if err != nil || current.Revision != 2 || current.MessageCount != 4 || current.ThroughSequence != 4 || !reflect.DeepEqual(page, first) {
				t.Fatal(current, page, err)
			}
			metadata, err := s.HistoryMetadataAtRevision(t.Context(), owner.ID, 0, current.ThroughSequence, 100, &current.Revision)
			if err != nil || metadata.Revision != current.Revision || len(metadata.Items) != 4 {
				t.Fatal(metadata, err)
			}
			search, err := s.SearchHistoryAtRevision(t.Context(), owner.ID, 0, current.ThroughSequence, "missing", 100, &current.Revision)
			if err != nil || search.Revision != current.Revision || search.ScannedMessages != 4 {
				t.Fatal(search, err)
			}
			if _, _, err := s.HistoryPage(t.Context(), "missing", 0, 100, nil); !errors.Is(err, ErrNotFound) {
				t.Fatal("missing owner", err)
			}
			if _, _, err := s.HistoryPage(t.Context(), owner.ID, 0, 100, new(session.Revision(0))); !errors.Is(err, session.ErrInvalid) {
				t.Fatal("invalid revision", err)
			}
		})
	}
}

func TestHistoryPageAndRevisionShareOneDatabaseSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	writer := openTest(t, path)
	reader := openTest(t, path)
	_, owner := create(t, writer, nil)
	compactionHistoryTest(t, writer, owner.ID, "first")
	compactionHistoryTest(t, writer, owner.ID, "second")
	rewindStopped(t, writer, owner.ID)
	request := rewindRequest(t, writer, owner.ID, "edit", 4)
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		<-started
		_, err := writer.Rewind(t.Context(), request)
		finished <- err
	}()
	for i := range 100 {
		if i == 1 {
			close(started)
		}
		snapshot, page, err := reader.HistoryPage(t.Context(), owner.ID, 0, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		expected := int64(8)
		if snapshot.Revision == 2 {
			expected = 4
		} else if snapshot.Revision != 1 {
			t.Fatal(snapshot)
		}
		if snapshot.ThroughSequence != expected || snapshot.MessageCount != expected || int64(len(page)) != expected || page[len(page)-1].Sequence != expected {
			t.Fatal("mixed history snapshots", snapshot, page)
		}
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	snapshot, page, err := reader.HistoryPage(t.Context(), owner.ID, 0, 100, nil)
	if err != nil || snapshot.Revision != 2 || len(page) != 4 {
		t.Fatal(snapshot, page, err)
	}
}
