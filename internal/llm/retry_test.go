package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// noSleep swaps the backoff sleep for a no-op so retry tests run instantly.
func noSleep(t *testing.T) {
	t.Helper()
	orig := sleep
	sleep = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { sleep = orig })
}

func TestRetryableClassification(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{context.Canceled, false},
		{context.DeadlineExceeded, false},
		{&HTTPError{Status: "400 Bad Request", Body: "bad"}, false},
		{&HTTPError{Status: "401 Unauthorized", Body: "nope"}, false},
		{&HTTPError{Status: "403 Forbidden", Body: "nope"}, false},
		{&HTTPError{Status: "429 Too Many Requests", Body: "slow down"}, true},
		{&HTTPError{Status: "500 Internal Server Error", Body: "boom"}, true},
		{&HTTPError{Status: "524", Body: "origin timeout"}, true},
		{errors.New("dial tcp: connection refused"), true},
		{io.ErrUnexpectedEOF, true},
	}
	for _, c := range cases {
		if got := retryable(c.err); got != c.want {
			t.Errorf("retryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// A transient 524 then a good stream must succeed without the caller seeing
// the failure (and the OnRetry hook must fire with a delay).
func TestStreamRetriesTransientStatus(t *testing.T) {
	noSleep(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(524)
			fmt.Fprint(w, "error code: 524")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	var retries []RetryEvent
	c.OnRetry = func(ev RetryEvent) { retries = append(retries, ev) }

	msg, _, err := c.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "ok" {
		t.Fatalf("content: %q", msg.Content)
	}
	if calls.Load() != 3 {
		t.Fatalf("attempts: %d, want 3", calls.Load())
	}
	if len(retries) != 2 {
		t.Fatalf("OnRetry fired %d times, want 2", len(retries))
	}
	if retries[0].Attempt != 1 || retries[0].Err == nil {
		t.Fatalf("retry event: %+v", retries[0])
	}
}

// A recovered 520 must retain the failed attempt's missing usage separately.
func TestStreamRetries520PreservingUnknownAttemptUsage(t *testing.T) {
	noSleep(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(520)
			fmt.Fprint(w, "error code: 520")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],`+
			`"usage":{"prompt_tokens":4,"completion_tokens":2}}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(server.Close)
	client := New(server.URL, "test")
	var attempts []ModelAttempt
	var settled []ModelAttemptResult
	request := Request{Model: "fixture", Accounting: testCallAccounting(
		func(_ context.Context, attempt ModelAttempt) (func(ModelAttemptResult) error, error) {
			attempts = append(attempts, attempt)
			return func(result ModelAttemptResult) error {
				settled = append(settled, result)
				return nil
			}, nil
		},
	)}
	var retries []RetryEvent
	client.OnRetry = func(event RetryEvent) { retries = append(retries, event) }
	message, _, err := client.Stream(t.Context(), request, nil, nil, nil)
	if err != nil || message.Content != "ok" {
		t.Fatalf("message=%+v err=%v", message, err)
	}
	if calls.Load() != 2 || len(attempts) != 2 || len(settled) != 2 || len(retries) != 1 {
		t.Fatalf("calls=%d attempts=%+v settled=%+v retries=%+v", calls.Load(), attempts, settled, retries)
	}
	if attempts[0].LogicalID == "" || attempts[0].LogicalID != attempts[1].LogicalID ||
		attempts[0].Number != 1 || attempts[1].Number != 2 {
		t.Fatalf("attempt identities=%+v", attempts)
	}
	if !settled[0].Dispatched || !settled[0].Failed || settled[0].Usage.Reported {
		t.Fatalf("failed request must retain unknown usage: %+v", settled[0])
	}
	if !settled[1].Dispatched || settled[1].Failed || !settled[1].Usage.Reported ||
		settled[1].Usage.PromptTokens != 4 || settled[1].Usage.CompletionTokens != 2 {
		t.Fatalf("successful retry usage=%+v", settled[1])
	}
	if retries[0].Max != DefaultMaxAttempts {
		t.Fatalf("retry limit=%d, want %d", retries[0].Max, DefaultMaxAttempts)
	}
}

// Transport errors (connection refused) are retryable too.
func TestStreamRetriesTransportError(t *testing.T) {
	noSleep(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // now refuses connections

	c := New(url, "k")
	var retried int
	c.OnRetry = func(RetryEvent) { retried++ }

	_, _, err := c.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error after retries exhausted")
	}
	if retried != DefaultMaxAttempts-1 {
		t.Fatalf("retried %d times, want %d", retried, DefaultMaxAttempts-1)
	}
}

// A permanent error (401) must surface on the first attempt, no retries.
func TestStreamDoesNotRetryPermanentStatus(t *testing.T) {
	noSleep(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, _, err := New(srv.URL, "k").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("attempts: %d, want 1 (no retry on 401)", calls.Load())
	}
}

// Context-limit errors must NOT be retried — the agent's compaction path
// depends on seeing them immediately.
func TestStreamDoesNotRetryContextLimit(t *testing.T) {
	noSleep(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"code":"context_length_exceeded"}}`)
	}))
	defer srv.Close()

	_, _, err := New(srv.URL, "k").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if !IsContextLimit(err) {
		t.Fatalf("expected context-limit error, got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("attempts: %d, want 1", calls.Load())
	}
}

// Once visible text has streamed, a mid-stream failure must surface rather
// than retry — a retry would replay the already-rendered text.
func TestStreamDoesNotRetryAfterEmission(t *testing.T) {
	noSleep(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		// hang up without [DONE]: scanner hits EOF and Stream treats it as a
		// clean end... so instead force an error chunk after the text.
		fmt.Fprint(w, "data: {\"error\":{\"message\":\"stream died\"}}\n\n")
	}))
	defer srv.Close()

	var streamed strings.Builder
	c := New(srv.URL, "k")
	var retried int
	c.OnRetry = func(RetryEvent) { retried++ }
	_, _, err := c.Stream(context.Background(), Request{Model: "m"},
		func(d string) { streamed.WriteString(d) }, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if retried != 0 {
		t.Fatalf("retried %d times after emission, want 0", retried)
	}
	if streamed.String() != "partial" {
		t.Fatalf("streamed: %q", streamed.String())
	}
}

// MaxRetries overrides the default budget; 1 means a single attempt.
func TestMaxRetriesConfigurable(t *testing.T) {
	noSleep(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	c.MaxRetries = 2
	if _, _, err := c.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 2 {
		t.Fatalf("attempts: %d, want 2 (MaxRetries=2)", calls.Load())
	}

	calls.Store(0)
	c.MaxRetries = 1
	if _, _, err := c.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("attempts: %d, want 1 (MaxRetries=1 disables retries)", calls.Load())
	}
}

// The OnRetry event carries the configured max so the UI can show N/M.
func TestRetryEventCarriesMax(t *testing.T) {
	noSleep(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	c.MaxRetries = 3
	var evs []RetryEvent
	c.OnRetry = func(ev RetryEvent) { evs = append(evs, ev) }
	_, _, _ = c.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if len(evs) != 2 {
		t.Fatalf("events: %d, want 2", len(evs))
	}
	if evs[0].Max != 3 {
		t.Fatalf("event Max: %d, want 3", evs[0].Max)
	}
}

// Complete retries transient statuses the same way Stream does.
func TestCompleteRetriesTransientStatus(t *testing.T) {
	noSleep(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, "rate limited")
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"done"}}]}`)
	}))
	defer srv.Close()

	got, _, err := New(srv.URL, "k").Complete(context.Background(), Request{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "done" {
		t.Fatalf("complete: %q", got)
	}
}

// Caller cancellation during backoff must abort the retry loop promptly.
func TestRetryRespectsCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()

	start := time.Now()
	_, _, err := New(srv.URL, "k").Stream(ctx, Request{Model: "m"}, nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("cancellation took %v; backoff should have been interrupted", elapsed)
	}
}

