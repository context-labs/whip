package process

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCellSettledIncludesLanguageErrors(t *testing.T) {
	for _, engine := range []string{EngineStarlark, EngineQuickJS} {
		t.Run(engine, func(t *testing.T) {
			kernel := testModulesKernel(t, engine, nil, nil)
			failure := `fail("language failure")`
			if engine == EngineQuickJS {
				failure = `throw new Error("language failure")`
			}
			result, err := kernel.Exec(t.Context(), Cell{Code: "42"})
			if err != nil || !result.Settled {
				t.Fatalf("successful final result: %+v, %v", result, err)
			}
			result, err = kernel.Exec(t.Context(), Cell{Code: failure})
			if err == nil || !strings.Contains(err.Error(), "language failure") || !result.Settled {
				t.Fatalf("language error final result: %+v, %v", result, err)
			}
		})
	}
}

func TestCellWithoutFinalResultIsUnsettled(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, engine := range []string{EngineStarlark, EngineQuickJS} {
		for _, test := range []struct {
			name string
			args []string
		}{
			{"transport", []string{"-test-exit", "execution", "nonzero"}},
			{"mismatched result", []string{"-test-protocol-response", "mismatch"}},
			{"unexpected frame", []string{"-test-protocol-response", "unexpected"}},
		} {
			t.Run(engine+"/"+test.name, func(t *testing.T) {
				command := append([]string{executable, "-test.run=TestWorkerProcess", "--"}, test.args...)
				kernel, err := NewKernel(KernelOptions{Engine: engine, Command: command})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(kernel.Close)
				result, err := kernel.Exec(t.Context(), Cell{Code: "42"})
				if err == nil || result.Settled {
					t.Fatalf("accepted a missing or invalid final result: %+v, %v", result, err)
				}
			})
		}
	}
}

func TestCellCancellationAndTimeoutAreUnsettled(t *testing.T) {
	for _, engine := range []string{EngineStarlark, EngineQuickJS} {
		for _, timeout := range []bool{false, true} {
			name := "cancellation"
			if timeout {
				name = "timeout"
			}
			t.Run(engine+"/"+name, func(t *testing.T) {
				entered := make(chan struct{})
				kernel := testModulesKernel(t, engine, nil, HostFunc(func(context.Context, string, string, map[string]any) (any, error) {
					close(entered)
					return "ok", nil
				}))
				kernel.limits.Steps = ^uint64(0)
				turnCtx, _, release, err := kernel.AcquireTurn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer release()
				ctx, cancel := context.WithCancel(turnCtx)
				if timeout {
					cancel()
					ctx, cancel = context.WithTimeout(turnCtx, time.Second)
				}
				defer cancel()
				code := "files.read(path='begin')\nfor i in range(1000000000):\n    pass"
				if engine == EngineQuickJS {
					code = "await files.read({path:'begin'}); for (;;) {}"
				}
				type outcome struct {
					result Result
					err    error
				}
				done := make(chan outcome, 1)
				go func() {
					result, err := kernel.Exec(ctx, Cell{Code: code})
					done <- outcome{result, err}
				}()
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("cell did not reach its host call")
				}
				want := context.DeadlineExceeded
				if !timeout {
					want = context.Canceled
					cancel()
				}
				select {
				case got := <-done:
					if !errors.Is(got.err, want) || got.result.Settled {
						t.Fatalf("interrupted cell result: %+v, %v", got.result, got.err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("interrupted cell did not return")
				}
			})
		}
	}
}

func TestQuickJSUnsettledCellWaitsForLateHostCompletion(t *testing.T) {
	entered, finishHost := make(chan struct{}), make(chan struct{})
	kernel := testModulesKernel(t, EngineQuickJS, nil, HostFunc(func(ctx context.Context, _, _ string, _ map[string]any) (any, error) {
		close(entered)
		<-ctx.Done()
		<-finishHost
		ReportHostOperation(ctx, "late-effect")
		return "completed effect", nil
	}))
	completed := make(chan HostCall, 1)
	kernel.observeHost = func(HostCall, map[string]any) func(HostCall, any) {
		return func(call HostCall, _ any) { completed <- call }
	}
	turnCtx, _, release, err := kernel.AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	workerDone := kernel.worker.done
	ctx, cancel := context.WithCancel(turnCtx)
	unblock := sync.OnceFunc(func() { close(finishHost) })
	defer func() { cancel(); unblock(); release() }()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := kernel.Exec(ctx, Cell{Code: `await files.read({path:"late"})`})
		done <- outcome{result, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("host did not start")
	}
	cancel()
	select {
	case <-workerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled worker did not exit")
	}
	select {
	case <-done:
		t.Fatal("execution returned before its host completed")
	default:
	}
	unblock()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) || got.result.Settled {
			t.Fatalf("late host completion settled the cancelled cell: %+v, %v", got.result, got.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("execution did not drain the late host completion")
	}
	select {
	case call := <-completed:
		if call.Status != "completed" || call.OperationID != "late-effect" {
			t.Fatalf("lost successful late effect: %+v", call)
		}
	default:
		t.Fatal("execution returned before publishing the late host completion")
	}
}
