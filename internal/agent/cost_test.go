package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

// The proactive compaction trigger must fire on the provider-REPORTED prompt
// size, not the chars/4 estimate: a session whose real context crosses
// compactPct of the window compacts even when the text estimate stays below
// it (images are undercounted by the estimator).
func TestMaybeCompactUsesRealUsage(t *testing.T) {
	var summaryCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream { // compaction summary call
			summaryCalls.Add(1)
			w.Write([]byte(`{"choices":[{"message":{"content":"folded"}}],"usage":{"prompt_tokens":100,"completion_tokens":10}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`+"\n\n")
		// Report a prompt size that is 40% of a 1M window — over compactPct=30
		// even though the tiny text history estimates to a few hundred tokens.
		fmt.Fprint(w, `data: {"choices":[],"usage":{"prompt_tokens":400000,"completion_tokens":5}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ag := newTestAgent(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.ContextLimit = 1_000_000
	ag.CompactThreshold = 0.30
	// Seed enough history that there's something to fold.
	for range 6 {
		ag.Messages = append(ag.Messages,
			llm.Message{Role: "user", Content: strings.Repeat("q", 2000)},
			llm.Message{Role: "assistant", Content: strings.Repeat("a", 2000)},
		)
	}
	// Estimate is far below 30% of 1M, so this ONLY compacts if the trigger
	// reads the real 400k prompt size.
	if est := EstimateTokens(ag.Messages); est >= 300_000 {
		t.Fatalf("test setup: estimate %d should be well under 300k for the real-usage path to matter", est)
	}
	if _, err := ag.Turn(context.Background(), "continue", Events{}); err != nil {
		t.Fatal(err)
	}
	if summaryCalls.Load() == 0 {
		t.Fatal("real prompt size 400k (>30% of 1M) should have triggered compaction despite the low text estimate")
	}
	// The fold invalidates the pre-fold prompt size: it must not linger and
	// re-trigger a fold of the fresh fold on the next round.
	if ag.lastPrompt != 0 {
		t.Fatalf("lastPrompt should reset after compaction, got %d", ag.lastPrompt)
	}
}

// Only the agent's own conversation requests drive lastPrompt. AddUsage also
// receives foreground-subagent and summary-call usage (session spend), whose
// prompt sizes say nothing about this conversation's context.
func TestLastPromptIgnoresFannedInUsage(t *testing.T) {
	ag := newTestAgent(nil, "m", 100, "sys")
	ag.AddUsage(llm.Usage{PromptTokens: 900_000})
	if ag.lastPrompt != 0 {
		t.Fatalf("AddUsage must not set lastPrompt, got %d", ag.lastPrompt)
	}
	ag.notePrompt(llm.Usage{PromptTokens: 1234})
	ag.notePrompt(llm.Usage{}) // no usage reported: keep the last real value
	if ag.lastPrompt != 1234 {
		t.Fatalf("notePrompt should set lastPrompt to 1234, got %d", ag.lastPrompt)
	}
}

// When the provider reports no usage, the estimate fallback still drives the
// trigger (no regression for usage-less providers).
func TestMaybeCompactEstimateFallback(t *testing.T) {
	var summaryCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			summaryCalls.Add(1)
			w.Write([]byte(`{"choices":[{"message":{"content":"folded"}}]}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n") // no usage chunk
	}))
	defer srv.Close()

	ag := newTestAgent(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.ContextLimit = 1000 // tiny window so the estimate crosses 30%
	ag.CompactThreshold = 0.30
	for range 6 {
		ag.Messages = append(ag.Messages,
			llm.Message{Role: "user", Content: strings.Repeat("q", 2000)},
			llm.Message{Role: "assistant", Content: strings.Repeat("a", 2000)},
		)
	}
	if _, err := ag.Turn(context.Background(), "continue", Events{}); err != nil {
		t.Fatal(err)
	}
	if summaryCalls.Load() == 0 {
		t.Fatal("with no usage reported, the estimate (>30% of the tiny window) should still trigger compaction")
	}
}
