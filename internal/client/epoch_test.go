package client_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestEphemeralCallsPinObservedProcessBeforeDispatch(t *testing.T) {
	var calls atomic.Int32
	c, _ := peer(t, func(_ context.Context, request protocol.Request) *protocol.Response {
		calls.Add(1)
		return success(t, request, protocol.ShellInputResult{Sequence: 1})
	})
	params := protocol.ShellInputParams{SessionID: "owner", OperationID: "operation", Sequence: 1, DataBase64: "eA=="}
	var result protocol.ShellInputResult
	for _, epoch := range []protocol.ID{"", "replacement"} {
		if err := c.CallAtEpoch(t.Context(), epoch, "shell.input", params, &result); err == nil {
			t.Fatalf("epoch %q was accepted", epoch)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("ephemeral input reached a mismatched process")
	}
	if err := c.CallAtEpoch(t.Context(), "boot", "shell.input", params, &result); err != nil || result.Sequence != 1 || calls.Load() != 1 {
		t.Fatalf("observed process rejected: %+v %v, calls=%d", result, err, calls.Load())
	}
	if err := c.Call(t.Context(), "shell.input", params, &result); err != nil || calls.Load() != 2 {
		t.Fatalf("ordinary durable recovery path changed: %v", err)
	}
}
