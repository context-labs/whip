package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

const (
	responsesOutput = `[{"type":"reasoning","encrypted_content":"opaque-secret","future":{"number":9007199254740993}},{"type":"message","id":"message-item","phase":"commentary","role":"assistant","content":[{"type":"output_text","text":"Thinking done."}]},{"type":"function_call","id":"item","call_id":"execute1","name":"execute","arguments":"{\"code\":\"print(42)\"}"}]`
	responsesUsage  = `{"input_tokens":20,"output_tokens":5,"input_tokens_details":{"cached_tokens":8},"output_tokens_details":{"reasoning_tokens":2},"cost":0.25}`
)

func responsesTerminal(output, usage string) string {
	return `{"status":"completed","output":` + output + `,"usage":` + usage + `}`
}

func responsesEvent(kind, payload string) string {
	return streamEvent(`{"type":"` + kind + `",` + payload + `}`)
}

func responsesProvider(body string) OpenAI {
	p := chatProvider("https://example.test/v1")
	resolve := p.Resolve
	p.Resolve = func(ctx context.Context, selection session.ModelSelection) (Route, error) {
		route, err := resolve(ctx, selection)
		route.Kind = "openai-responses"
		return route, err
	}
	p.Client = &http.Client{Transport: contextLimitTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	return p
}

func TestResponsesStreamContinuationAndFrozenWire(t *testing.T) {
	stream := responsesEvent("response.output_text.delta", `"delta":"Thinking "`) +
		responsesEvent("response.output_item.added", `"output_index":2,"item":{"type":"function_call","call_id":"execute1","name":"execute","arguments":""}`) +
		responsesEvent("response.function_call_arguments.delta", `"output_index":2,"delta":"{\"code\":"`) +
		responsesEvent("response.completed", `"response":`+responsesTerminal(responsesOutput, responsesUsage))
	provider := responsesProvider(stream)
	request := chatRequest()
	request.Tools = []Tool{executeTool()}
	request.Messages[0].Parts = append(request.Messages[0].Parts, session.Part{Type: "content", ReferenceID: "image"})
	request.Contents = map[string]Content{"image": {MediaType: "image/png", Data: []byte("fixture-image")}}
	transport := provider.Client.Transport
	var received []byte
	provider.Client.Transport = contextLimitTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://example.test/v1/responses" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Fatal("Responses route or authentication mismatch")
		}
		var err error
		received, err = io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		return transport.RoundTrip(r)
	})
	prepared, err := provider.Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.Selection.Effort = "off"
	request.Contents["image"].Data[0] = 'X'
	var text, arguments, callID, callName string
	response, err := prepared.Execute(t.Context(), func(chunk Chunk) {
		text += chunk.Text
		if chunk.Call != nil {
			arguments += chunk.Call.Arguments
			callID += chunk.Call.ID
			callName += chunk.Call.Name
		}
	})
	if err != nil || response.Continuation == nil || len(response.Parts) != 2 || text != "Thinking done." || arguments != `{"code":"print(42)"}` || callID != "execute1" || callName != "execute" {
		t.Fatalf("response=%+v error=%v text=%q args=%q id=%q name=%q", response, err, text, arguments, callID, callName)
	}
	digest := sha256.Sum256(received)
	if prepared.Snapshot.Adapter != "openai-responses" || prepared.Snapshot.RequestDigest != hex.EncodeToString(digest[:]) || *response.Usage.CachedInput != 8 || *response.Usage.Reasoning != 2 || *response.ReportedCostNanoUSD != 250000000 {
		t.Fatal("Responses accounting or exact request evidence lost")
	}
	for _, expected := range []string{`"max_output_tokens":100`, `"effort":"low"`, `"strict":false`, `"store":false`, `"input_image"`, `Zml4dHVyZS1pbWFnZQ==`, `"prompt_cache_key":"session"`} {
		if !strings.Contains(string(received), expected) {
			t.Fatalf("missing wire contract %s", expected)
		}
	}
	snapshot, marshalErr := json.Marshal(prepared.Snapshot)
	if marshalErr != nil || strings.Contains(string(received), "test-secret") || strings.Contains(string(snapshot), "test-secret") || strings.Contains(response.Continuation.Data, "test-secret") {
		t.Fatal("credential entered provider body or durable evidence")
	}
	request = chatRequest()
	request.Tools = []Tool{executeTool()}
	request.Messages = append(request.Messages, Message{Role: session.Assistant, Parts: response.Parts, Continuation: response.Continuation}, Message{Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "execute1", Output: "42"}}}})
	scope := response.Continuation.Scope
	for _, tc := range []struct {
		name, scope string
		edit        bool
		wantReplay  bool
	}{
		{name: "same credential route model", scope: scope, wantReplay: true},
		{name: "different credential", scope: responseScope("https://example.test/v1/responses", "other-key", "model")},
		{name: "different route", scope: responseScope("https://other.test/v1/responses", "test-secret", "model")},
		{name: "different model", scope: responseScope("https://example.test/v1/responses", "test-secret", "other-model")},
		{name: "edited visible output", scope: scope, edit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.edit {
				request.Messages[1].Parts[0].Text = "edited"
			}
			body, err := encodeResponses(request, tc.scope, 100)
			if err != nil || strings.Contains(string(body), "opaque-secret") != tc.wantReplay || !strings.Contains(string(body), "function_call_output") {
				t.Fatalf("replay=%v error=%v", strings.Contains(string(body), "opaque-secret"), err)
			}
			if tc.wantReplay {
				for _, preserved := range []string{"9007199254740993", `"id":"message-item"`, `"phase":"commentary"`} {
					if !strings.Contains(string(body), preserved) {
						t.Fatalf("private output field changed: %s", preserved)
					}
				}
			}
		})
	}
}

