package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExposedReasoningSurvivesStreamAndMessageHistory(t *testing.T) {
	const want = "Hypothesis A failed.\nInvestigate B → cleanup."
	for _, callback := range []bool{false, true} {
		name := "nil_callback"
		if callback {
			name = "thinking_callback"
		}
		t.Run(name, func(t *testing.T) {
			srv := sseServer(t,
				`data: {"choices":[{"delta":{"reasoning_content":"Hypothesis A failed.\n"}}]}`,
				`data: {"choices":[{"delta":{"reasoning_content":"Investigate B → cleanup.","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"read","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`,
				`data: [DONE]`,
			)
			defer srv.Close()
			var thought strings.Builder
			var onThink func(string)
			if callback {
				onThink = func(s string) { thought.WriteString(s) }
			}
			msg, _, err := New(srv.URL, "test-key").Stream(t.Context(), Request{Model: "reasoning-model"}, nil, onThink, nil)
			if err != nil || msg.ReasoningContent != want || msg.Content != "" || len(msg.ToolCalls) != 1 {
				t.Fatalf("reasoning/tool stream = %+v, %v", msg, err)
			}
			if callback && thought.String() != want {
				t.Fatalf("thinking callback = %q", thought.String())
			}
			data, err := json.Marshal(msg)
			if err != nil {
				t.Fatal(err)
			}
			var restored Message
			if err := json.Unmarshal(data, &restored); err != nil {
				t.Fatal(err)
			}
			wire, err := New("https://proxy.example/v1", "key").encodeChatRequest(Request{
				Model: "reasoning-model", Messages: stripAuthored([]Message{restored}),
			})
			if err != nil {
				t.Fatal(err)
			}
			var request Request
			if err := json.Unmarshal(wire, &request); err != nil {
				t.Fatal(err)
			}
			if got := request.Messages[0]; got.ReasoningContent != want || got.Content != "" || got.ToolCalls[0].ID != "c1" {
				t.Fatalf("history/request lost reasoning or mixed it into content: %+v", got)
			}
		})
	}
}

func TestReasoningContentDoesNotChangeLegacyMessages(t *testing.T) {
	for _, body := range []string{
		`{"role":"assistant","content":"answer"}`,
		`{"role":"assistant","content":null,"reasoning_content":null}`,
	} {
		msg := Message{ReasoningContent: "stale thought"}
		if err := json.Unmarshal([]byte(body), &msg); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(msg)
		if err != nil || msg.ReasoningContent != "" || strings.Contains(string(data), "reasoning_content") {
			t.Fatalf("legacy message gained stale/empty reasoning: %s, %v", data, err)
		}
	}
}

func TestReasoningReplayRespectsDirectOpenAIChatContract(t *testing.T) {
	history := []Message{{Role: "assistant", Content: "visible", ReasoningContent: "compatible-provider thought"}}
	for _, endpoint := range []string{"https://api.openai.com/v1", "https://api.openai.com/custom/v1"} {
		data, err := New(endpoint, "key").encodeChatRequest(Request{Model: "model", Messages: history})
		if err != nil {
			t.Fatal(err)
		}
		hasReasoning := strings.Contains(string(data), "reasoning_content")
		if hasReasoning != (endpoint != "https://api.openai.com/v1") {
			t.Fatalf("unexpected reasoning wire contract for %s: %s", endpoint, data)
		}
		if history[0].ReasoningContent == "" {
			t.Fatal("provider filtering mutated saved history")
		}
	}
}

func TestReasoningCountsTowardContextEstimate(t *testing.T) {
	plain := []Message{{Role: "assistant", Content: "visible"}}
	withReasoning := []Message{{Role: "assistant", Content: "visible", ReasoningContent: strings.Repeat("r", 8000)}}
	if EstimateTokens(withReasoning)-EstimateTokens(plain) < 2000 {
		t.Fatal("a large retained reasoning history was omitted from the context estimate")
	}
}
