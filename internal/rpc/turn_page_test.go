package rpc_test

import (
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestTurnPageRPCReadsCanonicalOwnerMetadataWithoutClaiming(t *testing.T) {
	r, c := fixture(t)
	owner := create(t, c).Root.ID
	call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{
		Identity: protocol.RequestIdentity{ClientID: "history", RequestID: "first"}, SessionID: owner,
		Source: "user", Parts: []protocol.Part{{Type: "text", Text: "first"}},
	})
	empty := call[protocol.TurnPageResult](t, c, "sessions.turns", protocol.TurnPageParams{SessionID: owner, Limit: 1})
	if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != nil {
		t.Fatal("metadata read claimed work", empty)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitHistoryTurn(t, c, "first")
	submitHistoryTurn(t, c, owner, "second")
	first := call[protocol.TurnPageResult](t, c, "sessions.turns", protocol.TurnPageParams{SessionID: owner, Limit: 1})
	if len(first.Items) != 1 || first.NextCursor == nil || *first.NextCursor != first.Items[0].ID {
		t.Fatal(first)
	}
	canonical := call[protocol.Turn](t, c, "turns.get", protocol.TurnParams{TurnID: first.Items[0].ID})
	if !reflect.DeepEqual(canonical, first.Items[0]) {
		t.Fatal("page changed canonical turn", canonical, first.Items[0])
	}
	call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: owner, Lifecycle: "stopped"})
	older := call[protocol.TurnPageResult](t, c, "sessions.turns", protocol.TurnPageParams{SessionID: owner, Before: first.NextCursor, Limit: 1})
	if len(older.Items) != 1 || older.Items[0].ID == first.Items[0].ID || older.NextCursor != nil {
		t.Fatal(older)
	}
	foreign := create(t, c).Root.ID
	requireHistoryError(t, c, "sessions.turns", protocol.TurnPageParams{SessionID: foreign, Before: first.NextCursor, Limit: 1}, "NOT_FOUND")
	requireHistoryError(t, c, "sessions.turns", protocol.TurnPageParams{SessionID: "missing", Limit: 1}, "NOT_FOUND")
	var invalid protocol.TurnPageResult
	if err := c.Call(t.Context(), "sessions.turns", protocol.TurnPageParams{SessionID: owner, Limit: 101}, &invalid); err == nil {
		t.Fatal("invalid limit passed contract validation")
	}
}
