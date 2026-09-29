package llm

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"strings"
	"testing"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// sse builds an Anthropic event stream from event objects, marshaled here so
// fixtures stay valid JSON by construction.
func sse(events ...any) string {
	lines := make([]string, 0, len(events)+1)
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			panic(err)
		}
		lines = append(lines, "data: "+string(encoded))
	}
	lines = append(lines, "")
	return strings.Join(lines, "\n")
}

func sseResponse(events ...any) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(sse(events...)))}
}

func event(kind string, fields map[string]any) map[string]any {
	event := map[string]any{"type": kind}
	maps.Copy(event, fields)
	return event
}

func TestEncodeMessagesBasics(t *testing.T) {
	tool := NewTool("calc", "Evaluate arithmetic", `{"type":"object","properties":{"expr":{"type":"string"}}}`)
	req := Request{
		Model: "claude-opus-5-5",
		Messages: []Message{
			{Role: "system", Content: "You are whip."},
			{Role: "user", Content: "What is 6*7? Use the calc tool."},
		},
		Tools:     []Tool{tool},
		MaxTokens: 1024,
	}
	body, err := encodeMessages(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["system"] != "You are whip." {
		t.Fatalf("system not hoisted: %s", body)
	}
	if decoded["max_tokens"] != float64(1024) {
		t.Fatalf("max_tokens missing: %s", body)
	}
	if decoded["anthropic_version"] != "2023-06-01" {
		t.Fatalf("version field missing: %s", body)
	}
	msgs := decoded["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["role"] != "user" {
		t.Fatalf("unexpected messages: %s", body)
	}
	tools := decoded["tools"].([]any)
	if tools[0].(map[string]any)["name"] != "calc" {
		t.Fatalf("tool name not anthropic shape: %s", body)
	}
	if _, exists := decoded["prompt_cache_key"]; exists {
		t.Fatal("chat-only prompt_cache_key leaked into messages request")
	}
}

func TestEncodeMessagesToolRoundTrip(t *testing.T) {
	// The exact history shape that 400s through the chat translator: an
	// assistant tool-call turn (empty content) followed by a tool result.
	// Natively this encodes to tool_use + tool_result blocks cleanly.
	var call ToolCall
	call.ID, call.Type = "toolu_1", "function"
	call.Function.Name, call.Function.Arguments = "calc", `{"expr":"6*7"}`
	req := Request{
		Model: "claude-opus-5-5",
		Messages: []Message{
			{Role: "user", Content: "What is 6*7?"},
			{Role: "assistant", ToolCalls: []ToolCall{call}},
			{Role: "tool", ToolCallID: "toolu_1", Name: "calc", Content: "42"},
		},
	}
	body, err := encodeMessages(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type      string         `json:"type"`
				ID        string         `json:"id"`
				ToolUseID string         `json:"tool_use_id"`
				Input     map[string]any `json:"input"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", len(decoded.Messages), body)
	}
	assistant := decoded.Messages[1]
	if assistant.Role != "assistant" || len(assistant.Content) != 1 || assistant.Content[0].Type != "tool_use" {
		t.Fatalf("assistant tool_use not encoded: %s", body)
	}
	if assistant.Content[0].Input["expr"] != "6*7" {
		t.Fatalf("tool input not decoded from arguments: %s", body)
	}
	result := decoded.Messages[2]
	if result.Role != "user" || result.Content[0].Type != "tool_result" || result.Content[0].ToolUseID != "toolu_1" {
		t.Fatalf("tool_result not encoded as user role: %s", body)
	}
}

func TestEncodeMessagesThinkingBudget(t *testing.T) {
	// Effort maps to thinking.budget_tokens; the budget always leaves room
	// for visible output below the max_tokens ceiling.
	for effort, want := range map[string]int{"low": 2048, "medium": 4096, "high": 4096 /* capped by maxTokens/2 */, "max": 4096 /* capped by maxTokens/2 */} {
		req := Request{Model: "claude-opus-5-5", ReasoningEffort: effort, MaxTokens: 8192}
		body, err := encodeMessages(req)
		if err != nil {
			t.Fatalf("%s: %v", effort, err)
		}
		var decoded struct {
			Thinking struct {
				Type         string `json:"type"`
				BudgetTokens int    `json:"budget_tokens"`
			} `json:"thinking"`
		}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Thinking.Type != "enabled" || decoded.Thinking.BudgetTokens != want {
			t.Fatalf("%s: thinking = %+v, want enabled/%d", effort, decoded.Thinking, want)
		}
	}
	// Known off-levels omit thinking entirely.
	for _, off := range []string{"", "off", "none"} {
		req := Request{Model: "claude-opus-5-5", ReasoningEffort: off, MaxTokens: 1024}
		body, err := encodeMessages(req)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `"thinking"`) {
			t.Fatalf("effort %q must not enable thinking: %s", off, body)
		}
	}
	// Unknown levels fail loudly rather than guess a budget.
	if _, err := encodeMessages(Request{Model: "x", ReasoningEffort: "ultra"}); err == nil {
		t.Fatal("expected error for unknown effort level")
	}
}

func TestEncodeMessagesReplaysThinkingBeforeToolUse(t *testing.T) {
	var call ToolCall
	call.ID, call.Type = "toolu_1", "function"
	call.Function.Name, call.Function.Arguments = "calc", `{"expr":"6*7"}`
	req := Request{
		Model: "claude-opus-5-5",
		Messages: []Message{{
			Role:      "assistant",
			Thinking:  []ThinkingBlock{{Thinking: "hmm", Signature: "sig-abc"}},
			ToolCalls: []ToolCall{call},
		}},
	}
	body, err := encodeMessages(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []struct {
			Content []struct {
				Type      string `json:"type"`
				Thinking  string `json:"thinking"`
				Signature string `json:"signature"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	blocks := decoded.Messages[0].Content
	if len(blocks) < 2 || blocks[0].Type != "thinking" || blocks[0].Signature != "sig-abc" || blocks[1].Type != "tool_use" {
		t.Fatalf("thinking must precede tool_use with signature verbatim: %s", body)
	}
}

func TestParseMessagesSSECapturesThinkingSignature(t *testing.T) {
	stream := sse(
		event("message_start", nil),
		event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "thinking"}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "rea"}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "thinking_delta", "thinking": "soning"}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "sig-123"}}),
		event("content_block_start", map[string]any{"index": 1, "content_block": map[string]any{"type": "tool_use", "id": "toolu_2", "name": "calc"}}),
		event("content_block_delta", map[string]any{"index": 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{}`}}),
		event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "tool_use"}}),
		event("message_stop", nil),
	)
	var thought string
	msg, _, err := parseMessagesSSE(strings.NewReader(stream), nil, func(s string) { thought += s }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if thought != "reasoning" {
		t.Fatalf("onThink stream: %q", thought)
	}
	if len(msg.Thinking) != 1 || msg.Thinking[0].Thinking != "reasoning" || msg.Thinking[0].Signature != "sig-123" {
		t.Fatalf("captured thinking: %+v", msg.Thinking)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("tool calls: %+v", msg.ToolCalls)
	}
}

func TestChatEncodeClearsThinking(t *testing.T) {
	// Chat-completions has no thinking shape; captured blocks must not leak.
	client := New("https://api.inference.net/v1", "key")
	msgs := []Message{{Role: "assistant", Thinking: []ThinkingBlock{{Thinking: "x", Signature: "y"}}}}
	req := Request{Model: "claude-opus-5-5", Messages: msgs}
	body, err := client.encodeChatRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "signature") || strings.Contains(string(body), "sig") {
		t.Fatalf("thinking leaked into chat request: %s", body)
	}
	if len(msgs[0].Thinking) == 0 {
		t.Fatal("encodeChatRequest must not mutate the caller's history")
	}
}

func TestMessageThinkingRoundTripsThroughJSON(t *testing.T) {
	// Persisted session compat: old sessions decode with no thinking; new
	// ones keep the signature verbatim across marshal/unmarshal.
	encoded, err := json.Marshal(Message{Role: "assistant", Thinking: []ThinkingBlock{{Thinking: "t", Signature: "s"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"signature":"s"`) {
		t.Fatalf("signature not persisted: %s", encoded)
	}
	var decoded Message
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Thinking) != 1 || decoded.Thinking[0].Signature != "s" {
		t.Fatalf("round trip: %+v", decoded.Thinking)
	}
	var legacy Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"old"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Thinking != nil {
		t.Fatal("legacy messages must decode with nil thinking")
	}
}

