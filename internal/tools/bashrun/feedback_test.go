package bashrun

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestOnUpdateThrottle: a command emitting output in stages fires OnUpdate
// with monotonically growing snapshots, and the throttle keeps consecutive
// calls at least ~updateInterval apart (pi's bash onUpdate is 100ms).
func TestOnUpdateThrottle(t *testing.T) {
	var mu sync.Mutex
	var snaps []string
	var times []time.Time

	started := time.Now()
	res := Run(context.Background(), Options{
		Command: `echo stage1; sleep 0.35; echo stage2; sleep 0.35; echo stage3`,
		Timeout: 10 * time.Second,
		OnUpdate: func(soFar string) {
			mu.Lock()
			snaps = append(snaps, soFar)
			times = append(times, time.Now())
			mu.Unlock()
		},
	})
	elapsed := time.Since(started)

	mu.Lock()
	defer mu.Unlock()

	if len(snaps) < 2 {
		t.Fatalf("expected ≥2 partial snapshots over ~0.7s of staged output, got %d: %v", len(snaps), snaps)
	}
	// Snapshots are cumulative: each is a prefix-extension of the last, and
	// the union covers what the command printed before the final Result.
	for i := 1; i < len(snaps); i++ {
		if !strings.HasPrefix(snaps[i], snaps[i-1]) && !strings.HasPrefix(snaps[i-1], snaps[i]) {
			t.Errorf("snapshot %d is not a prefix-extension of %d:\n%q\nvs\n%q", i, i-1, snaps[i], snaps[i-1])
		}
	}
	joined := strings.Join(snaps, "")
	if !strings.Contains(joined, "stage1") {
		t.Errorf("no snapshot contained the first stage's output: %v", snaps)
	}
	// A delayed ticker delivery can make adjacent callbacks less than one
	// interval apart. Bound the average rate instead.
	if maxCalls := int(elapsed/updateInterval) + 1; len(times) > maxCalls {
		t.Errorf("OnUpdate fired %d times over %s; want at most %d", len(times), elapsed, maxCalls)
	}
	// The final Result still carries the complete output (OnUpdate never
	// claims to deliver the end state).
	if !strings.Contains(res.Output, "stage3") {
		t.Errorf("final result missing last stage: %q", res.Output)
	}
}

// TestOnUpdateNil: no callback, no panic, output intact (the default path).
func TestOnUpdateNil(t *testing.T) {
	res := Run(context.Background(), Options{Command: `echo plain`})
	if strings.TrimSpace(res.Output) != "plain" {
		t.Fatalf("output wrong without OnUpdate: %q", res.Output)
	}
}

// TestOnUpdateFastCommand: a command that exits before the first tick may
// legitimately fire zero times; what matters is the run doesn't stall.
func TestOnUpdateFastCommand(t *testing.T) {
	fired := make(chan struct{}, 8)
	done := make(chan Result, 1)
	go func() {
		done <- Run(context.Background(), Options{
			Command:  `echo fast`,
			OnUpdate: func(string) { fired <- struct{}{} },
		})
	}()
	select {
	case res := <-done:
		if !strings.Contains(res.Output, "fast") {
			t.Fatalf("output wrong: %q", res.Output)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fast command with OnUpdate stalled")
	}
}

// TestSpill: the full output lands in a temp file the model can read back.
func TestSpill(t *testing.T) {
	full := "HEAD\n" + strings.Repeat("x", 100) + "\nTAIL\n"
	path := Spill(full)
	if path == "" {
		t.Fatal("Spill returned no path")
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file unreadable: %v", err)
	}
	if string(data) != full {
		t.Fatalf("spill file content mismatch: got %d bytes, want %d", len(data), len(full))
	}
	// Private to the user — the output may carry secrets.
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("spill file perms wrong: %v %v", fi, err)
	}
}
