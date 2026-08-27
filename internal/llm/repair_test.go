package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRepairToolHistoryWellFormedUnchanged(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "c1"}}},
		{Role: "tool", Content: "r1", ToolCallID: "c1"},
		{Role: "assistant", Content: "done"},
	}
	got := repairToolHistory(msgs)
	if len(got) != len(msgs) {
		t.Fatalf("well-formed history changed length: %d -> %d", len(msgs), len(got))
	}
	for i := range msgs {
		if got[i].Role != msgs[i].Role || got[i].Content != msgs[i].Content {
			t.Fatalf("message %d changed: %+v -> %+v", i, msgs[i], got[i])
		}
	}
}

func TestRepairToolHistoryUnansweredCalls(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1"}, {ID: "c2"}}},
	}
	got := repairToolHistory(msgs)
	if len(got) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(got), got)
	}
	for i, id := range []string{"c1", "c2"} {
		m := got[2+i]
		if m.Role != "tool" || m.ToolCallID != id || m.Content == "" {
			t.Fatalf("missing synthetic result for %s: %+v", id, m)
		}
	}
}

func TestRepairToolHistoryPartiallyAnswered(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1"}, {ID: "c2"}}},
		{Role: "tool", Content: "r1", ToolCallID: "c1"}, // c2 never answered
	}
	got := repairToolHistory(msgs)
	if len(got) != 4 {
		t.Fatalf("want 4 messages, got %d: %+v", len(got), got)
	}
	last := got[3]
	if last.Role != "tool" || last.ToolCallID != "c2" {
		t.Fatalf("synthetic result for c2 missing: %+v", last)
	}
}

func TestRepairToolHistoryOrphanedToolResult(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "tool", Content: "big file contents", ToolCallID: "c-gone"},
		{Role: "user", Content: "q"},
	}
	got := repairToolHistory(msgs)
	if len(got) != 3 {
		t.Fatalf("want 3 messages, got %d: %+v", len(got), got)
	}
	m := got[1]
	if m.Role != "user" || m.ToolCallID != "" || !strings.Contains(m.Content, "big file contents") {
		t.Fatalf("orphan not flattened to user context: %+v", m)
	}
}

func TestRepairToolHistoryIdempotent(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1"}}},
		{Role: "tool", Content: "orphan", ToolCallID: "c-gone"},
	}
	once := repairToolHistory(msgs)
	twice := repairToolHistory(once)
	if len(once) != len(twice) {
		t.Fatalf("not idempotent: %d -> %d", len(once), len(twice))
	}
	for i := range once {
		if once[i].Role != twice[i].Role || once[i].Content != twice[i].Content || once[i].ToolCallID != twice[i].ToolCallID {
			t.Fatalf("not idempotent at %d: %+v -> %+v", i, once[i], twice[i])
		}
	}
}

// Kimi K3 rejects a request body whose assistant tool_calls have no following
// tool result with: 400 "tool messages need a resolvable tool name". The
// request Stream sends must always carry the synthetic results.
func TestStreamSendsSyntheticResultsForUnansweredCalls(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	msgs := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_1"}}},
	}
	if _, _, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m", Messages: msgs}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, `"tool_call_id":"call_1"`) || !strings.Contains(s, `"role":"tool"`) {
		t.Fatalf("no tool result paired with unanswered call: %s", s)
	}
}

// Moonshot/Kimi rejects tool messages without a "name" field (400 "tool
// messages need a resolvable tool name"). Every tool result on the wire —
// real, synthetic, or resumed from an old session — must carry the owning
// function's name.
func TestStreamToolMessagesCarryName(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n"))
	}))
	defer srv.Close()

	calls := []ToolCall{{ID: "call_1"}}
	calls[0].Function.Name = "bash"
	msgs := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: calls},
		// a real result from a pre-Name session: ToolCallID set, Name empty
		{Role: "tool", Content: "ok", ToolCallID: "call_1"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "call_2"}}}, // unanswered
	}
	if _, _, err := New(srv.URL, "test-key").Stream(context.Background(), Request{Model: "m", Messages: msgs}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, `"tool_call_id":"call_1","name":"bash"`) {
		t.Fatalf("real result missing backfilled name: %s", s)
	}
	if !strings.Contains(s, `"role":"tool","content":"(interrupted before execution)","tool_call_id":"call_2"`) {
		t.Fatalf("synthetic result missing: %s", s)
	}
}
