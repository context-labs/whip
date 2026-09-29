package model

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func imageToolRequest() Request {
	request := chatRequest()
	request.Tools = []Tool{executeTool()}
	request.Messages = append(request.Messages, Message{Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "first", Name: "execute", Arguments: json.RawMessage(`{}`)}}, {Type: "tool_call", Call: &session.ToolCall{ID: "second", Name: "execute", Arguments: json.RawMessage(`{}`)}}}},
		Message{Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "first", Output: "canonical first"}}, {Type: "content", ReferenceID: "image"}}},
		Message{Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "second", Output: "canonical second"}}}})
	request.Contents = map[string]Content{"image": {MediaType: "image/jpeg", Data: []byte("trusted screenshot")}}
	return request
}

func TestTypedToolImagesPreserveHistoryAndTranslateAfterAllToolResults(t *testing.T) {
	request := imageToolRequest()
	before, _ := json.Marshal(request.Messages)
	for _, kind := range []string{"chat", "responses", "subscription"} {
		t.Run(kind, func(t *testing.T) {
			var raw []byte
			var err error
			switch kind {
			case "chat":
				raw, err = encodeChat(request, "https://example.test/v1", 100)
			case "responses":
				raw, err = encodeResponses(request, "scope", 100)
			case "subscription":
				provider, _, configured := subscriptionFixture()
				configured.Messages = request.Messages
				configured.Contents = request.Contents
				configured.Tools = request.Tools
				provider.Client = &http.Client{Transport: contextLimitTransport(func(r *http.Request) (*http.Response, error) {
					raw, _ = io.ReadAll(r.Body)
					return subscriptionResponse(200, responsesTerminal(responsesOutput, responsesUsage)), nil
				})}
				var prepared Prepared
				prepared, err = provider.Prepare(t.Context(), configured)
				if err == nil {
					_, err = prepared.Execute(t.Context(), nil)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			body := string(raw)
			encoded := "dHJ1c3RlZCBzY3JlZW5zaG90"
			if !strings.Contains(body, encoded) || strings.Index(body, "canonical second") > strings.Index(body, encoded) {
				t.Fatal("image missing or interrupted outstanding tool results", body)
			}
			if !strings.Contains(body, `"role":"user"`) {
				t.Fatal("image role incompatible", body)
			}
			after, _ := json.Marshal(request.Messages)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("wire projection mutated canonical history")
			}
		})
	}
}

func TestToolImagesRetainExistingProviderBoundsAndHydrationRequirement(t *testing.T) {
	for _, kind := range []string{"chat", "responses"} {
		t.Run(kind, func(t *testing.T) {
			encode := func(request Request) error {
				if kind == "chat" {
					_, err := encodeChat(request, "https://example.test/v1", 100)
					return err
				}
				_, err := encodeResponses(request, "scope", 100)
				return err
			}
			request := imageToolRequest()
			delete(request.Contents, "image")
			if err := encode(request); err == nil {
				t.Fatal("unhydrated image accepted")
			}
			request = imageToolRequest()
			request.Contents["image"] = Content{MediaType: "image/jpeg", Data: make([]byte, session.MaxContentBytes)}
			if err := encode(request); err == nil {
				t.Fatal("image silently raised aggregate context budget")
			}
			request = imageToolRequest()
			request.Messages[2].Parts[1] = session.Part{Type: "text", Text: "not an attachment"}
			if err := encode(request); err == nil {
				t.Fatal("tool text accepted as image part")
			}
		})
	}
}
