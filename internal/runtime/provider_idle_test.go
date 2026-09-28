package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestProviderIdleSettlesUncertaintyWithoutRedispatch(t *testing.T) {
	for _, knownUsage := range []bool{false, true} {
		name := "unknown usage"
		if knownUsage {
			name = "reported usage"
		}
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				_, _ = io.Copy(io.Discard, request.Body)
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if knownUsage {
					_, _ = io.WriteString(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":5,\"cost\":0.25}}\n\n")
				}
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"provisional\"}}]}\n\n")
				_ = http.NewResponseController(w).Flush()
				<-request.Context().Done()
			}))
			t.Cleanup(server.Close)
			provider := model.OpenAI{IdleTimeout: 100 * time.Millisecond, Resolve: func(context.Context, session.ModelSelection) (model.Route, error) {
				return model.Route{URL: server.URL, MaxOutputTokens: 100, TimeoutMillis: 3000, MaxAttempts: 3}, nil
			}}
			r := openTest(t, t.TempDir(), provider)
			owner := createTest(t, r)
			if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "fixture", Name: "fixture-model"}}); err != nil {
				t.Fatal(err)
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "idle")
			admission := waitTest(t, r, "idle", terminal)
			if admission.Turn.State != session.Failed || admission.Turn.Failure == nil || !strings.Contains(*admission.Turn.Failure, "stalled") {
				t.Fatalf("turn=%+v", admission.Turn)
			}
			attempts, err := r.ModelAttempts(t.Context(), admission.Turn.ID, "", 100)
			if err != nil || len(attempts) != 1 || calls.Load() != 1 {
				t.Fatalf("idle replayed despite uncertainty: calls=%d attempts=%+v err=%v", calls.Load(), attempts, err)
			}
			attempt := attempts[0]
			if attempt.State != session.AttemptUncertain || attempt.DispatchedAt == nil || attempt.MessageID != nil || attempt.Result == nil || attempt.Result.ElapsedMillis == nil {
				t.Fatalf("lost dispatched uncertainty evidence: %+v", attempt)
			}
			if knownUsage {
				if attempt.Result.Usage.Input == nil || *attempt.Result.Usage.Input != 20 || attempt.Result.Usage.Output == nil || *attempt.Result.Usage.Output != 5 || attempt.CostNanoUSD == nil || *attempt.CostNanoUSD != 250000000 {
					t.Fatalf("known accounting lost at stall: %+v", attempt)
				}
			} else if attempt.Result.Usage.Input != nil || attempt.Result.Usage.Output != nil || attempt.CostNanoUSD != nil {
				t.Fatalf("unknown accounting became known zero: %+v", attempt)
			}
			history, err := r.History(t.Context(), owner.ID, 0, 100)
			if err != nil || len(history) != 1 || history[0].Role != session.User {
				t.Fatalf("partial output entered transcript: %+v %v", history, err)
			}
			// A lost acknowledgement retry is the same failed input, not another
			// attempt even though the prepared provider policy allowed three.
			retry := submitTest(t, r, owner.ID, "idle")
			if retry.Turn == nil || retry.Turn.ID != admission.Turn.ID || retry.Turn.State != session.Failed {
				t.Fatalf("input retry changed the idle outcome: %+v", retry)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatalf("idle response was redispatched: %d", calls.Load())
			}
		})
	}
}
