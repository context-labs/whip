package rpc_test

import (
	"errors"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestUsageRPCReadsCanonicalOwnerWithoutStartingQueuedWork(t *testing.T) {
	r, c := fixture(t)
	root := create(t, c).Root
	identity := protocol.RequestIdentity{ClientID: "usage", RequestID: "input"}
	call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{Identity: identity, SessionID: root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "hi"}}})
	before := call[protocol.Usage](t, c, "usage.get", protocol.SessionParams{SessionID: root.ID})
	if before.SessionID != root.ID || before.Attempts != (protocol.UsageAttempts{}) {
		t.Fatal(before)
	}
	if receipt := call[protocol.Admission](t, c, "receipts.get", identity); receipt.Turn != nil {
		t.Fatal("usage read started queued input", receipt)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		receipt := call[protocol.Admission](t, c, "receipts.get", identity)
		if receipt.Turn != nil && receipt.Turn.FinishedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completion timed out")
		}
		time.Sleep(time.Millisecond)
	}
	got := call[protocol.Usage](t, c, "usage.get", protocol.SessionParams{SessionID: root.ID})
	if got.Attempts.Settled != 1 || got.ReportedCost.Attempts+got.EstimatedCost.Attempts+got.UnknownCost != 1 || got.InputTokens.KnownAttempts+got.InputTokens.MissingAttempts != 1 {
		t.Fatal(got)
	}
	foreign := create(t, c).Root
	if got := call[protocol.Usage](t, c, "usage.get", protocol.SessionParams{SessionID: foreign.ID}); got.Attempts.Settled != 0 {
		t.Fatal("cross-root usage", got)
	}
	var missing protocol.Usage
	var wire *client.Error
	if err := c.Call(t.Context(), "usage.get", protocol.SessionParams{SessionID: "absent"}, &missing); !errors.As(err, &wire) || wire.Kind != "NOT_FOUND" {
		t.Fatal(err)
	}
}
