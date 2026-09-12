package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
)

func responseFixture(items string) string {
	return `{"type":"response.completed","response":{"status":"completed","output":` + items +
		`,"usage":{"input_tokens":17,"output_tokens":9,"input_tokens_details":{"cached_tokens":5},"output_tokens_details":{"reasoning_tokens":3}}}}`
}

func decodeFixture(stream string) (Message, Usage, error) {
	return decodeResponses(strings.NewReader(stream), "account", "model", func(string) {}, func(string) {}, func(string, string, string) {})
}

func TestResponsesToolContinuationRoundTrip(t *testing.T) {
	items := `[{"type":"reasoning","id":"r1","encrypted_content":"opaque-state","summary":[]},` +
		`{"type":"message","id":"m1","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking."}]},` +
		`{"type":"function_call","id":"fc1","call_id":"call1","name":"rlm_exec","arguments":"{\"code\":\"print(1)\"}"}]`
	stream := "event: response.output_item.added\ndata: " +
		`{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"call1","name":"rlm_exec","arguments":""}}` +
		"\n\ndata: " + `{"type":"response.function_call_arguments.delta","output_index":2,"delta":"{\"code\":"}` +
		"\n\ndata: " + `{"type":"response.function_call_arguments.delta","output_index":2,"delta":"\"print(1)\"}"}` +
		"\n\ndata: " + `{"type":"response.output_text.delta","delta":"Checking."}` +
		"\n\ndata: " + responseFixture(items) + "\n\n"
	var text string
	var args []string
	message, usage, err := decodeResponses(strings.NewReader(stream), "account", "model", func(delta string) {
		text += delta
	}, func(string) {}, func(_, _, snapshot string) { args = append(args, snapshot) })
	if err != nil || message.Content != "Checking." || text != message.Content || len(message.ToolCalls) != 1 {
		t.Fatalf("decode: message=%+v text=%q err=%v", message, text, err)
	}
	if len(args) != 4 || args[1] != `{"code":` || args[2] != `{"code":"print(1)"}` {
		t.Fatalf("tool arguments are not cumulative: %v", args)
	}
	if !usage.HasUsage() || usage.PromptTokens != 17 || usage.CompletionTokens != 9 || usage.Cached() != 5 || usage.Cost != nil {
		t.Fatalf("incorrect subscription usage: %+v", usage)
	}
	stored, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(stored, &restored); err != nil {
		t.Fatal(err)
	}
	req := Request{Model: "model", Messages: []Message{
		{Role: "system", Content: "WHIP instructions"},
		{Role: "user", Content: "Do work"},
		restored,
		{Role: "tool", ToolCallID: "call1", Content: "1"},
	}}
	for round := range 2 {
		body, err := encodeResponses(req, "account")
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			Instructions string            `json:"instructions"`
			Input        []json.RawMessage `json:"input"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			t.Fatal(err)
		}
		if wire.Instructions != "WHIP instructions" || len(wire.Input) != 5+round*2 ||
			strings.Count(string(body), "opaque-state") != 1 || !strings.Contains(string(body), `"phase":"commentary"`) {
			t.Fatalf("continuation duplicated or lost: %s", body)
		}
		next, _, err := decodeFixture("data: " + responseFixture(
			`[{"type":"function_call","call_id":"call2","name":"rlm_exec","arguments":"{\"code\":\"print(2)\"}"}]`,
		) + "\n\n")
		if err != nil {
			t.Fatal(err)
		}
		req.Messages = append(req.Messages, next, Message{Role: "tool", ToolCallID: "call2", Content: "2"})
	}
	for _, scope := range []struct{ account, model string }{{"other-account", "model"}, {"account", "other-model"}} {
		req.Model = scope.model
		body, err := encodeResponses(req, scope.account)
		if err != nil || strings.Contains(string(body), "opaque-state") {
			t.Fatalf("forwarded state into another account/model: %v", err)
		}
	}
	chat, err := json.Marshal(stripAuthored([]Message{restored}))
	if err != nil || strings.Contains(string(chat), "continuation") || strings.Contains(string(chat), "opaque-state") {
		t.Fatal("forwarded Responses continuation to Chat Completions")
	}
}

func TestResponsesInputImagesAndTools(t *testing.T) {
	req := Request{
		Model: "model", ReasoningEffort: "xhigh", PromptCacheKey: "session-cache", MaxTokens: 123,
		Messages: []Message{{Role: "user", Content: "Describe", Parts: []ContentPart{ImagePart("png", []byte("image"))}}},
		Tools:    []Tool{NewTool("rlm_exec", "Run Starlark", `{"type":"object","properties":{"code":{"type":"string"}},"required":["code"]}`)},
	}
	body, err := encodeResponses(req, "account")
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(body, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["stream"] != true || wire["store"] != false || wire["max_tokens"] != nil || wire["max_output_tokens"] != nil {
		t.Fatalf("wrong subscription profile: %s", body)
	}
	if !strings.Contains(string(body), `"type":"input_image"`) || !strings.Contains(string(body), `"type":"input_text"`) ||
		!strings.Contains(string(body), `"name":"rlm_exec"`) || strings.Contains(string(body), `"function":`) {
		t.Fatalf("wrong input/tool translation: %s", body)
	}
}

func TestResponsesCompletedItemsWithoutTerminalOutput(t *testing.T) {
	items := []string{
		`{"type":"reasoning","id":"r1","encrypted_content":"opaque-state","summary":[]}`,
		`{"type":"message","id":"m1","role":"assistant","phase":"commentary","content":[{"type":"output_text","text":"Checking."}]}`,
		`{"type":"function_call","id":"fc1","call_id":"call1","name":"rlm_exec","arguments":"{\"code\":\"print(1)\"}"}`,
	}
	stream := "data: " + `{"type":"response.output_text.delta","delta":"Checking."}` + "\n\n"
	// Completion order need not match the provider's output order.
	var completedItems strings.Builder
	for _, index := range []int{2, 0, 1} {
		fmt.Fprintf(&completedItems, "data: {\"type\":\"response.output_item.done\",\"output_index\":%d,\"item\":%s}\n\n", index, items[index])
	}
	stream += completedItems.String()
	if message, _, err := decodeFixture(stream); !errors.Is(err, io.ErrUnexpectedEOF) || len(message.ToolCalls) != 0 || message.Continuation.Items != "" {
		t.Fatalf("published items before response completion: %+v %v", message, err)
	}
	var text string
	message, _, err := decodeResponses(strings.NewReader(stream+"data: "+responseFixture(`[]`)+"\n\n"), "account", "model",
		func(delta string) { text += delta }, func(string) {}, func(string, string, string) {})
	if err != nil || text != "Checking." || message.Content != text || len(message.ToolCalls) != 1 {
		t.Fatalf("lost completed output items: %+v text=%q err=%v", message, text, err)
	}
	expected := "[" + strings.Join(items, ",") + "]"
	if message.Continuation.Items != expected {
		t.Fatalf("lost opaque fields or output order: %s", message.Continuation.Items)
	}
	body, err := encodeResponses(Request{Model: "model", Messages: []Message{
		message,
		{Role: "tool", ToolCallID: "call1", Content: "1"},
	}}, "account")
	if err != nil || !strings.Contains(string(body), strings.Join(items, ",")) {
		t.Fatalf("completed items did not survive tool continuation: %s %v", body, err)
	}
}

func TestResponsesRejectMissingCompletedItem(t *testing.T) {
	for _, index := range []int{-1, 0, 1, maxResponseItems} {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			stream := fmt.Sprintf("data: {\"type\":\"response.output_item.added\",\"output_index\":%d,\"item\":{\"type\":\"function_call\",\"call_id\":\"call1\",\"name\":\"rlm_exec\"}}\n\n", index)
			message, _, err := decodeFixture(stream + "data: " + responseFixture(`[]`) + "\n\n")
			if err == nil || len(message.ToolCalls) != 0 || message.Continuation.Items != "" {
				t.Fatalf("accepted missing completed item: %+v %v", message, err)
			}
		})
	}
}

func TestResponsesNeverPublishIncompleteTools(t *testing.T) {
	for _, terminal := range []string{
		"",
		`{"type":"response.incomplete","response":{"status":"incomplete","output":[]}}`,
		`{"type":"response.failed","response":{"status":"failed","output":[]}}`,
		responseFixture(`[{"type":"function_call","call_id":"call","name":"rlm_exec","arguments":"{bad"}]`),
		responseFixture(`[{"type":"function_call","call_id":"call","name":"rlm_exec","arguments":"null"}]`),
		responseFixture(`[{"type":"computer_call","id":"call"}]`),
		`{"type":"error","message":"secret-provider-body"}`,
		`not json`,
		"[DONE]",
	} {
		t.Run(fmt.Sprintf("case-%d", len(terminal)), func(t *testing.T) {
			stream := "data: " + `{"type":"response.output_text.delta","delta":"Partial"}` + "\n\n"
			if terminal != "" {
				stream += "data: " + terminal + "\n\n"
			}
			message, _, err := decodeFixture(stream)
			if err == nil || message.Content != "Partial" || len(message.ToolCalls) != 0 || message.Continuation.Items != "" {
				t.Fatalf("published incomplete response: %+v %v", message, err)
			}
			if strings.Contains(err.Error(), "secret-provider-body") {
				t.Fatal("upstream error body leaked")
			}
		})
	}
}

func TestResponsesSSEFramingAndUsage(t *testing.T) {
	stream := ": comment\r\nevent: ignored\r\ndata: {\"type\":\"response.completed\",\r\ndata: " +
		`"response":{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}}`
	message, usage, err := decodeFixture(stream)
	if err != nil || message.Content != "ok" || usage.HasUsage() {
		t.Fatalf("multiline frame/missing usage: %+v %+v %v", message, usage, err)
	}
	for _, raw := range []string{`{"input_tokens":1}`, `{"input_tokens":-1,"output_tokens":2}`, `{"input_tokens":2,"output_tokens":1,"output_tokens_details":{"reasoning_tokens":5}}`} {
		if _, err := responseUsage(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid usage: %s", raw)
		}
	}
	if _, _, err := decodeFixture(""); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("missing completion was accepted: %v", err)
	}
}
