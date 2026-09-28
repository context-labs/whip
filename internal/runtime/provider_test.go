package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func openHTTPTest(t *testing.T, handler http.HandlerFunc) (*Runtime, session.Session) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	provider := model.OpenAI{Resolve: func(context.Context, session.ModelSelection) (model.ChatRoute, error) {
		return model.ChatRoute{URL: server.URL, MaxOutputTokens: 100, TimeoutMillis: 3000, MaxAttempts: 3}, nil
	}}
	r := openTest(t, t.TempDir(), provider)
	s := createTest(t, r)
	s, err := r.UpdateConfiguration(t.Context(), s.ID, s.ConfigRevision, session.ConfigPatch{
		Model: &session.ModelSelection{Provider: "fixture", Name: "fixture-model"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r, s
}

func TestProviderRetriesHaveDistinctDurableAttempts(t *testing.T) {
	var calls atomic.Int32
	r, s := openHTTPTest(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":"busy"}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"real HTTP response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"cost":0.001}}`)
	})
	submitTest(t, r, s.ID, "retry")
	admission := waitTest(t, r, "retry", terminal)
	attempts, err := r.ModelAttempts(t.Context(), admission.Turn.ID, "", 100)
	if err != nil || len(attempts) != 2 || calls.Load() != 2 || admission.Turn.State != session.Succeeded {
		t.Fatalf("attempts=%+v calls=%d turn=%+v err=%v", attempts, calls.Load(), admission.Turn, err)
	}
	first, second := attempts[0], attempts[1]
	if first.State != session.AttemptFailed || first.CostNanoUSD != nil || first.MessageID != nil || first.Number != 1 {
		t.Fatalf("rejected attempt: %+v", first)
	}
	if second.State != session.AttemptSucceeded || second.CostNanoUSD == nil || *second.CostNanoUSD != 1000000 || second.Number != 2 || second.LogicalID != first.LogicalID || second.Request.RequestDigest != first.Request.RequestDigest {
		t.Fatalf("retry evidence: %+v", second)
	}
	history, err := r.History(t.Context(), s.ID, 0, 100)
	if err != nil || len(history) != 2 || second.MessageID == nil || *second.MessageID != history[1].ID {
		t.Fatalf("retry duplicated transcript: %+v %v", history, err)
	}
}

func TestProviderTransportUncertaintyIsNotAutomaticallyRetried(t *testing.T) {
	var calls atomic.Int32
	r, s := openHTTPTest(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		connection, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	})
	submitTest(t, r, s.ID, "uncertain")
	admission := waitTest(t, r, "uncertain", terminal)
	attempts, err := r.ModelAttempts(t.Context(), admission.Turn.ID, "", 100)
	if err != nil || len(attempts) != 1 || calls.Load() != 1 || admission.Turn.State != session.Failed {
		t.Fatalf("attempts=%+v calls=%d turn=%+v err=%v", attempts, calls.Load(), admission.Turn, err)
	}
	if attempts[0].State != session.AttemptUncertain || attempts[0].CostNanoUSD != nil || attempts[0].Result.Usage.Input != nil {
		t.Fatalf("uncertainty became a known zero or terminal provider response: %+v", attempts[0])
	}
}

func TestSlowProviderDoesNotBlockDurableCancellation(t *testing.T) {
	entered := make(chan struct{})
	r, s := openHTTPTest(t, func(_ http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		close(entered)
		<-request.Context().Done()
	})
	submitTest(t, r, s.ID, "cancel")
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("request never entered provider")
	}
	a, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "cancel"})
	if err != nil || a.Turn == nil {
		t.Fatalf("admission=%+v err=%v", a, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := r.CancelTurn(ctx, a.Turn.ID); err != nil {
		t.Fatal(err)
	}
	a = waitTest(t, r, "cancel", terminal)
	attempts, err := r.ModelAttempts(t.Context(), a.Turn.ID, "", 100)
	if err != nil || len(attempts) != 1 || a.Turn.State != session.Cancelled || attempts[0].State != session.AttemptUncertain {
		t.Fatalf("turn=%+v attempts=%+v err=%v", a.Turn, attempts, err)
	}
}
