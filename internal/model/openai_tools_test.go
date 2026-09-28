package model

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func executeTool() Tool {
	return Tool{Name: "execute", Description: "Execute a code cell.", InputSchema: json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"}},"required":["code"],"additionalProperties":false}`)}
}

func TestChatToolDeclarationsCallsAndResultsPreserveDurableMeaning(t *testing.T) {
	var received []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		received, err = io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"Calculating.","tool_calls":[{"id":"next_call","type":"function","function":{"name":"execute","arguments":"{\"code\":\"print(2)\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":5,"cost":0.01}}`)
	}))
	defer server.Close()
	request := chatRequest()
	request.Tools = []Tool{executeTool()}
	request.Messages = append(request.Messages,
		Message{Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "prior_call", Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}}},
		Message{Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "prior_call", Output: "1", IsError: false}}}},
	)
	prepared, err := chatProvider(server.URL).Prepare(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	// Mutating declarations after preparation cannot change either the request
	// body or the set of names accepted from its response.
	request.Tools[0].Name = "different"
	request.Tools[0].InputSchema[0] = '['
	response, err := prepared.Execute(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Parts) != 2 || response.Parts[0].Text != "Calculating." || response.Parts[1].Call.ID != "next_call" || response.Parts[1].Call.Name != "execute" || string(response.Parts[1].Call.Arguments) != `{"code":"print(2)"}` {
		t.Fatalf("decoded parts: %+v", response.Parts)
	}
	if response.Usage.Input == nil || *response.Usage.Input != 20 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 10000000 {
		t.Fatalf("tool response lost accounting: %+v", response)
	}
	var wire struct {
		Messages []chatMessage `json:"messages"`
		Tools    []chatTool    `json:"tools"`
	}
	if err := json.Unmarshal(received, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Tools) != 1 || wire.Tools[0].Function.Name != "execute" || !reflect.DeepEqual(wire.Tools[0].Function.Parameters, executeTool().InputSchema) {
		t.Fatalf("tool declaration was not frozen: %+v", wire.Tools)
	}
	call, result := wire.Messages[len(wire.Messages)-2], wire.Messages[len(wire.Messages)-1]
	if call.Role != "assistant" || call.Content != nil || len(call.ToolCalls) != 1 || call.ToolCalls[0].Function.Arguments != `{"code":"print(1)"}` || result.Role != "tool" || result.ToolCallID != "prior_call" || result.Content != "1" {
		t.Fatalf("invalid tool transcript encoding: %+v %+v", call, result)
	}
}

func TestChatRejectsInvalidCallsWithoutRetryOrLosingAccounting(t *testing.T) {
	valid := `{"id":"call","type":"function","function":{"name":"execute","arguments":"{\"code\":\"print(1)\"}"}}`
	for _, tc := range []struct{ name, calls, finish string }{
		{"undeclared", strings.ReplaceAll(valid, "execute", "other"), "tool_calls"},
		{"invalid ID", strings.Replace(valid, `"id":"call"`, `"id":"../call"`, 1), "tool_calls"},
		{"nonobject arguments", strings.Replace(valid, `{\"code\":\"print(1)\"}`, `[]`, 1), "tool_calls"},
		{"malformed arguments", strings.Replace(valid, `{\"code\":\"print(1)\"}`, `{`, 1), "tool_calls"},
		{"wrong kind", strings.Replace(valid, `"type":"function"`, `"type":"custom"`, 1), "tool_calls"},
		{"duplicate ID", valid + "," + valid, "tool_calls"},
		{"too many calls", strings.TrimSuffix(strings.Repeat(valid+",", session.MaxToolCalls+1), ","), "tool_calls"},
		{"missing calls", "", "tool_calls"},
		{"wrong boundary", valid, "stop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[`+tc.calls+`]},"finish_reason":"`+tc.finish+`"}],"usage":{"cost":0.25,"prompt_tokens":7}}`)
			}))
			defer server.Close()
			request := chatRequest()
			request.Tools = []Tool{executeTool()}
			prepared, err := chatProvider(server.URL).Prepare(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			response, err := prepared.Execute(t.Context())
			failure, ok := errors.AsType[*CallError](err)
			if !ok || failure.Retryable || len(response.Parts) != 0 || calls.Load() != 1 {
				t.Fatalf("invalid call result=%+v err=%v calls=%d", response, err, calls.Load())
			}
			if response.Usage.Input == nil || *response.Usage.Input != 7 || response.ReportedCostNanoUSD == nil || *response.ReportedCostNanoUSD != 250000000 {
				t.Fatalf("invalid calls erased accounting: %+v", response)
			}
		})
	}
}

func TestChatRejectsInvalidToolDeclarationsBeforeDispatch(t *testing.T) {
	for _, tools := range [][]Tool{
		{executeTool(), executeTool()},
		{{Name: "invalid.name", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		{{Name: "execute", InputSchema: json.RawMessage(`null`)}},
		{{Name: "execute", InputSchema: json.RawMessage(`{"type":"array"}`)}},
		{{Name: "execute", InputSchema: json.RawMessage(`{`)}},
	} {
		request := chatRequest()
		request.Tools = tools
		if _, err := chatProvider("https://provider.example").Prepare(t.Context(), request); err == nil {
			t.Fatal("invalid tool declarations prepared successfully")
		}
	}
}
