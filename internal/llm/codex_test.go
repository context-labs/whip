package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/codexauth"
)

func TestCodexStreamRequestAndEvents(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/codex/responses" {
			http.Error(w, "wrong route", http.StatusNotFound)
			return
		}
		for header, expected := range map[string]string{
			"Authorization":      "Bearer access",
			"ChatGPT-Account-ID": "account",
			"Originator":         "whip",
			"OpenAI-Beta":        "responses=experimental",
			"Accept":             "text/event-stream",
			"Content-Type":       "application/json",
		} {
			if value := r.Header.Get(header); value != expected {
				http.Error(w, header+" = "+value, http.StatusBadRequest)
				return
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"plan\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\",\"encrypted_content\":\"encrypted\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"message\",\"id\":\"msg-1\",\"phase\":\"commentary\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"done\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"fc-1\",\"call_id\":\"call-1\",\"name\":\"bash\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.function_call_arguments.delta\",\"call_id\":\"call-1\",\"delta\":\"{\\\"command\\\":\\\"p\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.function_call_arguments.done\",\"call_id\":\"call-1\",\"arguments\":\"{\\\"command\\\":\\\"pwd\\\"}\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":7,\"input_tokens_details\":{\"cached_tokens\":5}}}}\n\n")
	}))
	defer srv.Close()

	tool := NewTool("bash", "run a command", `{"type":"object"}`)
	var text, think strings.Builder
	msg, usage, err := NewCodex(srv.URL, codexSource(t)).Stream(context.Background(), Request{
		Model:           "gpt-5.4",
		MaxTokens:       128000,
		ReasoningEffort: "high",
		Messages: []Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "inspect the repository"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "old-call", ItemID: "fc-old", Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read", Arguments: `{"path":"README.md"}`}}}},
			{Role: "tool", ToolCallID: "old-call", Name: "read", Content: "file contents"},
		},
		Tools: []Tool{tool},
	}, func(delta string) { text.WriteString(delta) }, func(delta string) { think.WriteString(delta) })
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "done" || text.String() != "done" || think.String() != "plan" {
		t.Fatalf("message streams: msg=%+v text=%q think=%q", msg, text.String(), think.String())
	}
	if msg.ResponseID != "msg-1" {
		t.Fatalf("response message ID = %q", msg.ResponseID)
	}
	if msg.ResponsePhase != "commentary" {
		t.Fatalf("response message phase = %q", msg.ResponsePhase)
	}
	if len(msg.CodexReasoning) != 1 || !strings.Contains(string(msg.CodexReasoning[0]), `"encrypted_content":"encrypted"`) {
		t.Fatalf("Codex reasoning = %s", msg.CodexReasoning)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call-1" || msg.ToolCalls[0].Function.Name != "bash" || msg.ToolCalls[0].Function.Arguments != `{"command":"pwd"}` {
		t.Fatalf("tool calls: %+v", msg.ToolCalls)
	}
	if msg.ToolCalls[0].ItemID != "fc-1" {
		t.Fatalf("tool call item ID = %q", msg.ToolCalls[0].ItemID)
	}
	if usage.PromptTokens != 12 || usage.CompletionTokens != 7 || usage.Cached() != 5 {
		t.Fatalf("usage: %+v", usage)
	}
	if got["model"] != "gpt-5.4" || got["instructions"] != "system prompt" || got["stream"] != true || got["store"] != false || got["tool_choice"] != "auto" || got["parallel_tool_calls"] != true {
		t.Fatalf("request = %#v", got)
	}
	if include, ok := got["include"].([]any); !ok || len(include) != 1 || include[0] != "reasoning.encrypted_content" {
		t.Fatalf("include = %#v", got["include"])
	}
	if _, ok := got["max_output_tokens"]; ok {
		t.Fatalf("Codex subscription request must omit max_output_tokens: %#v", got)
	}
	reasoning, ok := got["reasoning"].(map[string]any)
	if !ok || reasoning["effort"] != "high" {
		t.Fatalf("reasoning = %#v", got["reasoning"])
	}
	input, ok := got["input"].([]any)
	if !ok || len(input) != 3 {
		t.Fatalf("input = %#v", got["input"])
	}
	if input[1].(map[string]any)["type"] != "function_call" || input[2].(map[string]any)["type"] != "function_call_output" {
		t.Fatalf("tool history input = %#v", input)
	}
	tools, ok := got["tools"].([]any)
	if !ok || len(tools) != 1 || tools[0].(map[string]any)["name"] != "bash" {
		t.Fatalf("tools = %#v", got["tools"])
	}
}

func TestCodexStreamSkipsMalformedSSEEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {not JSON}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"still works\"}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	msg, _, err := NewCodex(srv.URL, codexSource(t)).Stream(context.Background(), Request{Model: "gpt-5.4"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content != "still works" {
		t.Fatalf("stream content = %q, want a valid event after malformed SSE to be retained", msg.Content)
	}
}

func TestCodexStreamPropagatesRequestCancellation(t *testing.T) {
	started := make(chan struct{})
	client := NewCodex("https://codex.test", codexSource(t))
	client.HTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, _, err := client.Stream(ctx, Request{Model: "gpt-5.4"}, nil, nil)
		errCh <- err
	}()
	<-started
	cancel()

	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("Stream() error = %v, want context cancellation", err)
	}
}

func TestCodexStreamKeepsInterleavedToolCallsCorrelated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"fc-read\",\"call_id\":\"call-read\",\"name\":\"read\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"id\":\"fc-search\",\"call_id\":\"call-search\",\"name\":\"search\"}}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.function_call_arguments.delta\",\"call_id\":\"call-read\",\"delta\":\"{\\\"path\\\":\\\"REA\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.function_call_arguments.delta\",\"call_id\":\"call-search\",\"delta\":\"{\\\"query\\\":\\\"code\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.function_call_arguments.done\",\"call_id\":\"call-search\",\"arguments\":\"{\\\"query\\\":\\\"codex\\\"}\"}\n\n")
		fmt.Fprint(w, "data: {\"type\":\"response.function_call_arguments.done\",\"call_id\":\"call-read\",\"arguments\":\"{\\\"path\\\":\\\"README.md\\\"}\"}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	msg, _, err := NewCodex(srv.URL, codexSource(t)).Stream(context.Background(), Request{Model: "gpt-5.4"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("tool calls = %+v, want two calls", msg.ToolCalls)
	}
	for index, want := range []struct {
		id, itemID, name, arguments string
	}{
		{id: "call-read", itemID: "fc-read", name: "read", arguments: `{"path":"README.md"}`},
		{id: "call-search", itemID: "fc-search", name: "search", arguments: `{"query":"codex"}`},
	} {
		call := msg.ToolCalls[index]
		if call.ID != want.id || call.ItemID != want.itemID || call.Function.Name != want.name || call.Function.Arguments != want.arguments {
			t.Fatalf("tool call = %+v, want id=%q item=%q name=%q arguments=%q", call, want.id, want.itemID, want.name, want.arguments)
		}
	}
}

