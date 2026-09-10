package llm_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
)

type compatibilityTransport func(*http.Request) (*http.Response, error)

func (f compatibilityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// These fixtures verify Whip's contract for each preset destination, not live
// provider availability. They exercise streamed arguments and the next request's
// tool-result history without issuing a network request or billable inference.
func TestPresetChatToolRoundTrip(t *testing.T) {
	for _, preset := range config.ProviderPresets() {
		if preset.ID == openaiauth.Provider {
			continue
		}
		t.Run(preset.ID, func(t *testing.T) {
			model := config.PresetModels(preset.ID)[0]
			if preset.ID == "openai" {
				model = config.PresetModels(preset.ID)[1] // Astra uses the separately tested Responses transport.
			}
			client := llm.New(preset.Provider.BaseURL, "fixture-only-key")
			client.CacheKey = "fixture-session"
			calls := 0
			client.HTTP = &http.Client{Transport: compatibilityTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != preset.Provider.BaseURL+"/chat/completions" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-only-key" {
					t.Fatal("incorrect request destination or authentication")
				}
				var body struct {
					llm.Request
					Thinking struct {
						Type string `json:"type"`
					} `json:"thinking"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Model != model.ID || body.MaxTokens != 1024 {
					t.Fatalf("model/output request = %+v", body.Request)
				}
				if preset.ID == "deepseek" && (body.Thinking.Type != "disabled" || body.ReasoningEffort != "") {
					t.Fatal("DeepSeek enabled unreplayable thinking history")
				}
				if len(model.ReasoningEfforts) > 0 && body.ReasoningEffort != model.ReasoningEfforts[0] {
					t.Fatal("reasoning effort changed")
				}
				switch preset.ID {
				case "cerebras", "groq", "deepseek", "fireworks-ai", "togetherai", "deepinfra":
					if body.PromptCacheKey != "" {
						t.Fatal("undocumented explicit cache-key parameter sent")
					}
				}
				var response string
				if calls == 1 {
					if !body.Stream || body.StreamOptions == nil || !body.StreamOptions.IncludeUsage || len(body.Tools) != 1 || body.Tools[0].Function.Name != "rlm_exec" {
						t.Fatal("stream/tool contract changed")
					}
					response = "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"rlm_exec\",\"arguments\":\"{\\\"code\\\":\\\"\"}}]}}]}\n\n" +
						"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"6 * 7\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n"
				} else {
					last := body.Messages[len(body.Messages)-1]
					if last.Role != "tool" || last.ToolCallID != "call-1" || last.Name != "rlm_exec" || last.Content != `{"value":42,"steps":1}` {
						t.Fatalf("tool result was not retained: %+v", last)
					}
					if calls == 2 {
						response = "data: {\"choices\":[{\"delta\":{\"content\":\"42\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":20,\"completion_tokens\":1}}\n\ndata: [DONE]\n\n"
					} else {
						if body.Stream || body.StreamOptions != nil {
							t.Fatal("non-stream helper sent stream options")
						}
						response = `{"choices":[{"message":{"content":"42"}}],"usage":{"prompt_tokens":20,"completion_tokens":1}}`
					}
				}
				return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			request := llm.Request{Model: model.ID, MaxTokens: 1024, Messages: []llm.Message{{Role: "user", Content: "Calculate 6 * 7"}}, Tools: []llm.Tool{llm.NewTool("rlm_exec", "Run code", `{"type":"object","properties":{"code":{"type":"string"}}}`)}}
			if len(model.ReasoningEfforts) > 0 {
				request.ReasoningEffort = model.ReasoningEfforts[0]
			}
			assistant, usage, err := client.Stream(t.Context(), request, nil, nil, nil)
			if err != nil || len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Function.Arguments != `{"code":"6 * 7"}` || usage.CompletionTokens != 5 {
				t.Fatalf("tool stream = %+v %+v %v", assistant, usage, err)
			}
			request.Messages = append(request.Messages, assistant, llm.Message{Role: "tool", ToolCallID: "call-1", Content: `{"value":42,"steps":1}`})
			assistant, usage, err = client.Stream(t.Context(), request, nil, nil, nil)
			if err != nil || assistant.Content != "42" || usage.PromptTokens != 20 {
				t.Fatalf("tool result stream: %+v %+v %v", assistant, usage, err)
			}
			text, usage, err := client.Complete(t.Context(), request)
			if err != nil || text != "42" || usage.CompletionTokens != 1 || calls != 3 {
				t.Fatalf("completion = %q %+v %v (%d calls)", text, usage, err, calls)
			}
		})
	}
}

func TestCustomEndpointKeepsGenericChatRequest(t *testing.T) {
	client := llm.New("https://api.deepseek.com/custom/v1", "key")
	client.HTTP = &http.Client{Transport: compatibilityTransport(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "thinking") || !strings.Contains(string(body), `"reasoning_effort":"high"`) || !strings.Contains(string(body), `"prompt_cache_key":"explicit"`) {
			t.Fatalf("custom route was rewritten: %s", body)
		}
		return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`))}, nil
	})}
	_, _, err := client.Complete(t.Context(), llm.Request{Model: "deepseek-v4-flash", ReasoningEffort: "high", PromptCacheKey: "explicit"})
	if err != nil {
		t.Fatal(fmt.Errorf("custom request: %w", err))
	}
}
