//go:build integration

package rlm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// TestPackagedRuntime executes the supplied distribution, never the test binary.
// The release runner binds this report to the signed package's manifest.
func TestPackagedRuntime(t *testing.T) {
	executable := os.Getenv("WHIP_RLM_TEST_EXECUTABLE")
	if !filepath.IsAbs(executable) {
		t.Fatal("WHIP_RLM_TEST_EXECUTABLE must name an absolute packaged executable")
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	type observation struct {
		Descriptor    EngineDescriptor `json:"descriptor"`
		Checks        []string         `json:"checks"`
		StartupMillis float64          `json:"startupMillis"`
		CellMillis    float64          `json:"cellMillis"`
		ResidentBytes uint64           `json:"residentBytes"`
	}
	report := struct {
		SHA256       string                 `json:"sha256"`
		Architecture string                 `json:"architecture"`
		Engines      map[string]observation `json:"engines"`
	}{hex.EncodeToString(digest[:]), runtime.GOARCH, make(map[string]observation)}
	for _, engine := range []string{EngineStarlark, EngineQuickJS} {
		t.Run(engine, func(t *testing.T) {
			var calls atomic.Int32
			kernel, err := NewKernel(KernelOptions{
				Command: []string{executable, "_kernel"}, Engine: engine,
				Scratch: &memoryScratch{}, Checkpoints: &memoryCheckpoints{},
				Host: HostFunc(func(_ context.Context, module, operation string, args map[string]any) (any, error) {
					if module != "state" || operation != "private_get" || args["key"] != "answer" {
						return nil, fmt.Errorf("unexpected host call: %s.%s", module, operation)
					}
					calls.Add(1)
					return 40, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(kernel.Close)
			started := time.Now()
			if err := kernel.Start(); err != nil {
				t.Fatal(err)
			}
			observed := observation{Descriptor: kernel.Describe(), StartupMillis: float64(time.Since(started).Microseconds()) / 1000}
			code := "answer = state.private_get(key='answer')\nprint('runtime-ready')\nanswer + 2"
			if engine == EngineQuickJS {
				code = `var answer = await state.private_get({key:"answer"}); print("runtime-ready"); answer + 2`
			}
			started = time.Now()
			result, err := kernel.Exec(t.Context(), code)
			observed.CellMillis = float64(time.Since(started).Microseconds()) / 1000
			if err != nil || !result.HasValue || fmt.Sprint(result.Value) != "42" || result.Output != "runtime-ready\n" || calls.Load() != 1 {
				t.Fatalf("execution: result=%+v calls=%d error=%v", result, calls.Load(), err)
			}
			kernel.mu.Lock()
			observed.ResidentBytes, err = residentBytes(kernel.worker.command.Process.Pid)
			kernel.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			code = "answer += 2\nanswer"
			if engine == EngineQuickJS {
				code = "answer += 2; answer"
			}
			result, err = kernel.Exec(t.Context(), code)
			if err != nil || fmt.Sprint(result.Value) != "42" {
				t.Fatalf("persistent state: %+v %v", result, err)
			}
			if err := kernel.Suspend(); err != nil {
				t.Fatal(err)
			}
			result, err = kernel.Exec(t.Context(), "answer")
			if err != nil || fmt.Sprint(result.Value) != "42" || result.Restored == nil || calls.Load() != 1 {
				t.Fatalf("checkpoint restore: %+v calls=%d error=%v", result, calls.Load(), err)
			}
			observed.Checks = []string{"execution", "output", "host-call", "persistent-state", "checkpoint-restore"}
			if engine == EngineQuickJS {
				ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
				_, err := kernel.Exec(ctx, "answer = 99; for (;;) {}")
				cancel()
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("cancellation: %v", err)
				}
				result, err = kernel.Exec(t.Context(), "answer")
				if err != nil || fmt.Sprint(result.Value) != "42" || result.Restored == nil {
					t.Fatalf("recovery: %+v %v", result, err)
				}
				observed.Checks = append(observed.Checks, "cancellation", "recovery")
			}
			report.Engines[engine] = observed
		})
	}
	if t.Failed() {
		return
	}
	data, err = json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if filename := os.Getenv("WHIP_RLM_TEST_REPORT"); filename != "" {
		if err := os.WriteFile(filename, append(data, '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Log(string(data))
}
