package rpc_test

import (
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestInputSteeringRPCPromotesOriginalInputAndKeepsDeletedReceipt(t *testing.T) {
	r, c := fixture(t)
	root := create(t, c).Root
	// This fixture leaves workers stopped and owns its isolated database; claiming
	// directly makes the socket projection test independent of model timing.
	db, err := store.Open(t.Context(), filepath.Join(filepath.Dir(r.SocketPath()), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	submit := func(id string) protocol.Admission {
		return call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "human", RequestID: protocol.ID(id)}, SessionID: root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: id}}})
	}
	submit("opening")
	target, err := db.Claim(t.Context(), session.SessionID(root.ID))
	if err != nil {
		t.Fatal(err)
	}
	queued := submit("queued")
	params := protocol.SteerInputParams{EditID: "promotion", SessionID: root.ID, InputID: queued.Input.ID, TurnID: protocol.ID(target.Turn.ID)}
	accepted := call[protocol.InputSteeringResult](t, c, "inputs.steer", params)
	if accepted.Input.ID != queued.Input.ID || accepted.Input.TurnID != nil || accepted.Input.Steering.Consumed {
		t.Fatal(accepted)
	}
	read := call[protocol.InputSteeringResult](t, c, "inputs.steering", protocol.InputSteeringParams{EditID: params.EditID, SessionID: root.ID})
	if read.ID != accepted.ID || read.TurnID != params.TurnID {
		t.Fatal(read)
	}
	if _, err := db.ObserveSteers(t.Context(), target.Turn.ID); err != nil {
		t.Fatal(err)
	}
	consumed := call[protocol.InputSteeringResult](t, c, "inputs.steer", params)
	if !consumed.Input.Steering.Consumed || *consumed.Input.TurnID != params.TurnID {
		t.Fatal(consumed)
	}
	requireHistoryError(t, c, "inputs.steering", protocol.InputSteeringParams{EditID: params.EditID, SessionID: "foreign"}, "NOT_FOUND")
	changed := params
	changed.TurnID = "other"
	requireHistoryError(t, c, "inputs.steer", changed, "CONFLICT")
	bad := protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "human", RequestID: "bad"}, SessionID: root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "bad"}}, Delivery: "queued", TargetTurnID: &params.TurnID}
	var rejected protocol.Admission
	if err := c.Call(t.Context(), "sessions.submit", bad, &rejected); err == nil {
		t.Fatal("queued delivery accepted target")
	}
	if _, err := db.Finish(t.Context(), target.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	call[protocol.DeleteResult](t, c, "sessions.delete", protocol.SessionParams{SessionID: root.ID})
	deleted := call[protocol.InputSteeringResult](t, c, "inputs.steer", params)
	if !deleted.Deleted || deleted.Input != nil {
		t.Fatal(deleted)
	}
}
