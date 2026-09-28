package model

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

// These fixtures cover the pinned wire profiles, not live provider availability.
func TestChatCompatibilityProfiles(t *testing.T) {
	for _, tc := range []struct {
		name, url, model string
		omitCache        bool
		disableThinking  bool
	}{
		{name: "openai", url: "https://api.openai.com/v1"},
		{name: "openrouter", url: "https://openrouter.ai/api/v1"},
		{name: "inference", url: "https://api.inference.net/v1"},
		{name: "xai", url: "https://api.x.ai/v1"},
		{name: "cerebras", url: "https://api.cerebras.ai/v1", omitCache: true},
		{name: "groq", url: "https://api.groq.com/openai/v1", omitCache: true},
		{name: "fireworks", url: "https://api.fireworks.ai/inference/v1", omitCache: true},
		{name: "together", url: "https://api.together.ai/v1", omitCache: true},
		{name: "deepinfra", url: "https://api.deepinfra.com/v1/openai", omitCache: true},
		{name: "deepseek v4", url: "https://api.deepseek.com", omitCache: true, disableThinking: true},
		{name: "deepseek trailing slash", url: "https://api.deepseek.com/", omitCache: true, disableThinking: true},
		{name: "deepseek other model", url: "https://api.deepseek.com", model: "deepseek-chat", omitCache: true},
		{name: "deepseek model prefix lookalike", url: "https://api.deepseek.com", model: "deepseek-v40-flash", omitCache: true},
		{name: "deepseek custom path", url: "https://api.deepseek.com/custom/v1"},
		{name: "deepseek lookalike host", url: "https://api.deepseek.com.example.test"},
		{name: "deepseek custom port", url: "https://api.deepseek.com:8443"},
		{name: "deepseek http proxy", url: "http://api.deepseek.com"},
		{name: "groq custom path", url: "https://api.groq.com/openai/v10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := chatRequest()
			request.OutputTokenLimit = new(int64(37))
			request.Selection.Name = tc.model
			if request.Selection.Name == "" {
				request.Selection.Name = "deepseek-v4-flash"
			}
			request.Selection.Effort = "high"
			request.Tools = []Tool{executeTool()}
			request.Messages = append(request.Messages,
				Message{Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{"code":"print(42)"}`)}}}},
				Message{Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "call", Output: "42"}}}},
			)
			provider := chatProvider(tc.url)
			var received []byte
			calls := 0
			provider.Client = &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != strings.TrimRight(tc.url, "/")+"/chat/completions" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Fatal("compatibility profile changed request destination or authentication")
				}
				var err error
				received, err = io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(streamText("42") + streamFinish("stop") + streamUsage() + streamEvent("[DONE]")))}, nil
			})}
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			response, err := prepared.Execute(t.Context(), nil)
			if err != nil || calls != 1 || len(response.Parts) != 1 || response.Parts[0].Text != "42" || response.Usage.Input == nil || *response.Usage.Input != 20 {
				t.Fatalf("response=%+v error=%v calls=%d", response, err, calls)
			}
			var wire struct {
				Model    string            `json:"model"`
				Effort   *string           `json:"reasoning_effort"`
				Cache    *string           `json:"prompt_cache_key"`
				Thinking *chatThinking     `json:"thinking"`
				Messages []chatMessage     `json:"messages"`
				Tools    []chatTool        `json:"tools"`
				Limit    int64             `json:"max_completion_tokens"`
				Stream   bool              `json:"stream"`
				Options  chatStreamOptions `json:"stream_options"`
			}
			if err := json.Unmarshal(received, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Model != request.Selection.Name || wire.Limit != 37 || !wire.Stream || !wire.Options.IncludeUsage || len(wire.Tools) != 1 || wire.Tools[0].Function.Name != "execute" {
				t.Fatalf("request contract changed: %+v", wire)
			}
			if tc.omitCache != (wire.Cache == nil) || wire.Cache != nil && *wire.Cache != string(request.SessionID) {
				t.Fatalf("incorrect cache field: %+v", wire)
			}
			if tc.disableThinking {
				if wire.Effort != nil || wire.Thinking == nil || wire.Thinking.Type != "disabled" {
					t.Fatalf("unreplayable thinking enabled: %+v", wire)
				}
			} else if wire.Effort == nil || *wire.Effort != "high" || wire.Thinking != nil {
				t.Fatalf("generic reasoning contract changed: %+v", wire)
			}
			call, result := wire.Messages[len(wire.Messages)-2], wire.Messages[len(wire.Messages)-1]
			if call.Role != "assistant" || len(call.ToolCalls) != 1 || call.ToolCalls[0].Function.Arguments != `{"code":"print(42)"}` || result.Role != "tool" || result.ToolCallID != "call" || result.Content != "42" {
				t.Fatalf("tool history changed: %+v %+v", call, result)
			}
			assertChatWireEvidence(t, prepared, request.Selection, received)
		})
	}
}

func TestChatCacheKeyAndOffEffortAreFrozenAtPreparation(t *testing.T) {
	for _, id := range []session.SessionID{"session", session.SessionID(strings.Repeat("s", 128))} {
		t.Run(string(id), func(t *testing.T) {
			request := chatRequest()
			request.SessionID = id
			request.Selection.Effort = "off"
			selection := request.Selection
			route := Route{URL: "https://example.test/v1", Credential: "test-secret", MaxOutputTokens: 100, TimeoutMillis: 1000, MaxAttempts: 3}
			provider := OpenAI{Resolve: func(context.Context, session.ModelSelection) (Route, error) { return route, nil }}
			var received []byte
			provider.Client = &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.String() != "https://example.test/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-secret" {
					t.Fatal("prepared route or credential changed")
				}
				var err error
				received, err = io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))}, nil
			})}
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			request.TurnID = "next_turn"
			request.Purpose = "compaction"
			next, err := provider.Prepare(t.Context(), request)
			if err != nil || next.Snapshot.RequestDigest != prepared.Snapshot.RequestDigest {
				t.Fatalf("same-session cache affinity changed across turns/purposes: %v", err)
			}
			request.SessionID = "different_session"
			other, err := provider.Prepare(t.Context(), request)
			if err != nil || other.Snapshot.RequestDigest == prepared.Snapshot.RequestDigest {
				t.Fatalf("different sessions shared a cache key: %v", err)
			}
			request.Selection.Effort = "high"
			request.Messages[0].Parts[0].Text = "changed"
			route.URL, route.Credential = "https://api.deepseek.com", "replacement-secret"
			for range 2 {
				if _, err := prepared.Execute(t.Context(), nil); err != nil {
					t.Fatal(err)
				}
				assertChatWireEvidence(t, prepared, selection, received)
				var wire map[string]json.RawMessage
				if err := json.Unmarshal(received, &wire); err != nil {
					t.Fatal(err)
				}
				if _, exists := wire["reasoning_effort"]; exists {
					t.Fatal("off effort must omit the wire parameter")
				}
				var key string
				if err := json.Unmarshal(wire["prompt_cache_key"], &key); err != nil {
					t.Fatal(err)
				}
				want := string(id)
				if len(want) > 64 {
					digest := sha256.Sum256([]byte(want))
					want = hex.EncodeToString(digest[:])
				}
				if key != want || len(key) > 64 {
					t.Fatalf("cache key=%q want=%q", key, want)
				}
			}
		})
	}
}

func assertChatWireEvidence(t *testing.T, prepared Prepared, selection session.ModelSelection, body []byte) {
	t.Helper()
	digest := sha256.Sum256(body)
	if prepared.Snapshot.RequestDigest != hex.EncodeToString(digest[:]) || prepared.Snapshot.Model != selection {
		t.Fatal("durable evidence lost the exact wire digest or original model selection")
	}
	evidence, err := json.Marshal(prepared.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("secret")) || bytes.Contains(evidence, []byte("secret")) {
		t.Fatal("credential leaked into the request body or durable evidence")
	}
}
