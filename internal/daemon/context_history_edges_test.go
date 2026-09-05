package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

func historyEdgeCall(t *testing.T, host *recursiveHost, operation string, args map[string]any) (map[string]any, error) {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	value, err := host.Call(t.Context(), "context", operation, wire)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func TestHistorySearchRejectsUnavailableExplicitSequence(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	if err := store.Save(root.ID(), 1, []llm.Message{{}, {Role: "user", Content: "source"}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name         string
		seq, through int
	}{
		{name: "after_source", seq: 2, through: 1},
		{name: "beyond_frozen_upper", seq: 1, through: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := historyEdgeCall(t, runtime.rootNode.host, "search", map[string]any{"query": "source", "seq": tc.seq, "through_seq": tc.through}); err == nil {
				t.Fatal("unavailable explicit message appeared to be a complete empty search")
			}
		})
	}
}

func TestHistorySearchResumesAcrossFieldsAndMessages(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	call := llm.ToolCall{ID: "field-call", Type: "function"}
	call.Function.Name, call.Function.Arguments = "read", `{"needle":"needle"}`
	raw := []llm.Message{{},
		{Role: "assistant", Content: strings.Repeat("needle;", 19), Parts: []llm.ContentPart{{Type: "text", Text: "needle"}, {Type: "text", Text: "needle needle"}}, ToolCalls: []llm.ToolCall{call}},
		{Role: "tool", Content: "needle", ToolCallID: call.ID, Name: "read"},
	}
	if err := store.Save(root.ID(), 1, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"query": "needle"}
	seen, perField := map[string]bool{}, map[string]int{}
	for page := 0; ; page++ {
		if page > 3 {
			t.Fatal("history search cursor did not make progress")
		}
		result := historyCall(t, runtime.rootNode.host, "search", args)
		for _, match := range result["matches"].([]map[string]any) {
			key := fmt.Sprintf("%v/%v/%v", match["seq"], match["field"], match["span"].(map[string]any)["start"])
			if seen[key] {
				t.Fatal("duplicate continuation match", key)
			}
			seen[key] = true
			perField[fmt.Sprintf("%v/%v", match["seq"], match["field"])]++
		}
		if !result["truncated"].(bool) {
			break
		}
		args = result["next"].(map[string]any)
		args["query"] = "needle"
	}
	if len(seen) != 25 || perField["1/content"] != 19 || perField["1/parts.0.text"] != 1 || perField["1/parts.1.text"] != 2 || perField["1/tool_calls.0.arguments"] != 2 || perField["2/content"] != 1 {
		t.Fatalf("field continuation lost matches: %+v", perField)
	}
}

func TestHistorySearchRenewsBudgetBeforeStartingAnotherField(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	raw := []llm.Message{{}, {Role: "user", Content: strings.Repeat("x", maxSearchScan-3), Parts: []llm.ContentPart{{Type: "text", Text: "no"}, {Type: "text", Text: "needle"}}}}
	if err := store.Save(root.ID(), 1, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	first := historyCall(t, runtime.rootNode.host, "search", map[string]any{"query": "needle"})
	if first["stop_reason"] != "scan_limit" || first["scanned"] != int64(maxSearchScan-3) {
		t.Fatal(first)
	}
	next := first["next"].(map[string]any)
	if next["seq"] != 1 || next["field"] != "parts.0.text" || next["offset"] != int64(0) {
		t.Fatalf("cursor skipped short field: %+v", next)
	}
	next["query"] = "needle"
	last := historyCall(t, runtime.rootNode.host, "search", next)
	matches := last["matches"].([]map[string]any)
	if last["truncated"] != false || len(matches) != 1 || matches[0]["field"] != "parts.1.text" {
		t.Fatalf("fresh budget continuation = %+v", last)
	}
}

func TestHistoryProvisionalCursorFreezesUpperAndSurvivesCommit(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	node := runtime.rootNode
	node.turn = turnJournal{TurnID: "same-turn"}
	node.recordTranscriptMessage(llm.Message{Role: "user", Content: "needle first"})
	node.recordTranscriptMessage(llm.Message{Role: "assistant", Content: "needle second"})
	first := historyCall(t, node.host, "history", map[string]any{"limit": 1})
	if first["turn_id"] != "same-turn" || first["through_seq"] != 2 || first["truncated"] != true {
		t.Fatal(first)
	}
	node.recordTranscriptMessage(llm.Message{Role: "user", Content: "needle later"})
	args := map[string]any{"after_seq": first["next_seq"], "through_seq": first["through_seq"], "turn_id": first["turn_id"]}
	second := historyCall(t, node.host, "history", args)
	rows := second["messages"].([]map[string]any)
	if len(rows) != 1 || rows[0]["seq"] != 2 || rows[0]["provisional"] != true || second["truncated"] != false {
		t.Fatal(second)
	}
	journal := node.turnJournal()
	raw := append([]llm.Message{{}}, journal.Messages...)
	if err := store.Save(root.ID(), 1, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	committed := historyCall(t, node.host, "history", args)
	rows = committed["messages"].([]map[string]any)
	if len(rows) != 1 || rows[0]["provisional"] != false || committed["turn_id"] != "same-turn" {
		t.Fatalf("same journal's commit invalidated/duplicated continuation: %+v", committed)
	}
	frozen := historyCall(t, node.host, "search", map[string]any{"query": "needle", "through_seq": first["through_seq"], "turn_id": first["turn_id"]})
	if len(frozen["matches"].([]map[string]any)) != 2 || frozen["truncated"] != false {
		t.Fatalf("committed rows and journal were counted twice: %+v", frozen)
	}
}

func TestHistoryProvisionalCursorsRejectReplacedTurn(t *testing.T) {
	_, _, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	node := runtime.rootNode
	node.turn = turnJournal{TurnID: "abandoned-turn"}
	node.recordTranscriptMessage(llm.Message{Role: "user", Content: strings.Repeat("needle;", 21)})
	inspection := historyCall(t, node.host, "inspect", nil)
	page := historyCall(t, node.host, "history", nil)
	read := historyCall(t, node.host, "history", map[string]any{"seq": 1, "field": "content", "length": 8})
	search := historyCall(t, node.host, "search", map[string]any{"query": "needle"})
	if inspection["turn_id"] != "abandoned-turn" || page["turn_id"] != "abandoned-turn" {
		t.Fatalf("provisional view did not expose its identity: %+v, %+v", inspection, page)
	}
	readCursor, searchCursor := read["next"].(map[string]any), search["next"].(map[string]any)
	searchCursor["query"] = "needle"
	if readCursor["turn_id"] != "abandoned-turn" || searchCursor["turn_id"] != "abandoned-turn" {
		t.Fatalf("continuations omitted provisional identity: %+v, %+v", readCursor, searchCursor)
	}
	node.turn = turnJournal{TurnID: "replacement-turn"}
	node.recordTranscriptMessage(llm.Message{Role: "user", Content: strings.Repeat("other;", 40)})
	for _, tc := range []struct {
		name, operation string
		cursor          map[string]any
	}{
		{name: "read", operation: "history", cursor: readCursor},
		{name: "search", operation: "search", cursor: searchCursor},
		{name: "page", operation: "history", cursor: map[string]any{"through_seq": page["through_seq"], "turn_id": page["turn_id"]}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := historyEdgeCall(t, node.host, tc.operation, tc.cursor); err == nil || !strings.Contains(err.Error(), "expired") {
				t.Fatalf("stale provisional cursor silently read another turn: %v", err)
			}
		})
	}
}

func TestHistorySerializedMessageCursorDetectsToolMetadataChange(t *testing.T) {
	_, _, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	node := runtime.rootNode
	node.turn = turnJournal{TurnID: "tool-turn"}
	call := llm.ToolCall{ID: "pending-tool", Type: "function"}
	call.Function.Name, call.Function.Arguments = "read", `{"path":"source.go"}`
	node.recordTranscriptMessage(llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}, Usage: &llm.Usage{PromptTokens: 100}, Model: "model"})
	first := historyCall(t, node.host, "history", map[string]any{"seq": 1, "field": "message", "length": 60})
	cursor := first["next"].(map[string]any)
	if cursor["message_revision"] == "" || cursor["message_revision"] == nil {
		t.Fatal("serialized message continuation has no revision")
	}
	call.DurationMs, call.ExitCode = 37, 1
	node.recordToolMetadata([]llm.ToolCall{call})
	if _, err := historyEdgeCall(t, node.host, "history", cursor); err == nil || !strings.Contains(err.Error(), "offset 0") {
		t.Fatalf("changing JSON metadata did not invalidate byte cursor: %v", err)
	}
	args := map[string]any{"seq": 1, "field": "message", "length": 60}
	var text strings.Builder
	for page := 0; ; page++ {
		if page > 10 {
			t.Fatal("stable JSON read did not complete")
		}
		result := historyCall(t, node.host, "history", args)
		text.WriteString(result["text"].(string))
		if result["truncated"] == false {
			break
		}
		args = result["next"].(map[string]any)
		args["length"] = 60
	}
	var restored llm.Message
	if err := json.Unmarshal([]byte(text.String()), &restored); err != nil || len(restored.ToolCalls) != 1 || restored.ToolCalls[0].ExitCode != 1 {
		t.Fatalf("reread failed to reconstruct settled JSON: %s, %v", text.String(), err)
	}
}