func TestParseMessagesSSE(t *testing.T) {
	stream := sse(
		event("message_start", map[string]any{"usage": map[string]any{"input_tokens": 10}}),
		event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_1", "name": "calc"}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"expr"`}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `:"6*7"}`}}),
		event("content_block_start", map[string]any{"index": 1, "content_block": map[string]any{"type": "text"}}),
		event("content_block_delta", map[string]any{"index": 1, "delta": map[string]any{"type": "text_delta", "text": "Done."}}),
		event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "tool_use"}}),
		event("message_stop", nil),
	)
	var toolArgs string
	msg, _, err := parseMessagesSSE(strings.NewReader(stream), nil, nil, func(id, name, args string) {
		toolArgs = args
	})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "Done." {
		t.Fatalf("text: %q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "toolu_1" || msg.ToolCalls[0].Function.Name != "calc" {
		t.Fatalf("tool calls: %+v", msg.ToolCalls)
	}
	if toolArgs != `{"expr":"6*7"}` {
		t.Fatalf("streamed args: %q", toolArgs)
	}
}

func TestMessagesClientTargetsMessagesEndpoint(t *testing.T) {
	var gotPath string
	client := NewMessagesClient("https://api.inference.net/v1", "key")
	client.HTTP = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		if r.Header.Get("X-Api-Key") != "key" {
			t.Fatal("missing X-Api-Key header")
		}
		if r.Header.Get("Anthropic-Version") == "" {
			t.Fatal("missing Anthropic-Version header")
		}
		return sseResponse(
			event("message_start", nil),
			event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": "hi"}}),
			event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "end_turn"}}),
			event("message_stop", nil),
		), nil
	})}
	msg, _, err := client.messagesOnce(t.Context(), []byte(`{}`), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("posted to %s, want /v1/messages", gotPath)
	}
	if msg.Content != "hi" {
		t.Fatalf("content: %q", msg.Content)
	}
}