func TestCodexComplete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(body), `"stream":true`) {
			http.Error(w, "complete streamed", http.StatusBadRequest)
			return
		}
		if strings.Contains(string(body), `"max_output_tokens"`) {
			http.Error(w, "max_output_tokens is not accepted by Codex subscriptions", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, `{"output":[{"type":"message","content":[{"type":"output_text","text":"summary"}]}],"usage":{"input_tokens":9,"output_tokens":2}}`)
	}))
	defer srv.Close()

	text, usage, err := NewCodex(srv.URL, codexSource(t)).Complete(context.Background(), Request{
		Model:    "gpt-5.4",
		Messages: []Message{{Role: "system", Content: "summarize"}, {Role: "user", Content: "history"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "summary" || usage.PromptTokens != 9 || usage.CompletionTokens != 2 {
		t.Fatalf("complete = %q, %+v", text, usage)
	}
}

// Codex's Responses endpoint distinguishes a previous assistant output from a
// new input message. In particular, output_text belongs to a completed message
// item with an ID; sending it as a bare assistant message is rejected by the
// subscription backend as an unknown content parameter.
func TestCodexRequestUsesOutputMessageForAssistantHistory(t *testing.T) {
	call := ToolCall{ID: "call-1", ItemID: "fc-1", Type: "function"}
	call.Function.Name = "read"
	call.Function.Arguments = `{"path":"README.md"}`
	body := codexRequest(Request{
		Model: "gpt-5.6-terra",
		Messages: []Message{
			{Role: "system", Content: "system prompt"},
			{Role: "user", Content: "inspect the repository"},
			{Role: "assistant", Content: "I will inspect it.", ResponseID: "msg-1", ResponsePhase: "commentary", CodexReasoning: []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"rs-1","encrypted_content":"encrypted"}`)}, ToolCalls: []ToolCall{call}},
			{Role: "tool", ToolCallID: "call-1", Content: "README contents"},
		},
	}, true)

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]any)
	if len(input) != 5 {
		t.Fatalf("input = %#v", input)
	}
	user := input[0].(map[string]any)
	if user["role"] != "user" {
		t.Fatalf("user input = %#v", user)
	}
	if _, ok := user["type"]; ok {
		t.Fatalf("user input must not carry a type discriminator: %#v", user)
	}
	reasoning := input[1].(map[string]any)
	if reasoning["type"] != "reasoning" || reasoning["id"] != "rs-1" || reasoning["encrypted_content"] != "encrypted" {
		t.Fatalf("reasoning history = %#v", reasoning)
	}
	assistant := input[2].(map[string]any)
	if assistant["type"] != "message" || assistant["role"] != "assistant" {
		t.Fatalf("assistant history = %#v", assistant)
	}
	if assistant["id"] != "msg-1" || assistant["status"] != "completed" {
		t.Fatalf("assistant output identity = %#v", assistant)
	}
	if assistant["phase"] != "commentary" {
		t.Fatalf("assistant output phase = %#v", assistant)
	}
	content, ok := assistant["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("assistant content = %#v", assistant["content"])
	}
	text := content[0].(map[string]any)
	if text["type"] != "output_text" || text["text"] != "I will inspect it." {
		t.Fatalf("assistant text = %#v", text)
	}
	if annotations, ok := text["annotations"].([]any); !ok || len(annotations) != 0 {
		t.Fatalf("assistant annotations = %#v", text["annotations"])
	}
	callItem := input[3].(map[string]any)
	if callItem["type"] != "function_call" || callItem["id"] != "fc-1" || callItem["call_id"] != "call-1" {
		t.Fatalf("assistant tool call = %#v", callItem)
	}
	if _, ok := callItem["content"]; ok {
		t.Fatalf("function call must not carry content: %#v", callItem)
	}
}

func TestCodexMessageID(t *testing.T) {
	for _, tt := range []struct {
		name string
		msg  Message
		want string
	}{
		{name: "provider ID", msg: Message{ResponseID: "msg-provider"}, want: "msg-provider"},
		{name: "older session", want: "msg_3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := codexMessageID(tt.msg, 3); got != tt.want {
				t.Fatalf("codexMessageID = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCodexRequestFlattensLegacyToolHistory(t *testing.T) {
	legacy := ToolCall{ID: "call-old", Type: "function"}
	legacy.Function.Name = "read"
	legacy.Function.Arguments = `{"path":"README.md"}`
	body := codexRequest(Request{
		Model: "gpt-5.6-terra",
		Messages: []Message{
			{Role: "user", Content: "inspect the repository"},
			{Role: "assistant", ToolCalls: []ToolCall{legacy}},
			{Role: "tool", ToolCallID: "call-old", Content: "README contents"},
			{Role: "user", Content: "continue"},
		},
	}, true)

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]any)
	if len(input) != 3 {
		t.Fatalf("input = %#v", input)
	}
	for _, item := range input {
		message := item.(map[string]any)
		if _, ok := message["type"]; ok {
			t.Fatalf("legacy history must not emit native Responses items: %#v", message)
		}
	}
	legacyContext := input[1].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if legacyContext != "[Earlier tool activity]\n\n[Tool call]\nread({\"path\":\"README.md\"})\n\n[Tool result]\nREADME contents" {
		t.Fatalf("legacy context = %q", legacyContext)
	}
}

func TestCodexRequestReplaysMalformedLegacyToolArguments(t *testing.T) {
	legacy := ToolCall{ID: "call-old", Type: "function"}
	legacy.Function.Name = "bash"
	legacy.Function.Arguments = `{"command":`
	body := codexRequest(Request{
		Model: "gpt-5.6-terra",
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{legacy}},
			{Role: "tool", ToolCallID: "call-old", Content: "tool rejected invalid arguments"},
		},
	}, true)

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]any)
	if len(input) != 1 {
		t.Fatalf("input = %#v", input)
	}
	context := input[0].(map[string]any)["content"].([]any)[0].(map[string]any)["text"]
	if context != "[Earlier tool activity]\n\n[Tool call]\nbash({\"command\":)\n\n[Tool result]\ntool rejected invalid arguments" {
		t.Fatalf("legacy context = %q", context)
	}
}

func TestCodexRequestCorrelatesMultipleToolResults(t *testing.T) {
	read := ToolCall{ID: "call-read", ItemID: "fc-read", Type: "function"}
	read.Function.Name = "read"
	read.Function.Arguments = `{"path":"README.md"}`
	search := ToolCall{ID: "call-search", ItemID: "fc-search", Type: "function"}
	search.Function.Name = "search"
	search.Function.Arguments = `{"query":"codex"}`
	body := codexRequest(Request{
		Model: "gpt-5.6-terra",
		Messages: []Message{
			{Role: "assistant", ToolCalls: []ToolCall{read, search}},
			{Role: "tool", ToolCallID: "call-read", Content: "README"},
			{Role: "tool", ToolCallID: "call-search", Content: "matches"},
		},
	}, true)

	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	input := got["input"].([]any)
	if len(input) != 4 {
		t.Fatalf("input = %#v", input)
	}
	for index, want := range []struct {
		typeName string
		callID   string
		itemID   string
		output   string
	}{
		{typeName: "function_call", callID: "call-read", itemID: "fc-read"},
		{typeName: "function_call", callID: "call-search", itemID: "fc-search"},
		{typeName: "function_call_output", callID: "call-read", output: "README"},
		{typeName: "function_call_output", callID: "call-search", output: "matches"},
	} {
		item := input[index].(map[string]any)
		if item["type"] != want.typeName || item["call_id"] != want.callID || (want.itemID != "" && item["id"] != want.itemID) || (want.output != "" && item["output"] != want.output) {
			t.Fatalf("input[%d] = %#v, want type=%q call_id=%q item_id=%q output=%q", index, item, want.typeName, want.callID, want.itemID, want.output)
		}
	}
}

func TestCodexModelsFetchesAccountCatalog(t *testing.T) {
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/codex/models" {
			http.Error(w, "wrong route", http.StatusNotFound)
			return
		}
		if got := r.URL.Query().Get("client_version"); got != codexModelsClientVersion {
			http.Error(w, "missing client_version", http.StatusBadRequest)
			return
		}
		gotHeaders = r.Header.Clone()
		fmt.Fprint(w, `{"models":[
  {"slug":"gpt-5.6-sol","supported_in_api":true,"context_window":1050000,"supported_reasoning_levels":[{"effort":"none"},{"effort":"low"},{"effort":"max"}],"input_modalities":["text","image"]},
  {"slug":"gpt-rollout","supported_in_api":false,"context_window":1000},
  {"slug":"gpt-5.4","supported_in_api":true,"max_context_window":272000,"supported_reasoning_levels":[{"effort":"medium"}]}
]}`)
	}))
	defer srv.Close()

	models, err := NewCodex(srv.URL, codexSource(t)).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotHeaders.Get("Authorization") != "Bearer access" || gotHeaders.Get("Chatgpt-Account-Id") != "account" || gotHeaders.Get("Originator") != "whip" {
		t.Fatalf("catalog auth headers = %#v", gotHeaders)
	}
	if len(models) != 2 {
		t.Fatalf("models = %+v, want two supported entries", models)
	}
	if got := models[0]; got.ID != "gpt-5.6-sol" || got.ContextLength != 1050000 || !got.SupportsVision() || strings.Join(got.ReasoningEfforts, ",") != "none,low,max" {
		t.Fatalf("first model = %+v", got)
	}
	if got := models[1]; got.ID != "gpt-5.4" || got.ContextLength != 272000 || !got.SupportsVision() || strings.Join(got.ReasoningEfforts, ",") != "medium" {
		t.Fatalf("second model = %+v", got)
	}
}

func TestCodexModelsReturnsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not entitled", http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := NewCodex(srv.URL, codexSource(t)).Models(context.Background())
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.Status != "403 Forbidden" || !strings.Contains(httpErr.Body, "not entitled") {
		t.Fatalf("error = %#v, want typed 403", err)
	}
}

func TestCodexModelsFailureModes(t *testing.T) {
	if _, err := NewCodex("https://codex.test", nil).Models(context.Background()); !errors.Is(err, codexauth.ErrLoginRequired) {
		t.Fatalf("nil source error = %v, want login required", err)
	}

	client := NewCodex("https://codex.test", codexSource(t))
	client.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	if _, err := client.Models(context.Background()); err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("transport error = %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[{"slug":"","supported_in_api":true},{"slug":"rollout","supported_in_api":false}]}`))
	}))
	defer srv.Close()
	models, err := NewCodex(srv.URL, codexSource(t)).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 0 {
		t.Fatalf("unsupported catalog entries = %+v, want none", models)
	}
	if efforts := (codexModel{SupportedReasoningLevels: []codexReasoningLevel{{}, {Effort: "high"}}}).ReasoningEfforts(); len(efforts) != 1 || efforts[0] != "high" {
		t.Fatalf("reasoning efforts = %v", efforts)
	}
}

func TestCodexStreamAndCompleteRequireLogin(t *testing.T) {
	client := NewCodex("https://codex.test", nil)
	if _, _, err := client.Stream(context.Background(), Request{Model: "gpt-5.4"}, nil, nil); !errors.Is(err, codexauth.ErrLoginRequired) {
		t.Fatalf("Stream() error = %v, want login required", err)
	}
	if _, _, err := client.Complete(context.Background(), Request{Model: "gpt-5.4"}); !errors.Is(err, codexauth.ErrLoginRequired) {
		t.Fatalf("Complete() error = %v, want login required", err)
	}
}

func TestCodexModelsRejectsMalformedCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":`))
	}))
	defer srv.Close()

	if _, err := NewCodex(srv.URL, codexSource(t)).Models(context.Background()); err == nil {
		t.Fatal("malformed catalog was accepted")
	}
}

