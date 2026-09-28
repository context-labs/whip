package process

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fatalTestError struct{ error }

func (fatalTestError) FatalHostError() {}
func (e fatalTestError) Unwrap() error { return e.error }

func TestFatalHostFailureCannotBeCaughtOrCheckpointed(t *testing.T) {
	for _, engine := range []string{EngineStarlark, EngineQuickJS} {
		t.Run(engine, func(t *testing.T) {
			checkpoints := &memoryCheckpoints{}
			injected := errors.New("unsettled accounting")
			var effects atomic.Int32
			host := HostFunc(func(_ context.Context, module, _ string, _ map[string]any) (any, error) {
				if module == "models" {
					return nil, fmt.Errorf("wrapped: %w", fatalTestError{injected})
				}
				effects.Add(1)
				return "effect", nil
			})
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			kernel, err := NewKernel(KernelOptions{Command: []string{executable, "-test.run=TestWorkerProcess", "--"}, Engine: engine, Host: host, Checkpoints: checkpoints})
			if err != nil {
				t.Fatal(err)
			}
			defer kernel.Close()
			initial, code := "before=1", `before=2
models.call(prompt="work")
files.write(path="later",content="forbidden")`
			if engine == EngineQuickJS {
				initial = "var before=1"
				code = `before=2;try {await models.call({prompt:"work"});}catch(e){print("caught");}await files.write({path:"later",content:"forbidden"});`
			}
			if _, err := kernel.Exec(t.Context(), Cell{Code: initial}); err != nil {
				t.Fatal(err)
			}
			saved := append([]byte{}, checkpoints.value.Data...)
			value, err := kernel.Exec(t.Context(), Cell{Code: code})
			if !errors.Is(err, injected) || !fatalHostError(err) || value.Settled || value.Scratch != nil || effects.Load() != 0 {
				t.Fatalf("fatal swallowed: result=%+v err=%v effects=%d", value, err, effects.Load())
			}
			if !bytes.Equal(saved, checkpoints.value.Data) || kernel.worker != nil {
				t.Fatal("aborted cell published checkpoint or retained worker")
			}
		})
	}
}

func TestQuickJSOrdinaryHostFailureRemainsCatchable(t *testing.T) {
	var effects atomic.Int32
	kernel := testQuickJS(t, HostFunc(func(_ context.Context, module, _ string, _ map[string]any) (any, error) {
		if module == "models" {
			return nil, errors.New("FatalHostError: ordinary settled provider error")
		}
		effects.Add(1)
		return "done", nil
	}), &memoryCheckpoints{})
	value, err := kernel.Exec(t.Context(), Cell{Code: `try {await models.call({prompt:"work"});} catch(e) {} await files.write({path:"later",content:"allowed"});`})
	if err != nil || !value.Settled || effects.Load() != 1 {
		t.Fatalf("ordinary error became fatal: %+v %v effects=%d", value, err, effects.Load())
	}
}

func TestQuickJSFatalHostFailureCancelsAndDrainsConcurrentCalls(t *testing.T) {
	entered := make(chan struct{})
	drained := make(chan struct{})
	var writes atomic.Int32
	kernel := testQuickJS(t, HostFunc(func(ctx context.Context, module, operation string, _ map[string]any) (any, error) {
		if module == "models" {
			<-entered
			return nil, fatalTestError{errors.New("accounting failed")}
		}
		if operation == "read" {
			close(entered)
			<-ctx.Done()
			close(drained)
			return nil, ctx.Err()
		}
		writes.Add(1)
		return "write", nil
	}), nil)
	kernel.limits.MaxConcurrentHostCalls = 2
	value, err := kernel.Exec(t.Context(), Cell{Code: `var a=models.call({prompt:"work"});var b=files.read({path:"ongoing"});try{await Promise.all([a,b]);}catch(e){} await files.write({path:"later",content:"forbidden"});`})
	if !fatalHostError(err) || value.Settled || writes.Load() != 0 {
		t.Fatalf("result=%+v error=%v writes=%d", value, err, writes.Load())
	}
	select {
	case <-drained:
	default:
		t.Fatal("returned before started host work drained")
	}
}

func TestQuickJSFatalHostFailureStopsQueuedDispatch(t *testing.T) {
	entered := make(chan struct{})
	queued := make(chan struct{})
	var effects atomic.Int32
	kernel := testQuickJS(t, HostFunc(func(_ context.Context, module, _ string, _ map[string]any) (any, error) {
		if module == "models" {
			close(entered)
			<-queued
			return nil, fatalTestError{errors.New("accounting failed")}
		}
		effects.Add(1)
		return "effect", nil
	}), nil)
	kernel.limits.MaxConcurrentHostCalls = 1
	kernel.observeHost = func(call HostCall, _ map[string]any) func(HostCall, any) {
		if call.Module == "files" {
			<-entered
			close(queued)
		}
		return nil
	}
	// Hold the second request's observer until the first call owns admission.
	// Releasing the first call now cancels the already-received queued request.
	done := make(chan error, 1)
	go func() {
		_, err := kernel.Exec(t.Context(), Cell{Code: `var pending=models.call({prompt:"work"}); var later=files.write({path:"queued",content:"forbidden"}); await Promise.all([pending,later]);`})
		done <- err
	}()
	select {
	case err := <-done:
		if !fatalHostError(err) || effects.Load() != 0 {
			t.Fatalf("queued effect ran: %v count=%d", err, effects.Load())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("fatal queue did not drain")
	}
}

func TestQuickJSHostWaitHasNoWholeCellDeadlineAndComputeRemainsBounded(t *testing.T) {
	clock := computeTimer{last: time.Unix(0, 0)}
	now := clock.last.Add(10 * time.Millisecond)
	if clock.advance(now, false) != 10*time.Millisecond {
		t.Fatal("initial compute not charged")
	}
	now = now.Add(11 * time.Minute)
	if clock.advance(now, true) != 10*time.Millisecond {
		t.Fatal("host wait consumed guest budget")
	}
	if clock.advance(now.Add(time.Second), false) != time.Second+10*time.Millisecond {
		t.Fatal("post-host compute not charged")
	}
	kernel := testQuickJS(t, HostFunc(func(ctx context.Context, _, _ string, _ map[string]any) (any, error) {
		if _, ok := ctx.Deadline(); ok {
			return nil, errors.New("host inherited whole-cell deadline")
		}
		return "ready", nil
	}), nil)
	value, err := kernel.Exec(t.Context(), Cell{Code: `await models.call({prompt:"bounded per-attempt work"});`})
	if err != nil || !value.Settled {
		t.Fatalf("host wait: %+v %v", value, err)
	}
	kernel.limits.Wall = 50 * time.Millisecond
	if _, err := kernel.Exec(t.Context(), Cell{Code: `while(true){}`}); err == nil || (!strings.Contains(err.Error(), "compute") && !strings.Contains(err.Error(), "deadline")) {
		t.Fatalf("busy guest did not hit compute deadline: %v", err)
	}
}