func TestMessagesStreamDispatchRoundTrip(t *testing.T) {
	// End to end through Stream's dispatch + retry machinery on the messages
	// flavor: encode, route to /messages, parse, accumulate tool calls.
	firstCall := true
	client := NewMessagesClient("https://api.example.test/v1", "key")
	client.HTTP = &http.Client{Transport: rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("posted to %s, want /v1/messages", r.URL.Path)
		}
		if firstCall {
			firstCall = false
			return sseResponse(
				event("message_start", nil),
				event("content_block_start", map[string]any{"index": 0, "content_block": map[string]any{"type": "tool_use", "id": "toolu_9", "name": "calc"}}),
				event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"expr":"2+2"}`}}),
				event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "tool_use"}}),
				event("message_stop", nil),
			), nil
		}
		return sseResponse(
			event("message_start", nil),
			event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": "4"}}),
			event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "end_turn"}}),
			event("message_stop", nil),
		), nil
	})}
	req := Request{
		Model:     "claude-opus-5-5",
		Messages:  []Message{{Role: "user", Content: "2+2 use calc"}},
		Tools:     []Tool{NewTool("calc", "arithmetic", `{"type":"object"}`)},
		MaxTokens: 128,
	}
	first, _, err := client.Stream(t.Context(), req, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].ID != "toolu_9" {
		t.Fatalf("first round tool calls: %+v", first.ToolCalls)
	}
}

func TestEncodeMessagesMergesParallelToolResults(t *testing.T) {
	// Two tool results from one assistant turn must land in a single user
	// message with two tool_result blocks; Anthropic rejects consecutive
	// user turns for parallel results.
	var callA, callB ToolCall
	callA.ID, callA.Type = "toolu_1", "function"
	callA.Function.Name, callA.Function.Arguments = "calc", `{"expr":"6*7"}`
	callB.ID, callB.Type = "toolu_2", "function"
	callB.Function.Name, callB.Function.Arguments = "calc", `{"expr":"2+2"}`
	req := Request{
		Model: "claude-opus-5-5",
		Messages: []Message{
			{Role: "user", Content: "compute both"},
			{Role: "assistant", ToolCalls: []ToolCall{callA, callB}},
			{Role: "tool", ToolCallID: "toolu_1", Content: "42"},
			{Role: "tool", ToolCallID: "toolu_2", Content: "4"},
		},
	}
	body, err := encodeMessages(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type      string `json:"type"`
				ToolUseID string `json:"tool_use_id"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	// user, assistant, merged-results = 3 messages (not 4).
	if len(decoded.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d: %s", len(decoded.Messages), body)
	}
	results := decoded.Messages[2]
	if results.Role != "user" {
		t.Fatalf("merged results role: %s, want user", results.Role)
	}
	if len(results.Content) != 2 {
		t.Fatalf("expected 2 tool_result blocks, got %d: %s", len(results.Content), body)
	}
	if results.Content[0].Type != "tool_result" || results.Content[0].ToolUseID != "toolu_1" {
		t.Fatalf("first result: %+v", results.Content[0])
	}
	if results.Content[1].Type != "tool_result" || results.Content[1].ToolUseID != "toolu_2" {
		t.Fatalf("second result: %+v", results.Content[1])
	}
}

func TestParseMessagesSSEReportsUsage(t *testing.T) {
	// message_start carries input_tokens under message.usage; message_delta
	// carries output_tokens at the root. Both must reach the returned Usage.
	stream := sse(
		event("message_start", map[string]any{"message": map[string]any{"usage": map[string]any{"input_tokens": 12}}}),
		event("content_block_delta", map[string]any{"index": 0, "delta": map[string]any{"type": "text_delta", "text": "hi"}}),
		event("message_delta", map[string]any{"delta": map[string]any{"stop_reason": "end_turn"}, "usage": map[string]any{"output_tokens": 7}}),
		event("message_stop", nil),
	)
	_, usage, err := parseMessagesSSE(strings.NewReader(stream), nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if usage.PromptTokens != 12 {
		t.Fatalf("prompt tokens: %d, want 12", usage.PromptTokens)
	}
	if usage.CompletionTokens != 7 {
		t.Fatalf("completion tokens: %d, want 7", usage.CompletionTokens)
	}
	if !usage.Reported {
		t.Fatal("usage.Reported should be true when token counts are present")
	}
}

func TestEncodeMessagesThinkingBudgetBelowMaxTokens(t *testing.T) {
	// When MaxTokens is small (2048), the 1024 budget floor must not
	// collide with the ceiling. max_tokens must always exceed budget_tokens,
	// and max_tokens must be present when thinking is enabled even if
	// MaxTokens was zero (Anthropic requires the field).
	for _, mt := range []int{0, 1024, 2048} {
		req := Request{Model: "claude-opus-5-5", ReasoningEffort: "minimal", MaxTokens: mt}
		body, err := encodeMessages(req)
		if err != nil {
			t.Fatalf("maxTokens=%d: %v", mt, err)
		}
		var decoded struct {
			MaxTokens int `json:"max_tokens"`
			Thinking  struct {
				BudgetTokens int `json:"budget_tokens"`
			} `json:"thinking"`
		}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.MaxTokens <= decoded.Thinking.BudgetTokens {
			t.Fatalf("maxTokens=%d: max_tokens %d <= budget %d", mt, decoded.MaxTokens, decoded.Thinking.BudgetTokens)
		}
		if decoded.MaxTokens == 0 {
			t.Fatalf("maxTokens=%d: max_tokens must be present when thinking is enabled", mt)
		}
	}
}
