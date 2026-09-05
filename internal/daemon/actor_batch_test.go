package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// A failing event in the middle of a worker batch used to make the actor
// return with the rest of the batch dropped on the floor: a worker blocked in
// routeControl never got its reply, and the shutdown drain then waited on that
// worker forever. The remainder must be handed back so the flush answers it.
func TestActorFailureAnswersTheRestOfTheBatch(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	reply := make(chan error, 1)
	controlRan := false
	batch := []workerEnvelope{
		{kind: workerStream, stream: &streamEnvelope{}}, // incomplete: the actor fails here
		{kind: workerControl, control: func(context.Context) error { controlRan = true; return nil }, reply: reply},
	}
	if err := root.processWorkerBatch(batch); err == nil {
		t.Fatal("an incomplete stream event must fail the batch")
	}
	if controlRan {
		t.Fatal("a control behind the failing event must not run on a failed actor")
	}
	select {
	case err := <-reply:
		t.Fatalf("control was answered before the flush: %v", err)
	default:
	}
	// run() flushes whatever is left on the queue after the actor returns.
	if err := root.flushPendingEvents(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	select {
	case err := <-reply:
		if !errors.Is(err, ErrStopped) {
			t.Fatalf("abandoned control reply = %v, want ErrStopped", err)
		}
	case <-time.After(time.Second):
		t.Fatal("abandoned control was never answered: the worker would block forever")
	}
}
