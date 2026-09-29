package tool

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestCapturedResourceRetirementCancelsPendingPermission(t *testing.T) {
	db, dispatcher, owner, turn, _ := dispatchFixture(t)
	lifetime, retire := context.WithCancel(t.Context())
	defer retire()
	var executions atomic.Int32
	dispatcher.coordination = preparedFixture(func(context.Context, session.Session, Invocation) (Prepared, error) {
		return Prepared{
			Capability: "mcp.call", Resource: "captured-server-generation", Arguments: json.RawMessage(`{}`), Lifetime: lifetime,
			Acquire: func(context.Context) (func(), error) { executions.Add(1); return func() {}, nil },
			Run:     func(context.Context, session.OperationID) (any, error) { executions.Add(1); return nil, nil },
		}, nil
	})
	done := make(chan error, 1)
	go func() {
		_, _, err := dispatcher.Call(t.Context(), Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "retired", Module: "mcp", Name: "call"})
		done <- err
	}()
	pending := awaitOperation(t, db, turn.ID, "retired", session.OperationWaiting)
	retire()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("retirement error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("retired resource still waiting for consent")
	}
	operation, err := db.Operation(t.Context(), pending.ID)
	if err != nil || operation.State != session.OperationCancelled || executions.Load() != 0 {
		t.Fatalf("retired operation executed or remained pending: %+v %v effects=%d", operation, err, executions.Load())
	}
	if _, _, err := dispatcher.Call(t.Context(), Invocation{SessionID: owner.ID, CellID: "cell", RequestID: "already-retired", Module: "mcp", Name: "call"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("already retired preparation: %v", err)
	}
}
