package quickjs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/rlm/engine"
)

func TestValidationPreservesPendingWork(t *testing.T) {
	factory, err := NewFactory(t.Context(), engine.Options{AllowedTools: []string{"files.read"}, Limits: engine.Limits{MaxRequestBytes: 256, MaxResultBytes: 256}})
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close(t.Context())
	vm, err := factory.New(t.Context(), "validation")
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close(t.Context())
	for _, tt := range []struct {
		name, id, code string
	}{
		{name: "empty identity", code: "1"},
		{name: "null identity", id: "a\x00b", code: "1"},
		{name: "oversized identity", id: strings.Repeat("a", 513), code: "1"},
		{name: "invalid source encoding", id: "cell", code: string([]byte{0xff})},
		{name: "oversized source", id: "cell", code: strings.Repeat("1", 257)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := vm.ValidateCell(t.Context(), tt.id, tt.code); err == nil {
				t.Fatal("invalid admission accepted")
			}
		})
	}
	const code = `await files.read({path:"note.txt"})`
	if err := vm.ValidateCell(t.Context(), "cell", code); err != nil {
		t.Fatal(err)
	}
	if err := vm.RunCell(t.Context(), "cell", code); err != nil {
		t.Fatalf("validation consumed the cell identity: %v", err)
	}
	if _, err := vm.Drain(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	requests := vm.TakeRequests()
	if len(requests) != 1 || requests[0].Tool != "files.read" || string(requests[0].Args) != `{"path":"note.txt"}` {
		t.Fatalf("pending request: %+v", requests)
	}
	if err := vm.ValidateCell(t.Context(), "next", "1"); !errors.Is(err, ErrBusy) {
		t.Fatalf("pending host work admitted another cell: %v", err)
	}
	if _, err := vm.Checkpoint(t.Context()); !errors.Is(err, ErrQueuedRequests) {
		t.Fatalf("pending work was checkpointed: %v", err)
	}
	id := requests[0].ID
	for _, tt := range []struct {
		name    string
		outcome engine.Outcome
	}{
		{name: "empty identity", outcome: engine.Outcome{OK: true, Value: []byte(`null`)}},
		{name: "null identity", outcome: engine.Outcome{ID: "a\x00b", OK: true, Value: []byte(`null`)}},
		{name: "success with error", outcome: engine.Outcome{ID: id, OK: true, Value: []byte(`null`), Error: &engine.RemoteError{Code: "E_HOST"}}},
		{name: "invalid result JSON", outcome: engine.Outcome{ID: id, OK: true, Value: []byte(`{"unfinished":`)}},
		{name: "trailing JSON value", outcome: engine.Outcome{ID: id, OK: true, Value: []byte(`42 true`)}},
		{name: "duplicate JSON keys", outcome: engine.Outcome{ID: id, OK: true, Value: []byte(`{"result":1,"result":2}`)}},
		{name: "invalid JSON key", outcome: engine.Outcome{ID: id, OK: true, Value: []byte(`{"unfinished`)}},
		{name: "excessive JSON nesting", outcome: engine.Outcome{ID: id, OK: true, Value: []byte(strings.Repeat("[", 66) + "0" + strings.Repeat("]", 66))}},
		{name: "non-finite exponent", outcome: engine.Outcome{ID: id, OK: true, Value: []byte(`1e1000`)}},
		{name: "oversized result", outcome: engine.Outcome{ID: id, OK: true, Value: []byte(`"` + strings.Repeat("a", 257) + `"`)}},
		{name: "failure without error", outcome: engine.Outcome{ID: id}},
		{name: "failure with value", outcome: engine.Outcome{ID: id, Value: []byte(`1`), Error: &engine.RemoteError{Code: "E_HOST"}}},
		{name: "oversized error", outcome: engine.Outcome{ID: id, Error: &engine.RemoteError{Code: "E_HOST", Message: strings.Repeat("a", 257)}}},
		{name: "invalid error encoding", outcome: engine.Outcome{ID: id, Error: &engine.RemoteError{Code: "E_HOST", Message: string([]byte{0xff})}}},
		{name: "encoded envelope exceeds limit", outcome: engine.Outcome{ID: id, Error: &engine.RemoteError{Code: "E_HOST", Message: strings.Repeat("a", 240)}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := vm.ValidateOutcome(t.Context(), tt.outcome); err == nil {
				t.Fatal("invalid host outcome accepted")
			}
			view, err := vm.Inspect(t.Context())
			if err != nil || len(view.Pending) != 1 || view.Pending[0] != id {
				t.Fatalf("validation changed pending work: %+v, %v", view, err)
			}
		})
	}
	valid := engine.Outcome{ID: id, OK: true, Value: []byte(`42`)}
	if err := vm.ValidateOutcome(t.Context(), engine.Outcome{ID: id, OK: true, Value: []byte(`[{"decimal":1.25,"exponent":1e3}]`)}); err != nil {
		t.Fatalf("finite nested JSON rejected: %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := vm.ValidateOutcome(cancelled, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled validation: %v", err)
	}
	if err := vm.ValidateOutcome(t.Context(), valid); err != nil {
		t.Fatal(err)
	}
	if accepted, err := vm.Deliver(t.Context(), valid); err != nil || !accepted {
		t.Fatalf("validation consumed the waiter: accepted=%v, %v", accepted, err)
	}
	if accepted, err := vm.Deliver(t.Context(), valid); err != nil || accepted {
		t.Fatalf("duplicate outcome accepted=%v, %v", accepted, err)
	}
	if _, err := vm.Drain(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	if err := vm.Finish(t.Context()); err != nil {
		t.Fatal(err)
	}
	view, err := vm.Inspect(t.Context())
	if err != nil || len(view.Pending) != 0 || !view.HasValue || string(view.Value) != "42" {
		t.Fatalf("settled result: %+v, %v", view, err)
	}
	if err := vm.ValidateCell(t.Context(), "cell", "1"); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("reused identity accepted: %v", err)
	}
}
