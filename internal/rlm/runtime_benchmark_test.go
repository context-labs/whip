package rlm

import (
	"context"
	"os"
	"testing"
	"time"
)

func benchmarkKernel(b *testing.B, engineID string, host Host, store CheckpointStore, concurrency int) *Kernel {
	b.Helper()
	executable, err := os.Executable()
	if err != nil {
		b.Fatal(err)
	}
	kernel, err := NewKernel(KernelOptions{Command: []string{executable, "-test.run=^TestWorkerProcess$", "--"}, Engine: engineID, Host: host, Checkpoints: store, Limits: Limits{MaxConcurrentHostCalls: concurrency}})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(kernel.Close)
	return kernel
}

func BenchmarkRuntimeWarmCell(b *testing.B) {
	workloads := []struct {
		name, starlark, javascript string
		checkpoint                 bool
		delay                      time.Duration
		concurrency                int
		onlyQuickJS                bool
	}{
		{name: "arithmetic", starlark: "total=0\nfor i in range(1000):\n total+=i\ntotal", javascript: "var total=0; for(var i=0;i<1000;i++) total+=i; total"},
		{name: "host_call", starlark: `files.read(path="fixture")`, javascript: `await files.read({path:"fixture"})`},
		{name: "host_fanout_8_matched1", starlark: `[files.read(path=str(i)) for i in range(8)]`, javascript: `await Promise.all(Array.from({length:8},(_,i)=>files.read({path:String(i)})))`, delay: time.Millisecond},
		{name: "host_fanout_8_native_async16", starlark: "", javascript: `await Promise.all(Array.from({length:8},(_,i)=>files.read({path:String(i)})))`, delay: time.Millisecond, concurrency: 16, onlyQuickJS: true},
		{name: "checkpoint", starlark: "counter+=1\ncounter", javascript: "counter+=1; counter", checkpoint: true},
	}
	for _, workload := range workloads {
		for _, engineID := range []string{EngineStarlark, EngineQuickJS} {
			if workload.onlyQuickJS && engineID != EngineQuickJS {
				continue
			}
			b.Run(workload.name+"/"+engineID, func(b *testing.B) {
				var store CheckpointStore
				if workload.checkpoint {
					store = &memoryCheckpoints{}
				}
				host := HostFunc(func(ctx context.Context, _, _ string, _ map[string]any) (any, error) {
					if workload.delay > 0 {
						select {
						case <-time.After(workload.delay):
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					}
					return map[string]any{"ok": true, "bytes": 42}, nil
				})
				kernel := benchmarkKernel(b, engineID, host, store, max(workload.concurrency, 1))
				setup := "counter=0"
				code := workload.starlark
				if engineID == EngineQuickJS {
					setup = "var counter=0"
					code = workload.javascript
				}
				if _, err := kernel.Exec(b.Context(), setup); err != nil {
					b.Fatal(err)
				}
				b.ResetTimer()
				var result Result
				for b.Loop() {
					var err error
					result, err = kernel.Exec(b.Context(), code)
					if err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				for name, value := range result.Metrics {
					b.ReportMetric(float64(value), name+"/op")
				}
				if concrete, ok := store.(*memoryCheckpoints); ok && concrete.value != nil {
					b.ReportMetric(float64(len(concrete.value.Data)), "image-bytes")
				}
			})
		}
	}
}

func BenchmarkRuntimeCheckpointRestore(b *testing.B) {
	for _, engineID := range []string{EngineStarlark, EngineQuickJS} {
		b.Run(engineID, func(b *testing.B) {
			store := &memoryCheckpoints{}
			kernel := benchmarkKernel(b, engineID, nil, store, 1)
			setup := "counter=1"
			if engineID == EngineQuickJS {
				setup = "var counter=1"
			}
			if _, err := kernel.Exec(b.Context(), setup); err != nil {
				b.Fatal(err)
			}
			b.ResetTimer()
			for b.Loop() {
				if err := kernel.Suspend(); err != nil {
					b.Fatal(err)
				}
				if _, err := kernel.Exec(b.Context(), "counter+1"); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(len(store.value.Data)), "image-bytes")
		})
	}
}

// Cold start measures construction through the first expression. Process cleanup
// runs outside the timer, and no image is loaded or published.
func BenchmarkRuntimeColdStart(b *testing.B) {
	for _, engineID := range []string{EngineStarlark, EngineQuickJS} {
		b.Run(engineID, func(b *testing.B) {
			for b.Loop() {
				kernel := benchmarkKernel(b, engineID, nil, nil, 1)
				if _, err := kernel.Exec(b.Context(), "6*7"); err != nil {
					b.Fatal(err)
				}
				b.StopTimer()
				kernel.Close()
				b.StartTimer()
			}
		})
	}
}
