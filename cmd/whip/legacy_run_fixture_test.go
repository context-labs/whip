package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

// respondToTitleRequest handles the daemon's prompt-only naming request without
// adding it to conversation captures or advancing a tool-call fixture.
func respondToTitleRequest(t *testing.T, w http.ResponseWriter, request llm.Request) bool {
	t.Helper()
	if len(request.Messages) == 0 || request.Messages[0].Role != "system" ||
		!strings.HasPrefix(request.Messages[0].Content, "Name this session based on") {
		return false
	}
	if request.Stream || len(request.Tools) != 0 || len(request.Messages) != 2 ||
		request.Messages[1].Role != "user" || strings.TrimSpace(request.Messages[1].Content) == "" {
		t.Errorf("title request must be nonstreaming and prompt-only: %+v", request)
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Fixture session title"},"finish_reason":"stop"}]}`)
	return true
}

// runFixture writes a config pointing the default model at a test server that
// replies with reply and records only conversation requests into reqs.
func legacyRunFixture(t *testing.T, reply string, reqs *[]llm.Request) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		if respondToTitleRequest(t, w, req) {
			return
		}
		if reqs != nil {
			*reqs = append(*reqs, req)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		body, _ := json.Marshal(reply)
		fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%s},"finish_reason":"stop"}]}`+"\n\n", body)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	home := t.TempDir()
	t.Setenv("WHIPCODE_HOME", home)
	cfg := fmt.Sprintf(`{
		"defaultModel": "test",
		"rlm": {"enabled": false},
		"mcpImport": {"claude": {"enabled": false}, "codex": {"enabled": false}},
		"providers": {"testprov": {"baseUrl": %q, "api": "openai-completions", "apiKey": "k"}},
		"models": {"test": {"providers": ["testprov"], "maxOut": 100}}
	}`, srv.URL)
	if err := os.WriteFile(filepath.Join(home, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	useTestDaemon(t)
}
