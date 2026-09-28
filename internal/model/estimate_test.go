package model

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func estimateRequest() Request {
	return Request{
		Instructions: "help",
		Messages: []Message{
			{Role: session.User, Parts: []session.Part{{Type: "text", Text: "question"}}},
			{Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{}`)}}}},
			{Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "call", Output: "result"}}}},
			{Role: session.User, Parts: []session.Part{{Type: "content", ReferenceID: "ref"}}},
		},
		Contents: map[string]Content{"ref": {MediaType: "text/plain", Data: []byte("body")}},
		Tools:    []Tool{{Name: "execute", Description: "code", InputSchema: json.RawMessage(`{}`)}},
	}
}

func TestEstimateInputTokensIncludesRequestComponents(t *testing.T) {
	more := strings.Repeat("x", 400)
	for _, tc := range []struct {
		name string
		grow func(*Request)
	}{
		{"instructions", func(r *Request) { r.Instructions += more }},
		{"role", func(r *Request) { r.Messages[0].Role += session.Role(more) }},
		{"text", func(r *Request) { r.Messages[0].Parts[0].Text += more }},
		{"call ID", func(r *Request) { r.Messages[1].Parts[0].Call.ID += more }},
		{"call name", func(r *Request) { r.Messages[1].Parts[0].Call.Name += more }},
		{"call arguments", func(r *Request) { r.Messages[1].Parts[0].Call.Arguments = json.RawMessage(`{"code":"` + more + `"}`) }},
		{"result call ID", func(r *Request) { r.Messages[2].Parts[0].Result.CallID += more }},
		{"result output", func(r *Request) { r.Messages[2].Parts[0].Result.Output += more }},
		{"content bytes", func(r *Request) { r.Contents["ref"] = Content{MediaType: "text/plain", Data: []byte("body" + more)} }},
		{"tool name", func(r *Request) { r.Tools[0].Name += more }},
		{"tool description", func(r *Request) { r.Tools[0].Description += more }},
		{"tool schema", func(r *Request) { r.Tools[0].InputSchema = json.RawMessage(`{"description":"` + more + `"}`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := estimateRequest()
			before := EstimateInputTokens(request)
			tc.grow(&request)
			if after := EstimateInputTokens(request); after < before+100 {
				t.Fatalf("component omitted: before=%d after=%d", before, after)
			}
		})
	}
}

func TestEstimateInputTokensIncludesContentMedia(t *testing.T) {
	request := Request{
		Messages: []Message{{Role: session.User, Parts: []session.Part{{Type: "content", ReferenceID: "ref"}}}},
		Contents: map[string]Content{"ref": {MediaType: "image/png", Data: []byte("body")}},
	}
	before := EstimateInputTokens(request)
	content := request.Contents["ref"]
	content.MediaType += strings.Repeat("x", 400)
	request.Contents["ref"] = content
	if after := EstimateInputTokens(request); after != before+100 {
		t.Fatalf("media omitted: before=%d after=%d", before, after)
	}
}

func TestEstimateInputTokensCountsContentOccurrencesWithoutMutating(t *testing.T) {
	for _, media := range []string{"text/plain", "image/png", "application/octet-stream"} {
		t.Run(media, func(t *testing.T) {
			request := Request{Messages: []Message{{Role: session.User}}, Contents: map[string]Content{
				"ref":    {MediaType: media, Data: bytes.Repeat([]byte("x"), 6000)},
				"unused": {MediaType: "image/jpeg", Data: bytes.Repeat([]byte("x"), 12000)},
			}}
			empty := EstimateInputTokens(request)
			request.Messages[0].Parts = []session.Part{{Type: "content", ReferenceID: "ref"}}
			once := EstimateInputTokens(request)
			request.Messages[0].Parts = append(request.Messages[0].Parts, request.Messages[0].Parts[0])
			twice := EstimateInputTokens(request)
			if once <= empty || twice-once != once-empty {
				t.Fatalf("repeated content was deduplicated: empty=%d once=%d twice=%d", empty, once, twice)
			}
			delete(request.Contents, "unused")
			if EstimateInputTokens(request) != twice {
				t.Fatal("unreferenced cache entry counted as input")
			}
			before, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			EstimateInputTokens(request)
			after, err := json.Marshal(request)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("estimation mutated the request")
			}
		})
	}
}

func TestEstimateInputTokensDoesNotValidateOrAuthorize(t *testing.T) {
	if EstimateInputTokens(Request{}) != 0 {
		t.Fatal("empty request has estimated input")
	}
	request := chatRequest()
	request.Messages[0].Parts = []session.Part{{Type: "content", ReferenceID: "missing"}}
	if EstimateInputTokens(request) < 1200 {
		t.Fatal("missing content silently estimated as empty")
	}
	if _, err := chatProvider("https://example.test").Prepare(t.Context(), request); err == nil {
		t.Fatal("estimation authorized missing content")
	}
	request = estimateRequest()
	before := EstimateInputTokens(request)
	request.Purpose, request.SessionID, request.TurnID = "compaction", "another", "another-turn"
	request.Selection = session.ModelSelection{Provider: "provider", Name: "other-model"}
	if EstimateInputTokens(request) != before {
		t.Fatal("non-content identity or route changed estimated input")
	}
}

func TestTokenEstimateArithmeticSaturates(t *testing.T) {
	for _, tc := range []struct {
		name         string
		total, value int64
		want         int64
	}{
		{"zero", 0, 0, 0},
		{"below", math.MaxInt64 - 2, 1, math.MaxInt64 - 1},
		{"exact", math.MaxInt64 - 2, 2, math.MaxInt64},
		{"overflow", math.MaxInt64 - 2, 3, math.MaxInt64},
		{"saturated", math.MaxInt64, 1, math.MaxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := addTokenEstimate(tc.total, tc.value); got != tc.want {
				t.Fatalf("sum=%d want=%d", got, tc.want)
			}
		})
	}
	for _, divisor := range []int{3, 4} {
		want := int64(math.MaxInt/divisor + 1)
		if got := estimateByteTokens(math.MaxInt, divisor); got != want {
			t.Fatalf("rounded maximum byte count overflowed: %d want=%d", got, want)
		}
	}
}
