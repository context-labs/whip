package rpc_test

import (
	"errors"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestModelInspectionRPCOwnerAndPreparedEvidence(t *testing.T) {
	r, c := fixture(t)
	root := create(t, c).Root
	identity := protocol.RequestIdentity{ClientID: "capture", RequestID: "input"}
	call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{Identity: identity, SessionID: root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "hi"}}})
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	var turn protocol.Turn
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := call[protocol.Admission](t, c, "receipts.get", identity)
		if got.Turn != nil && got.Turn.FinishedAt != nil {
			turn = *got.Turn
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(time.Millisecond)
	}
	attempts := call[protocol.ModelAttemptsResult](t, c, "turns.attempts", protocol.ModelAttemptsParams{TurnID: turn.ID, Limit: 100})
	if len(attempts.Items) != 1 {
		t.Fatal(attempts)
	}
	got := call[protocol.ModelInspection](t, c, "models.inspection", protocol.ModelInspectionParams{SessionID: root.ID, AttemptID: attempts.Items[0].ID})
	if got.Capture == nil || got.Capture.RequestDigest != got.RequestDigest || got.Capture.Instructions.Status != "available" {
		t.Fatal(got)
	}
	var missing protocol.ModelInspection
	var wire *client.Error
	foreign := create(t, c).Root
	if err := c.Call(t.Context(), "models.inspection", protocol.ModelInspectionParams{SessionID: foreign.ID, AttemptID: got.AttemptID}, &missing); !errors.As(err, &wire) || wire.Kind != "NOT_FOUND" {
		t.Fatal(err)
	}
}
