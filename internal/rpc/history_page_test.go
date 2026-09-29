package rpc_test

import (
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestHistoryPageRPCReadsTailAndRejectsStaleRevisionWithoutStartingWork(t *testing.T) {
	r, c := fixture(t)
	tree := create(t, c)
	request := protocol.HistoryPageParams{SessionID: tree.Root.ID, Direction: "backward", Limit: 2}
	if page := call[protocol.HistoryPageResult](t, c, "sessions.history_page", request); len(page.Messages) != 0 || page.NextCursor != nil {
		t.Fatal(page)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	submitHistoryTurn(t, c, tree.Root.ID, "first")
	submitHistoryTurn(t, c, tree.Root.ID, "second")
	tail := call[protocol.HistoryPageResult](t, c, "sessions.history_page", request)
	if len(tail.Messages) != 2 || tail.Messages[0].Sequence != 3 || tail.Messages[1].Sequence != 4 || tail.NextCursor == nil || *tail.NextCursor != 3 {
		t.Fatal("invalid tail", tail)
	}
	request.Cursor, request.ExpectedRevision = tail.NextCursor, new(tail.Snapshot.Revision)
	older := call[protocol.HistoryPageResult](t, c, "sessions.history_page", request)
	if len(older.Messages) != 2 || older.Messages[0].Sequence != 1 || older.NextCursor != nil {
		t.Fatal("invalid older page", older)
	}
	call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: tree.Root.ID, Lifecycle: "stopped"})
	call[protocol.HistoryEdit](t, c, "sessions.rewind", protocol.RewindParams{EditID: "page-rewind", SessionID: tree.Root.ID, ExpectedRevision: tail.Snapshot.Revision, ObservedThrough: tail.Snapshot.ThroughSequence, KeepThrough: 2})
	requireHistoryError(t, c, "sessions.history_page", request, "CONFLICT")
}