func TestResponsesCompletedItemsAndFailures(t *testing.T) {
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(responsesOutput), &items); err != nil {
		t.Fatal(err)
	}
	var completedItems strings.Builder
	for i, item := range items {
		raw, _ := json.Marshal(map[string]any{"type": "response.output_item.done", "output_index": i, "item": item})
		completedItems.WriteString(streamEvent(string(raw)))
	}
	for _, tc := range []struct {
		name, body string
		valid      bool
		usage      bool
	}{
		{name: "done items without terminal output", body: completedItems.String() + responsesEvent("response.completed", `"response":{"status":"completed","usage":`+responsesUsage+`}`), valid: true, usage: true},
		{name: "done items with empty terminal array", body: completedItems.String() + responsesEvent("response.completed", `"response":{"status":"completed","output":[ ],"usage":`+responsesUsage+`}`), valid: true, usage: true},
		{name: "multiline and comments", body: ": comment\r\ndata: {\"type\":\"response.completed\",\r\ndata: \"response\":" + responsesTerminal(responsesOutput, responsesUsage) + "}\r\n\r\n", valid: true, usage: true},
		{name: "done is not completion", body: completedItems.String() + streamEvent("[DONE]")},
		{name: "missing terminal", body: completedItems.String()},
		{name: "malformed status retains usage", body: responsesEvent("response.completed", `"response":{"status":17,"output":`+responsesOutput+`,"usage":`+responsesUsage+`}`), usage: true},
		{name: "malformed output retains usage", body: responsesEvent("response.completed", `"response":`+responsesTerminal(`"wrong"`, responsesUsage)), usage: true},
		{name: "failed retains usage", body: responsesEvent("response.failed", `"response":{"status":"failed","usage":`+responsesUsage+`,"error":{"code":"context_length_exceeded","message":"secret"}}`), usage: true},
		{name: "incomplete retains usage", body: responsesEvent("response.incomplete", `"response":{"status":"incomplete","usage":`+responsesUsage+`}`), usage: true},
		{name: "unknown tool", body: responsesEvent("response.completed", `"response":`+responsesTerminal(strings.ReplaceAll(responsesOutput, `"name":"execute"`, `"name":"unknown"`), responsesUsage)), usage: true},
		{name: "oversized continuation", body: responsesEvent("response.completed", `"response":`+responsesTerminal(strings.ReplaceAll(responsesOutput, "opaque-secret", strings.Repeat("x", session.MaxContinuationBytes)), responsesUsage)), usage: true},
		{name: "invalid item index", body: responsesEvent("response.output_item.added", `"output_index":999,"item":{"type":"reasoning"}`)},
		{name: "arguments without call", body: responsesEvent("response.function_call_arguments.delta", `"output_index":1,"delta":"secret"`)},
		{name: "stream text disagreement", body: responsesEvent("response.output_text.delta", `"delta":"different"`) + responsesEvent("response.completed", `"response":`+responsesTerminal(responsesOutput, responsesUsage)), usage: true},
		{name: "line flood", body: strings.Repeat(":\n", maxStreamLines+1)},
		{name: "oversized framing", body: ":" + strings.Repeat("x", maxResponseBytes)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := decodeResponsesStream(t.Context(), strings.NewReader(tc.body), strings.Repeat("a", 64), map[string]bool{"execute": true}, nil, false)
			if tc.valid {
				if err != nil || response.Continuation == nil || len(response.Parts) != 2 {
					t.Fatalf("response=%+v error=%v", response, err)
				}
			} else {
				failure, ok := errors.AsType[*CallError](err)
				if !ok || !failure.Uncertain || failure.Retryable || failure.ContextLimit || len(response.Parts) != 0 || response.Continuation != nil || strings.Contains(err.Error(), "secret") {
					t.Fatalf("unsafe failure response=%+v error=%v", response, err)
				}
			}
			if tc.usage && (response.Usage.Input == nil || *response.Usage.Input != 20 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000) {
				t.Fatal("known accounting lost")
			}
		})
	}
}

