package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/codexauth"
)

// TestLiveCodexRetries drives the retry policy against the real ChatGPT
// subscription backend through a fault-injecting reverse proxy. It needs a
// logged-in ~/.codex/auth.json and WHIP_LIVE_CODEX=1; otherwise it skips.
//
// Faults are injected before the request reaches the backend, so each retry
// that then succeeds is a genuine end-to-end recovery:
//   - 503 then 429+Retry-After  → two transport retries, then a real answer
//   - a corrupted bearer token   → real 401 → forced token refresh → resend
//   - RateLimits()               → real /wham/usage
func TestLiveCodexRetries(t *testing.T) {
	if os.Getenv("WHIP_LIVE_CODEX") == "" {
		t.Skip("set WHIP_LIVE_CODEX=1 to run against the real subscription")
	}
	source := &codexauth.Source{}
	if err := source.Available(); err != nil {
		t.Skip("no Codex login: " + err.Error())
	}
	upstream, _ := url.Parse("https://chatgpt.com/backend-api")
	var mode atomic.Value // "transient" | "badtoken" | ""
	var requests atomic.Int32
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(upstream)
		pr.Out.Host = upstream.Host
		if mode.Load() == "badtoken" && requests.Load() == 1 {
			pr.Out.Header.Set("Authorization", "Bearer definitely-not-a-valid-token")
		}
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := requests.Add(1)
		if mode.Load() == "transient" {
			switch n {
			case 1:
				http.Error(w, "injected outage", http.StatusServiceUnavailable)
				return
			case 2:
				w.Header().Set("Retry-After", "1")
				http.Error(w, `{"error":{"type":"rate_limit_exceeded","message":"injected"}}`, http.StatusTooManyRequests)
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	defer srv.Close()

	client := NewCodex(srv.URL, source)
	client.MaxRetries = 4
	var events []RetryEvent
	client.SetOnRetry(func(ev RetryEvent) { events = append(events, ev) })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	req := Request{Model: "gpt-5.5", Messages: []Message{{Role: "user", Content: "Reply with exactly the word PONG."}}}

	// 1. transient failures before the backend, then a real answer
	mode.Store("transient")
	requests.Store(0)
	msg, usage, err := client.Stream(ctx, req, nil, nil, nil)
	if err != nil {
		t.Fatalf("transient: %v", err)
	}
	if !strings.Contains(strings.ToUpper(msg.Content), "PONG") || usage.CompletionTokens == 0 {
		t.Fatalf("transient: msg=%q usage=%+v", msg.Content, usage)
	}
	if requests.Load() != 3 || len(events) != 2 || events[1].Delay < time.Second {
		t.Fatalf("transient: requests=%d events=%+v", requests.Load(), events)
	}
	t.Logf("transient: recovered after %d requests, retry delays %v %v", requests.Load(), events[0].Delay, events[1].Delay)

	// 2. bad bearer → real 401 → ForceRefresh against the real token endpoint → resend
	mode.Store("badtoken")
	requests.Store(0)
	events = nil
	before, _ := source.Credentials(ctx)
	msg, _, err = client.Stream(ctx, req, nil, nil, nil)
	if err != nil {
		t.Fatalf("badtoken: %v", err)
	}
	after, _ := source.Credentials(ctx)
	if !strings.Contains(strings.ToUpper(msg.Content), "PONG") || requests.Load() != 2 || len(events) != 0 {
		t.Fatalf("badtoken: msg=%q requests=%d events=%+v", msg.Content, requests.Load(), events)
	}
	if before.AccessToken == after.AccessToken {
		t.Fatal("badtoken: the access token should have been refreshed")
	}
	t.Logf("badtoken: 401 → refreshed token (changed=%v) → resend succeeded", before.AccessToken != after.AccessToken)

	// 3. live usage endpoint
	mode.Store("")
	limits, err := client.RateLimits(ctx)
	if err != nil || len(limits.Windows) == 0 {
		t.Fatalf("usage: %+v err=%v", limits, err)
	}
	t.Logf("usage: plan=%s windows=%+v", limits.Plan, limits.Windows)

	// 4. a permanent error still surfaces as such through the proxy
	requests.Store(0)
	_, _, err = client.Stream(ctx, Request{Model: "no-such-model-xyz", Messages: req.Messages}, nil, nil, nil)
	var he *HTTPError
	if err == nil || !errors.As(err, &he) || requests.Load() != 1 {
		t.Fatalf("permanent: err=%v requests=%d", err, requests.Load())
	}
	t.Logf("permanent: %v (1 request, no retry)", err)
}
