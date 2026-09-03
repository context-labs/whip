package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A Client with CacheKey stamps prompt_cache_key on every request; without
// one the field is omitted entirely (providers that don't know it must not
// see it).
func TestPromptCacheKeyStampedFromClient(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	c.CacheKey = "session-abc"
	if _, _, err := c.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"prompt_cache_key":"session-abc"`) {
		t.Fatalf("prompt_cache_key missing from request: %s", body)
	}

	// No CacheKey → no field on the wire.
	c2 := New(srv.URL, "k")
	if _, _, err := c2.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "prompt_cache_key") {
		t.Fatalf("prompt_cache_key should be omitted when unset: %s", body)
	}
}

// An explicit Request.PromptCacheKey overrides the client's (a descendant
// scopes its own key over the parent's).
func TestPromptCacheKeyRequestOverridesClient(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	c.CacheKey = "session-abc"
	req := Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}, PromptCacheKey: "session-abc/task-3"}
	if _, _, err := c.Stream(context.Background(), req, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"prompt_cache_key":"session-abc/task-3"`) {
		t.Fatalf("request-level key should win: %s", body)
	}
}

// The prefix-cache contract: two consecutive requests in a session must share
// a byte-identical prefix up through the last message of the earlier request.
// This guards regressions that silently break provider prefix caching (e.g.
// someone adding a per-turn timestamp to the system prompt).
func TestConsecutiveRequestsSharePrefix(t *testing.T) {
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	c := New(srv.URL, "k")
	c.CacheKey = "sess"
	msgs := []Message{
		{Role: "system", Content: "You are whip."},
		{Role: "user", Content: "first"},
	}
	// Turn 1.
	resp, _, err := c.Stream(context.Background(), Request{Model: "m", Messages: msgs}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Turn 2 appends assistant + user; the system+first-user prefix is frozen.
	msgs = append(msgs, Message{Role: "assistant", Content: resp.Content})
	msgs = append(msgs, Message{Role: "user", Content: "second"})
	if _, _, err := c.Stream(context.Background(), Request{Model: "m", Messages: msgs}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if len(bodies) != 2 {
		t.Fatalf("captured %d requests, want 2", len(bodies))
	}
	b1, b2 := string(bodies[0]), string(bodies[1])
	// Turn 1's full message array is a prefix of turn 2's (turn 2 only
	// appends). Both serialize the system + first user message identically.
	if !strings.Contains(b2, b1[strings.Index(b1, `"messages"`):strings.LastIndex(b1, `}]`)+1]) {
		t.Fatalf("turn-2 request does not contain turn-1's message prefix byte-identically.\nturn1: %s\nturn2: %s", b1, b2)
	}
	// Both carry the same cache key.
	if !strings.Contains(b1, `"prompt_cache_key":"sess"`) || !strings.Contains(b2, `"prompt_cache_key":"sess"`) {
		t.Fatalf("cache key must be stable across turns:\n%s\n%s", b1, b2)
	}
}