func TestResponsesHTTPFailureAndFallback(t *testing.T) {
	for _, tc := range []struct {
		name                                                           string
		status                                                         int
		body                                                           string
		contentType                                                    string
		broken, transport, cancelled, contextLimit, retryable, success bool
	}{
		{name: "JSON fallback", status: 200, body: responsesTerminal(responsesOutput, responsesUsage), success: true},
		{name: "confirmed context rejection", status: 400, body: `{"error":{"code":"context_length_exceeded","message":"secret"},"usage":` + responsesUsage + `}`, contextLimit: true},
		{name: "untyped rejection", status: 413, body: `{"error":{"message":"prompt_too_long secret"}}`},
		{name: "SSE HTTP rejection", status: 400, body: `{"error":{"type":"prompt_too_long"}}`, contentType: "text/event-stream"},
		{name: "retryable rejection", status: 429, body: `{"error":"secret"}`, retryable: true},
		{name: "partial response", status: 400, body: `{"error":{"code":"context_length_exceeded"}}`, broken: true},
		{name: "transport failure", transport: true},
		{name: "cancelled", cancelled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			provider := responsesProvider("")
			provider.Client.Transport = contextLimitTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.transport {
					return nil, errors.New("secret transport")
				}
				var body io.Reader = strings.NewReader(tc.body)
				if tc.broken {
					body = streamBrokenReader{body}
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: io.NopCloser(body)}, nil
			})
			request := chatRequest()
			request.Tools = []Tool{executeTool()}
			prepared, err := provider.Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancelled {
				cancel()
			}
			response, err := prepared.Execute(ctx, nil)
			if tc.success {
				if err != nil || response.Continuation == nil {
					t.Fatalf("fallback: %v", err)
				}
				return
			}
			if tc.cancelled {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation: %v", err)
				}
				return
			}
			failure, ok := errors.AsType[*CallError](err)
			if !ok || failure.ContextLimit != tc.contextLimit || failure.Retryable != tc.retryable || calls != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatalf("failure=%+v calls=%d", err, calls)
			}
			if tc.contextLimit && (response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000) {
				t.Fatal("rejection lost usage")
			}
		})
	}
}
