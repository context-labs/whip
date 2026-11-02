package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

// With no advertised output cap the route falls back to the whole context
// window as MaxTokens. Every conversation request must still ask for no more
// output than the window leaves beside its input (#345): providers reject
// input + max_tokens > context outright.
func TestTurnRequestsFitOutputInsideTheContextWindow(t *testing.T) {
	t.Parallel()
	const window = 100_000
	type seen struct {
		maxTokens int
		input     int
	}
	var requests []seen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests = append(requests, seen{maxTokens: request.MaxTokens, input: llm.EstimateTokens(request.Messages)})
		w.Header().Set("Content-Type", "text/event-stream")
		switch len(requests) {
		case 1:
			// The provider measures 60k prompt tokens: more than the local
			// estimate, so the next request must reserve room for it.
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"echo","arguments":"{\"s\":\"x\"}"}}]}}],"usage":{"prompt_tokens":60000,"completion_tokens":5}}`+"\n\n")
		default:
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":60010,"completion_tokens":1}}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	ag := NewRuntime(llm.New(server.URL, "key"), "model", window, "system", tools.NewServices())
	defer ag.Services.Close()
	ag.ContextLimit = window
	ag.CompactThreshold = 0.9 // keep proactive compaction out of the way
	ag.Tools = []tools.Tool{echoTool()}
	ag.MaxTurns = 1
	if _, err := ag.Turn(t.Context(), "go", Events{}); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want a tool round and a capped final answer", len(requests))
	}
	first, final := requests[0], requests[1]
	if first.maxTokens <= 0 || first.maxTokens >= window || first.input+first.maxTokens > window {
		t.Fatalf("first request max_tokens = %d beside ~%d input tokens in a %d window", first.maxTokens, first.input, window)
	}
	if final.maxTokens <= 0 || final.maxTokens > window-60_000 {
		t.Fatalf("final request max_tokens = %d, want at most %d after a 60000-token prompt", final.maxTokens, window-60_000)
	}
	if ag.MaxTokens != window {
		t.Fatalf("configured MaxTokens changed to %d", ag.MaxTokens)
	}
}
