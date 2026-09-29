package rpc_test

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestCellOutputRPCIsEpochScopedReadWithoutExecution(t *testing.T) {
	r, c := fixture(t)
	root := create(t, c).Root
	identity := protocol.RequestIdentity{ClientID: "output", RequestID: "queued"}
	call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{Identity: identity, SessionID: root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "work"}}})
	for range 3 {
		value := call[protocol.CellOutput](t, c, "cells.output", protocol.SessionParams{SessionID: root.ID})
		if value.Epoch != protocol.ID(r.ProcessEpoch()) || value.Preview != nil {
			t.Fatal(value)
		}
	}
	if receipt := call[protocol.Admission](t, c, "receipts.get", identity); receipt.Turn != nil {
		t.Fatal("output observation started queued work", receipt)
	}
	var result protocol.CellOutput
	var remote *client.Error
	if err := c.Call(t.Context(), "cells.output", protocol.SessionParams{SessionID: "absent"}, &result); !errors.As(err, &remote) || remote.Kind != "NOT_FOUND" {
		t.Fatal(err)
	}
	for _, params := range []any{map[string]any{"session_id": root.ID, "cell_id": "foreign"}, map[string]any{"session_id": ""}} {
		if err := c.Call(t.Context(), "cells.output", params, &result); err == nil {
			t.Fatal("invalid output query accepted", err)
		}
	}
}