func TestHistoryFailedToolMetadataSurvivesRawCommitAndReload(t *testing.T) {
	store, root, node, start := mailboxDeliveryFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if request.Messages[len(request.Messages)-1].Role != "tool" {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"failed-call","type":"function","function":{"name":"fail_fixture","arguments":"{}"}}]}}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"reported tool failure"},"finish_reason":"stop"}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	node.agent = agent.NewRuntime(llm.New(server.URL, "key"), "model", 1024, "system", tools.NewServices())
	t.Cleanup(node.agent.Services.Close)
	node.agent.Tools = []tools.Tool{{
		Def: llm.NewTool("fail_fixture", "fail deterministically", `{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) {
			return "", errors.New("expected fixture failure")
		},
	}}
	node.agent.WorkingDir = root.WorkingDirectory()
	if _, err := node.RunTurn(t.Context(), "work", nil, false, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	journal := node.turnJournal()
	if len(journal.Messages) != 4 || len(journal.Messages[1].ToolCalls) != 1 || journal.Messages[1].ToolCalls[0].ExitCode != 1 {
		t.Fatalf("settled tool metadata missing from journal: %+v", journal.Messages)
	}
	acknowledged := slices.Clone(journal.DeliveredInbox)
	for _, item := range start.Items {
		acknowledged = append(acknowledged, item.Seq)
	}
	if err := root.FinishAgentTurn(t.Context(), node.id, sessionstore.AgentTurnCommit{
		TurnID: start.TurnID, Status: "succeeded", AcknowledgedInbox: acknowledged, DeliveredMessages: journal.DeliveredMessages, Messages: journal.Messages,
	}); err != nil {
		t.Fatal(err)
	}
	node.agent.ReplaceHistory(nil)
	reloaded, err := store.LoadAgentTranscript(t.Context(), root.ID(), node.id)
	if err != nil || len(reloaded) != 4 || len(reloaded[1].ToolCalls) != 1 || reloaded[1].ToolCalls[0].ExitCode != 1 {
		t.Fatalf("raw commit/reload lost tool failure metadata: %+v, %v", reloaded, err)
	}
	if reloaded[1].ToolCalls[0].DurationMs != journal.Messages[1].ToolCalls[0].DurationMs || reloaded[2].ToolCallID != "failed-call" || !strings.Contains(reloaded[2].Content, "expected fixture failure") {
		t.Fatalf("reload changed tool metadata or matching result: %+v", reloaded)
	}
}
