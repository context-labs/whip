package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

func TestReasoningReplayedAfterToolAndResume(t *testing.T) {
	requests := make(chan llm.Request, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "text/event-stream")
		if len(request.Messages) == 2 {
			fmt.Fprint(w, "data: "+`{"choices":[{"delta":{"reasoning_content":"Inspect evidence before editing.","tool_calls":[{"index":0,"id":"reasoning-call","type":"function","function":{"name":"echo","arguments":"{\"s\":\"evidence\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, "data: "+`{"choices":[{"delta":{"reasoning_content":"Evidence confirms the hypothesis.","content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	ag := NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
	defer ag.Services.Close()
	ag.Tools = []tools.Tool{echoTool()}
	var journal []llm.Message
	final, err := ag.Turn(t.Context(), "work", Events{OnMessage: func(message llm.Message) int {
		journal = append(journal, message)
		return len(journal)
	}})
	if err != nil || final != "done" {
		t.Fatalf("turn = %q, %v", final, err)
	}
	<-requests
	second := <-requests
	if second.Messages[2].ReasoningContent != "Inspect evidence before editing." ||
		second.Messages[3].Role != "tool" || second.Messages[3].ToolCallID != "reasoning-call" {
		t.Fatalf("tool continuation lost the assistant decision: %+v", second.Messages)
	}
	if journal[1].ReasoningContent != second.Messages[2].ReasoningContent {
		t.Fatal("durable journal lost the streamed reasoning")
	}
	saved, err := json.Marshal(ag.MessagesSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	resumed := NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
	defer resumed.Services.Close()
	if err := json.Unmarshal(saved, &resumed.Messages); err != nil {
		t.Fatal(err)
	}
	if _, err := resumed.Turn(t.Context(), "continue", Events{}); err != nil {
		t.Fatal(err)
	}
	third := <-requests
	if third.Messages[2].ReasoningContent != "Inspect evidence before editing." ||
		third.Messages[4].ReasoningContent != "Evidence confirms the hypothesis." {
		t.Fatal("resumed request lost earlier reasoning")
	}
}

func TestReasoningOnlyInterruptedResponseIsPreserved(t *testing.T) {
	ag := &Agent{Model: "model", Provider: "provider"}
	ag.preserveModelResponse(Events{}, llm.Message{
		Role: "assistant", ReasoningContent: "Partial investigation",
	}, llm.Usage{}, errors.New("stream interrupted"))
	if len(ag.Messages) != 1 || ag.Messages[0].ReasoningContent != "Partial investigation" ||
		!strings.Contains(ag.Messages[0].Content, "[response interrupted]") {
		t.Fatalf("reasoning-only response discarded: %+v", ag.Messages)
	}
}

func TestCompactionTranscriptIncludesBoundedReasoning(t *testing.T) {
	msg := llm.Message{Role: "assistant", RawSequence: 7, ReasoningContent: "Investigate cleanup. " + strings.Repeat("x", 10000)}
	prompt := buildSummaryPrompt([]llm.Message{msg}, "")
	if !strings.Contains(prompt, "assistant reasoning: Investigate cleanup.") ||
		!strings.Contains(prompt, "context.history(seq=7)") || strings.Contains(prompt, strings.Repeat("x", 3000)) {
		t.Fatal("compaction lost reasoning or failed to bound its excerpt")
	}
}
