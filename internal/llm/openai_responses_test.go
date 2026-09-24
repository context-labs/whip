package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAstraAPIResponsesToolRoundTripAndBudget(t *testing.T) {
	client := New("https://api.openai.com/v1", "fixture-only-key")
	client.MaxRetries = 1
	calls, settled := 0, 0
	items := `[{"type":"reasoning","id":"r1","encrypted_content":"opaque-state","summary":[]},` +
		`{"type":"function_call","id":"fc1","call_id":"call1","name":"rlm_exec","arguments":"{\"code\":\"print(42)\"}"}]`
	client.HTTP = &http.Client{Transport: subscriptionTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://api.openai.com/v1/responses" || r.Header.Get("Authorization") != "Bearer fixture-only-key" || r.Header.Get("Chatgpt-Account-Id") != "" {
			t.Fatal("API credentials used the wrong destination or subscription headers")
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Model     string `json:"model"`
			MaxOutput int    `json:"max_output_tokens"`
			Reasoning struct {
				Effort string `json:"effort"`
			} `json:"reasoning"`
			Input []json.RawMessage `json:"input"`
			Tools []json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "gpt-6-astra" || body.MaxOutput != 32 || body.Reasoning.Effort != "medium" || len(body.Tools) != 1 {
			t.Fatalf("wrong model/effort/budget/tools: %s", raw)
		}
		if strings.Contains(string(raw), `"max_tokens"`) || strings.Contains(string(raw), `"messages"`) || strings.Contains(string(raw), "fixture-only-key") {
			t.Fatal("Chat Completions fields or secret leaked into Responses body")
		}
		output := items
		if calls > 1 {
			if strings.Count(string(raw), "opaque-state") != 1 || !strings.Contains(string(raw), `"type":"function_call_output"`) || !strings.Contains(string(raw), `"output":"42"`) {
				t.Fatalf("tool output/continuation lost: %s", raw)
			}
			output = `[{"type":"message","id":"m1","role":"assistant","content":[{"type":"output_text","text":"42"}]}]`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("data: " + responseFixture(output) + "\n\n"))}, nil
	})}
	budget := attemptBudgetFunc(func(_ context.Context, attempt ModelAttempt) (ModelPermit, error) {
		return ModelPermit{MaxTokens: 32, Timeout: attempt.Timeout, Settle: func(result ModelAttemptResult) error {
			settled++
			if !result.Dispatched || result.Failed {
				t.Fatalf("attempt not settled successfully: %+v", result)
			}
			return nil
		}}, nil
	})
	request := Request{
		Model: "gpt-6-astra", MaxTokens: 128000, ReasoningEffort: "medium",
		Messages:   []Message{{Role: "system", Content: "WHIP"}, {Role: "user", Content: "Calculate 6*7"}},
		Tools:      []Tool{NewTool("rlm_exec", "Run code", `{"type":"object","properties":{"code":{"type":"string"}}}`)},
		Accounting: &CallAccounting{Budget: budget},
	}
	message, usage, err := client.Stream(t.Context(), request, nil, nil, nil)
	if err != nil || len(message.ToolCalls) != 1 || usage.CompletionTokens != 9 {
		t.Fatalf("tool response: %+v %+v %v", message, usage, err)
	}
	stored, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(stored, &restored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), "fixture-only-key") {
		t.Fatal("API key persisted in history")
	}
	request.Messages = append(request.Messages, restored, Message{Role: "tool", ToolCallID: "call1", Name: "rlm_exec", Content: "42"})
	text, usage, err := client.Complete(t.Context(), request)
	if err != nil || text != "42" || usage.CompletionTokens != 9 || calls != 2 || settled != 2 {
		t.Fatalf("continuation: %q %+v %v calls=%d settled=%d", text, usage, err, calls, settled)
	}
	other := New("https://api.openai.com/v1", "other-fixture-key")
	body, err := encodeResponsesWithLimit(request, other.apiResponseScope(), 32)
	if err != nil || strings.Contains(string(body), "opaque-state") {
		t.Fatal("opaque continuation crossed API credentials")
	}
}

func TestAstraAPICustomRoutesKeepChatCompletions(t *testing.T) {
	for _, destination := range []string{"https://example.test/v1", "https://api.openai.com/proxy/v1"} {
		client := New(destination, "fixture-key")
		client.HTTP = &http.Client{Transport: subscriptionTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != destination+"/chat/completions" {
				t.Fatal("custom route switched protocols")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`))}, nil
		})}
		if text, _, err := client.Complete(t.Context(), Request{Model: "gpt-6-astra"}); err != nil || text != "ok" {
			t.Fatalf("custom completion: %q %v", text, err)
		}
	}
}
