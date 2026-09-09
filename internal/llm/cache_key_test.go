package llm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCacheKeysAtRequestBoundary(t *testing.T) {
	legacy := strings.Repeat("a", 32) + "/" + strings.Repeat("a", 32) + ":" + strings.Repeat("b", 16)
	for _, key := range []string{"", strings.Repeat("x", 64), strings.Repeat("x", 65), legacy, strings.Repeat("é", 40)} {
		t.Run(fmt.Sprintf("%d-bytes", len(key)), func(t *testing.T) {
			keys := make(chan string, 6)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request Request
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				keys <- request.PromptCacheKey
				if len(request.PromptCacheKey) > 64 {
					http.Error(w, "prompt_cache_key exceeds 64 bytes", http.StatusBadRequest)
					return
				}
				if request.Stream {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
				} else {
					fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`)
				}
			}))
			defer server.Close()
			for range 2 {
				client := New(server.URL, "test")
				client.CacheKey = key
				request := Request{Model: "test", Messages: []Message{{Role: "user", Content: "test"}}}
				if _, _, err := client.Stream(t.Context(), request, nil, nil, nil); err != nil {
					t.Fatal(err)
				}
				request.PromptCacheKey = key
				client.CacheKey = "different-client-key"
				if _, _, err := client.Complete(t.Context(), request); err != nil {
					t.Fatal(err)
				}
			}
			first := <-keys
			if len(key) <= 64 && first != key || len(key) > 64 && len(first) != 64 {
				t.Fatalf("unexpected key %q for %d bytes", first, len(key))
			}
			for range 3 {
				if got := <-keys; got != first {
					t.Fatalf("cache identity changed between calls: %q / %q", first, got)
				}
			}
		})
	}
	if normalizeCacheKey(legacy) == normalizeCacheKey(legacy+"other") {
		t.Fatal("distinct long keys collapsed")
	}
}

func TestPermanentRequestError(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		want   bool
	}{
		{"bad request", "400 Bad Request", true},
		{"credentials", "401 Unauthorized", true},
		{"rate limit", "429 Too Many Requests", false},
		{"timeout", "408 Request Timeout", false},
		{"server", "500 Internal Server Error", false},
		{"empty", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := fmt.Errorf("agent: %w", &HTTPError{Status: tc.status})
			if got := IsPermanentRequestError(err); got != tc.want {
				t.Fatalf("permanent = %v, want %v", got, tc.want)
			}
		})
	}
}
