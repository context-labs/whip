package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// Report latency without making machine-dependent performance a correctness gate.
// Four root agents submit concurrently; the provider is fake, SQLite is WAL/NORMAL,
// and subscriptions retain the production 50 ms polling interval.
func TestV2ConcurrentAdmissionAndEventLatency(t *testing.T) {
	fixture := newV2Fixture(t, &fakeRunner{})
	const agents, rounds = 4, 12
	roots := make([]string, agents)
	clients := make([]*Client, agents)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	for i := range agents {
		roots[i] = createRoot(t, fixture.store)
		transport := "unix"
		if i%2 == 1 {
			transport = "websocket"
		}
		clients[i] = fixture.dial(transport, fmt.Sprintf("latency-%d", i))
		if _, err := clients[i].Subscribe(ctx, roots[i], 0); err != nil {
			t.Fatal(err)
		}
	}
	type sample struct{ admission, completion time.Duration }
	samples := make(chan sample, agents*rounds)
	failures := make(chan error, agents)
	var workers sync.WaitGroup
	for i := range agents {
		workers.Go(func() {
			for n := range rounds {
				start := time.Now()
				receipt, err := clients[i].Submit(ctx, CommandParams{CommandID: fmt.Sprintf("turn-%d", n), Scope: "root", RootID: roots[i], Operation: "submit", Payload: json.RawMessage(`{"text":"measure"}`)})
				if err != nil {
					failures <- err
					return
				}
				admission := time.Since(start)
				if receipt.IngressSeq <= 0 {
					failures <- errors.New("missing durable admission")
					return
				}
				for {
					select {
					case event := <-clients[i].Events():
						if event.Kind == "turn.succeeded" {
							samples <- sample{admission: admission, completion: time.Since(start)}
							goto next
						}
						if event.Kind == "turn.failed" {
							failures <- errors.New("measurement turn failed")
							return
						}
					case <-ctx.Done():
						failures <- ctx.Err()
						return
					}
				}
			next:
			}
		})
	}
	workers.Wait()
	close(samples)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	admissions, completions := []time.Duration{}, []time.Duration{}
	for sample := range samples {
		admissions = append(admissions, sample.admission)
		completions = append(completions, sample.completion)
	}
	if len(admissions) != agents*rounds {
		t.Fatalf("received %d/%d measurements", len(admissions), agents*rounds)
	}
	slices.Sort(admissions)
	slices.Sort(completions)
	t.Logf("4 concurrent agents / 48 commands / mixed Unix + WebSocket: acceptance p50=%s p95=%s; submit-to-completion-event p50=%s p95=%s (includes fake execution and polling)", admissions[len(admissions)/2], admissions[len(admissions)*95/100], completions[len(completions)/2], completions[len(completions)*95/100])
}