// A provider that rejects or stalls the stream before any delta (Inference.net
// reports a 30 s no-token gateway stall as an SSE error chunk) gets exactly one
// repeat; the same failure after a delta, or twice in a row, surfaces unchanged.
func TestStreamRepeatsPreTokenProviderErrorOnce(t *testing.T) {
	noSleep(t)
	stall := "data: {\"error\":{\"message\":\"Inference stream timed out: No next token received for 30000ms\"}}\n\n"
	ok := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"
	partial := "data: {\"choices\":[{\"delta\":{\"content\":\"par\"}}]}\n\n" + stall
	cases := []struct {
		name      string
		responses []string
		wantCalls int
		wantErr   string
		wantText  string
	}{
		{"stall then success", []string{stall, ok}, 2, "", "ok"},
		{"closed before any delta then success", []string{"", ok}, 2, "", "ok"},
		{"two stalls", []string{stall, stall, ok}, 2, "No next token", ""},
		{"stall after a delta", []string{partial, ok}, 1, "No next token", "par"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				body := tc.responses[min(calls, len(tc.responses)-1)]
				calls++
				w.Write([]byte(body))
			}))
			defer srv.Close()
			msg, _, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
			if tc.wantErr == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("want error containing %q, got %v", tc.wantErr, err)
			}
			if msg.Content != tc.wantText {
				t.Fatalf("content %q, want %q", msg.Content, tc.wantText)
			}
			if calls != tc.wantCalls {
				t.Fatalf("provider called %d times, want %d", calls, tc.wantCalls)
			}
		})
	}
}
