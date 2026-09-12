package quickjs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/rlm/engine"
)

func TestFactoryRejectsInvalidPolicies(t *testing.T) {
	for _, tt := range []struct {
		name    string
		options engine.Options
	}{
		{name: "negative payload limit", options: engine.Options{Limits: engine.Limits{MaxResultBytes: -1}}},
		{name: "malformed tool name", options: engine.Options{AllowedTools: []string{"../files"}}},
		{name: "reserved property", options: engine.Options{AllowedTools: []string{"files.constructor"}}},
		{name: "duplicate authority", options: engine.Options{AllowedTools: []string{"files.read", "files.read"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			factory, err := NewFactory(t.Context(), tt.options)
			if err == nil || factory != nil {
				t.Fatalf("invalid policy produced factory=%v, error=%v", factory, err)
			}
		})
	}
}

func TestInterruptedGuestCannotBeResumed(t *testing.T) {
	factory, err := NewFactory(t.Context(), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close(t.Context())
	if vm, err := factory.New(t.Context(), ""); err == nil || vm != nil {
		t.Fatalf("empty session identity accepted: %v, %v", vm, err)
	}
	vm, err := factory.New(t.Context(), "interruption")
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close(t.Context())
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := vm.ValidateCell(cancelled, "cell", "1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission: %v", err)
	}
	if _, err := vm.Drain(t.Context(), 0); err == nil {
		t.Fatal("zero job budget accepted")
	}
	ctx, stop := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer stop()
	if err := vm.RunCell(ctx, "cell", "while (true) {}"); err == nil {
		t.Fatal("unbounded guest loop survived cancellation")
	}
	if _, err := vm.Inspect(t.Context()); !errors.Is(err, ErrClosed) {
		t.Fatalf("interrupted C stack remained inspectable: %v", err)
	}
	if err := vm.RunCell(t.Context(), "next", "42"); !errors.Is(err, ErrClosed) {
		t.Fatalf("interrupted runtime resumed: %v", err)
	}
	if requests := vm.TakeRequests(); len(requests) != 0 {
		t.Fatalf("interrupted runtime retained host authority: %+v", requests)
	}
	// Discarding one interrupted runtime must leave its factory usable.
	fresh, err := factory.New(t.Context(), "replacement")
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close(t.Context())
	if err := fresh.RunCell(t.Context(), "fresh", "42"); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.Drain(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	view, err := fresh.Inspect(t.Context())
	if err != nil || string(view.Value) != "42" {
		t.Fatalf("replacement runtime: %+v, %v", view, err)
	}
}

func TestSyntaxErrorsAndJobSlicesPreserveCellBoundaries(t *testing.T) {
	factory, err := NewFactory(t.Context(), engine.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close(t.Context())
	vm, err := factory.New(t.Context(), "job-slices")
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close(t.Context())
	if err := vm.RunCell(t.Context(), "syntax", "let = ;"); err == nil {
		t.Fatal("invalid syntax accepted")
	}
	view, err := vm.Inspect(t.Context())
	if err != nil || view.Error == nil {
		t.Fatalf("syntax rejection missing from view: %+v, %v", view, err)
	}
	if _, err := vm.Drain(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	if err := vm.Finish(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := vm.RunCell(t.Context(), "jobs", `var finished = false; let chain = Promise.resolve(); for (let i = 0; i < 8; i++) chain = chain.then(() => {}); chain.then(() => { finished = true }); 42`); err != nil {
		t.Fatalf("syntax error destroyed the runtime: %v", err)
	}
	if jobs, err := vm.Drain(t.Context(), 1); jobs != 1 || !errors.Is(err, engine.ErrJobBudget) {
		t.Fatalf("bounded job slice: jobs=%d, error=%v", jobs, err)
	}
	if err := vm.RunCell(t.Context(), "too-soon", "1"); !errors.Is(err, ErrBusy) {
		t.Fatalf("new cell admitted before microtasks settled: %v", err)
	}
	if err := vm.Finish(t.Context()); !errors.Is(err, ErrBusy) {
		t.Fatalf("authority closed before microtasks settled: %v", err)
	}
	if _, err := vm.Checkpoint(t.Context()); !errors.Is(err, ErrQueuedRequests) {
		t.Fatalf("snapshot captured unsettled microtasks: %v", err)
	}
	if _, err := vm.Drain(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	if err := vm.Finish(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := vm.RunCell(t.Context(), "after-jobs", "finished"); err != nil {
		t.Fatal(err)
	}
	if _, err := vm.Drain(t.Context(), 100); err != nil {
		t.Fatal(err)
	}
	view, err = vm.Inspect(t.Context())
	if err != nil || string(view.Value) != "true" {
		t.Fatalf("bounded draining lost queued work: %+v, %v", view, err)
	}
}
