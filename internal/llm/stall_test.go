package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const okStream = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"

// stallServer answers each request with the handler for that call number; a
// handler that blocks must return when the request context ends, which is
// exactly what a stalled provider does once the client hangs up.
func stallServer(t *testing.T, handlers ...func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The server only watches for a client hang-up after the request body
		// is consumed; drain it so a blocking handler ends when the client does.
		_, _ = io.Copy(io.Discard, r.Body)
		n := int(calls.Add(1)) - 1
		handlers[min(n, len(handlers)-1)](w, r)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func flush(w http.ResponseWriter) {
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func headersThenSilence(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flush(w)
	<-r.Context().Done()
}

func silenceBeforeHeaders(_ http.ResponseWriter, r *http.Request) {
	<-r.Context().Done()
}

func deltaThenSilence(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
	flush(w)
	<-r.Context().Done()
}

func good(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, okStream)
}

func TestStallBeforeFirstDeltaIsRetried(t *testing.T) {
	noSleep(t)
	for name, first := range map[string]func(http.ResponseWriter, *http.Request){
		"after headers": headersThenSilence, "before headers": silenceBeforeHeaders,
	} {
		t.Run(name, func(t *testing.T) {
			server, calls := stallServer(t, first, good)
			client := New(server.URL, "test-key")
			client.StallTimeout = 50 * time.Millisecond
			var retried []RetryEvent
			client.OnRetry = func(event RetryEvent) { retried = append(retried, event) }
			msg, _, err := client.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
			if err != nil || msg.Content != "ok" {
				t.Fatalf("content=%q err=%v", msg.Content, err)
			}
			if calls.Load() != 2 || len(retried) != 1 {
				t.Fatalf("calls=%d retries=%d", calls.Load(), len(retried))
			}
			if _, ok := errors.AsType[stallError](retried[0].Err); !ok {
				t.Fatalf("retry cause %v, want a stall", retried[0].Err)
			}
			if errors.Is(retried[0].Err, context.Canceled) {
				t.Fatal("a stall must not look like a caller cancel")
			}
		})
	}
}

func TestSlowHealthyStreamIsNotStalled(t *testing.T) {
	server, _ := stallServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 8 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n")
			flush(w)
			time.Sleep(20 * time.Millisecond)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	client := New(server.URL, "test-key")
	client.StallTimeout = 100 * time.Millisecond
	msg, _, err := client.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err != nil || msg.Content != "aaaaaaaa" {
		t.Fatalf("content=%q err=%v", msg.Content, err)
	}
}

// Until regeneration lands, a stall after the first delta surfaces as a typed
// stall error with the partial preserved, and is not repeated.
func TestStallAfterDeltaSurfacesTypedError(t *testing.T) {
	noSleep(t)
	server, calls := stallServer(t, deltaThenSilence, good)
	client := New(server.URL, "test-key")
	client.StallTimeout = 50 * time.Millisecond
	msg, _, err := client.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if _, ok := errors.AsType[stallError](err); !ok || msg.Content != "partial" || calls.Load() != 1 {
		t.Fatalf("content=%q calls=%d err=%v", msg.Content, calls.Load(), err)
	}
}

func TestAttemptCeilingIsTypedAndRetriedBeforeFirstDelta(t *testing.T) {
	noSleep(t)
	// SSE comment lines keep the stall watchdog fed without emitting a delta,
	// so only the per-attempt ceiling can end the first attempt.
	keepalive := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flush(w)
		for {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(5 * time.Millisecond):
				fmt.Fprint(w, ": keepalive\n\n")
				flush(w)
			}
		}
	}
	server, calls := stallServer(t, keepalive, good)
	client := New(server.URL, "test-key")
	client.StallTimeout = time.Second
	client.AttemptCeiling = 60 * time.Millisecond
	var retried []RetryEvent
	client.OnRetry = func(event RetryEvent) { retried = append(retried, event) }
	msg, _, err := client.Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err != nil || msg.Content != "ok" || calls.Load() != 2 || len(retried) != 1 {
		t.Fatalf("content=%q calls=%d retries=%d err=%v", msg.Content, calls.Load(), len(retried), err)
	}
	if _, ok := errors.AsType[ceilingError](retried[0].Err); !ok || !errors.Is(retried[0].Err, context.DeadlineExceeded) {
		t.Fatalf("retry cause %v, want the attempt ceiling", retried[0].Err)
	}
}

func TestCallerCancelAndDeadlineAreNeverRetried(t *testing.T) {
	noSleep(t)
	t.Run("deadline", func(t *testing.T) {
		server, calls := stallServer(t, headersThenSilence, good)
		client := New(server.URL, "test-key")
		client.StallTimeout = time.Second
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, _, err := client.Stream(ctx, Request{Model: "m"}, nil, nil, nil)
		if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
			t.Fatalf("calls=%d err=%v", calls.Load(), err)
		}
		if _, ok := errors.AsType[ceilingError](err); ok {
			t.Fatalf("a caller deadline was reported as whip's ceiling: %v", err)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		server, calls := stallServer(t, headersThenSilence, good)
		client := New(server.URL, "test-key")
		client.StallTimeout = time.Second
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(30*time.Millisecond, cancel)
		_, _, err := client.Stream(ctx, Request{Model: "m"}, nil, nil, nil)
		if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
			t.Fatalf("calls=%d err=%v", calls.Load(), err)
		}
	})
}

func TestCompleteStallIsRetried(t *testing.T) {
	noSleep(t)
	server, calls := stallServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		flush(w)
		<-r.Context().Done()
	}, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"summary"}}]}`)
	})
	client := New(server.URL, "test-key")
	client.StallTimeout = 50 * time.Millisecond
	text, _, err := client.Complete(context.Background(), Request{Model: "m"})
	if err != nil || text != "summary" || calls.Load() != 2 {
		t.Fatalf("text=%q calls=%d err=%v", text, calls.Load(), err)
	}
}

func TestStallTimeoutDefaultsPerPath(t *testing.T) {
	client := New("https://provider.example", "k")
	if client.stallTimeout(chatStall) != DefaultStallTimeout || client.stallTimeout(responsesStall) != DefaultResponsesStallTimeout {
		t.Fatalf("defaults: chat=%s responses=%s", client.stallTimeout(chatStall), client.stallTimeout(responsesStall))
	}
	client.StallTimeout = time.Second
	if client.stallTimeout(chatStall) != time.Second || client.stallTimeout(responsesStall) != time.Second {
		t.Fatal("an explicit stall timeout must apply to every path")
	}
	if client.HTTP.Timeout != 0 {
		t.Fatalf("the HTTP client must not impose a total timeout: %s", client.HTTP.Timeout)
	}
}