func TestCallCollectorHandlesPartialAndCompletedCalls(t *testing.T) {
	collector := callCollector{}
	collector.delta("", `{"ignored":true}`)
	collector.delta("call-1", `{"path":"REA`)
	collector.addAll([]responseItem{
		{Type: "message", ID: "msg-1"},
		{Type: "function_call", CallID: "call-1", ID: "fc-1", Name: "read", Arguments: `{"path":"README.md"}`},
		{Name: "unnamed"},
	})

	if len(collector.calls) != 2 {
		t.Fatalf("calls = %+v, want two calls", collector.calls)
	}
	if got := collector.calls[0]; got.ID != "call-1" || got.ItemID != "fc-1" || got.Function.Name != "read" || got.Function.Arguments != `{"path":"README.md"}` {
		t.Fatalf("completed call = %+v", got)
	}
	if got := collector.calls[1]; got.ID != "output-0" || got.Function.Name != "unnamed" {
		t.Fatalf("unnamed call = %+v", got)
	}
}

func TestCodexStreamErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		want       string
		wantStatus string
	}{
		{name: "HTTP error", status: http.StatusForbidden, body: "not entitled", want: "403 Forbidden", wantStatus: "403 Forbidden"},
		{name: "API error", status: http.StatusOK, body: "data: {\"type\":\"error\",\"error\":{\"message\":\"quota reached\"}}\n\n", want: "quota reached"},
		{name: "failed response", status: http.StatusOK, body: "data: {\"type\":\"response.failed\"}\n\n", want: "codex response failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, _, err := NewCodex(srv.URL, codexSource(t)).Stream(context.Background(), Request{Model: "gpt-5.4"}, nil, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Stream error = %v, want %q", err, tc.want)
			}
			if tc.wantStatus != "" {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.Status != tc.wantStatus || !strings.Contains(httpErr.Body, tc.body) {
					t.Fatalf("Stream error = %#v, want typed HTTP error %q containing %q", err, tc.wantStatus, tc.body)
				}
			}
		})
	}
}

func TestCodexCompleteErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "HTTP error", status: http.StatusUnauthorized, body: "sign in", want: "401 Unauthorized"},
		{name: "invalid JSON", status: http.StatusOK, body: "not json", want: "invalid character"},
		{name: "no text", status: http.StatusOK, body: `{"output":[{"type":"message","content":[{"type":"refusal","text":"no"}]}]}`, want: "no text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, _, err := NewCodex(srv.URL, codexSource(t)).Complete(context.Background(), Request{Model: "gpt-5.4"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Complete error = %v, want %q", err, tc.want)
			}
		})
	}
}

func codexSource(t *testing.T) *codexauth.Source {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"tokens":{"access_token":"access","refresh_token":"refresh","account_id":"account"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return &codexauth.Source{HomeDir: home}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
