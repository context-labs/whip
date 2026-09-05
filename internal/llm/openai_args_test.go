package llm

import (
	"context"
	"strings"
	"testing"
)

// Some providers stream a tool call whose accumulated function.arguments
// never closes into valid JSON — a provider emission bug, or a stream that
// ended mid-call. whip persists the assistant message with its tool_calls
// into history and replays them on the next turn, where strict providers
// validate incoming history and reject the whole request before the first
// token when an assistant tool_call carries malformed arguments.
//
// The malformed call must be dropped before it reaches history, the same way
// a max_tokens-truncated call is — see TestStreamLengthDiscardsToolCalls.
func TestStreamDiscardsToolCallWithInvalidJSONArgs(t *testing.T) {
	// arguments = `{"path":"/tmp/x"` — an object that never closed its brace
	srv := sseServer(t,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"w1","type":"function","function":{"name":"write","arguments":"{\"path\":\"/tmp/x\""}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	defer srv.Close()

	msg, _, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("tool call with invalid JSON arguments must be discarded before history, got %+v", msg.ToolCalls)
	}
	if !strings.Contains(msg.Content, "invalid JSON") {
		t.Fatalf("expected a discard note in content, got %q", msg.Content)
	}
}

// A stream that ends mid-tool-call with no finish_reason and no [DONE] (the
// server or a proxy closed the connection) leaves the accumulated arguments
// incomplete. Same root cause as invalid-args emission, same rejection on
// replay: the half-built call must not be persisted.
func TestStreamDiscardsIncompleteArgsOnDroppedStream(t *testing.T) {
	srv := sseServer(t,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"w1","type":"function","function":{"name":"write","arguments":"{\"path\":\"/tmp/x\""}}]}}]}`,
		// no finish_reason, no [DONE] — the server just stops sending
	)
	defer srv.Close()

	msg, _, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 0 {
		t.Fatalf("incomplete tool call from a dropped stream must be discarded, got %+v", msg.ToolCalls)
	}
}

// A tool call whose arguments stream into valid JSON must still be kept —
// the guard targets malformed arguments, not tool calls in general.
func TestStreamKeepsToolCallWithValidJSONArgs(t *testing.T) {
	srv := sseServer(t,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"w1","type":"function","function":{"name":"write","arguments":"{\"path\":\"/tmp/x\""}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":",\"content\":\"hi\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	defer srv.Close()

	msg, _, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("valid tool call must be kept, got %+v", msg.ToolCalls)
	}
	tc := msg.ToolCalls[0]
	if tc.Function.Arguments != `{"path":"/tmp/x","content":"hi"}` {
		t.Fatalf("arguments assembly changed: %q", tc.Function.Arguments)
	}
}

// A malformed call alongside a valid one: the valid sibling must still
// execute. Dropping only the bad call (not the whole turn) keeps the turn
// productive and leaves no orphan in history.
func TestStreamDropsOnlyMalformedKeepsValidSibling(t *testing.T) {
	srv := sseServer(t,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"good","type":"function","function":{"name":"read","arguments":"{\"path\":\"/a\"}"}}]}}]}`,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":1,"id":"bad","type":"function","function":{"name":"write","arguments":"{\"path\":\"/b\""}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	defer srv.Close()

	msg, _, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("only the malformed call should be dropped, got %+v", msg.ToolCalls)
	}
	tc := msg.ToolCalls[0]
	if tc.ID != "good" || tc.Function.Name != "read" || tc.Function.Arguments != `{"path":"/a"}` {
		t.Fatalf("valid sibling changed: %+v", tc)
	}
	if !strings.Contains(msg.Content, "write") || !strings.Contains(msg.Content, "invalid JSON") {
		t.Fatalf("discard note should name the dropped call, got %q", msg.Content)
	}
}

// A no-argument call (empty arguments) is a legitimate call shape, not a
// malformed one — it must be kept for the tool layer to handle.
func TestStreamKeepsToolCallWithEmptyArgs(t *testing.T) {
	srv := sseServer(t,
		`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"status"}}]}}]}`,
		`data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	)
	defer srv.Close()

	msg, _, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m"}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "c1" {
		t.Fatalf("no-arg call must be kept, got %+v", msg.ToolCalls)
	}
}
