package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func historyCall(t *testing.T, host *recursiveHost, operation string, args map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	result, err := host.Call(t.Context(), "context", operation, wire)
	if err != nil {
		t.Fatal(err)
	}
	return result.(map[string]any)
}

func TestHistorySearchUsesRawFieldsAndFrozenPages(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	host := runtime.rootNode.host
	empty := historyCall(t, host, "inspect", nil)
	if empty["through_seq"] != 0 {
		t.Fatal(empty)
	}
	call := llm.ToolCall{ID: "call", Type: "function"}
	call.Function.Name, call.Function.Arguments = "read", "{\"query\":\"needle\"}"
	raw := []llm.Message{{},
		{Role: "user", Content: strings.Repeat("x", 65534) + "needle\nΩ", Authored: true},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
		{Role: "tool", ToolCallID: "call", Content: "needle result"},
		{Role: "user", Content: "lead", Parts: []llm.ContentPart{{Type: "text", Text: "needle trailing"}, {Type: "image_url", ImageURL: &struct {
			URL string `json:"url"`
		}{URL: "needle image must not match"}}}},
	}
	if err := store.Save(root.ID(), 1, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordRawCompaction(t.Context(), root.ID(), root.ID(), 3, "summary omits original"); err != nil {
		t.Fatal(err)
	}
	runtime.rootNode.agent.ReplaceHistory(rlm.FocusedHistory([]llm.Message{{Role: "system", Content: "summary only"}}))
	if frozen := historyCall(t, host, "history", map[string]any{"through_seq": empty["through_seq"]}); len(frozen["messages"].([]map[string]any)) != 0 {
		t.Fatal(frozen)
	}
	page := historyCall(t, host, "history", map[string]any{"limit": 2})
	if !page["truncated"].(bool) || page["next_seq"] != 2 || page["through_seq"] != 4 {
		t.Fatal(page)
	}
	raw = append(raw, llm.Message{Role: "assistant", Content: "new needle"})
	if err := store.Save(root.ID(), 5, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	next := historyCall(t, host, "history", map[string]any{"after_seq": page["next_seq"], "through_seq": page["through_seq"]})
	if len(next["messages"].([]map[string]any)) != 2 || next["truncated"] != false {
		t.Fatal(next)
	}
	result := historyCall(t, host, "search", map[string]any{"query": "needle", "through_seq": 4})
	matches := result["matches"].([]map[string]any)
	if len(matches) != 4 || result["truncated"] != false {
		t.Fatal(result)
	}
	for i, want := range []string{"content", "tool_calls.0.arguments", "content", "parts.0.text"} {
		if matches[i]["field"] != want || matches[i]["seq"] != i+1 || matches[i]["provisional"] != false {
			t.Fatal(matches[i])
		}
	}
	span := matches[0]["span"].(map[string]any)
	if span["start"] != int64(65534) || span["end"] != int64(65540) {
		t.Fatal(span)
	}
	if live := historyCall(t, host, "search", map[string]any{"query": "new needle"}); len(live["matches"].([]map[string]any)) != 1 {
		t.Fatal(live)
	}
	for _, args := range []map[string]any{{"agent_id": "sibling"}, {"root_id": "other"}} {
		if _, err := host.Call(t.Context(), "context", "history", args); !errors.Is(err, sessionstore.ErrAgentAccess) {
			t.Fatalf("cross-agent access: %v", err)
		}
	}
}

func TestHistoryLargeMessageReadAndSearchContinuation(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	host := runtime.rootNode.host
	body := strings.Repeat("界", 4000) + strings.Repeat(" needle", 45)
	raw := []llm.Message{{}, {Role: "tool", Content: body}}
	if err := store.Save(root.ID(), 1, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"seq": 1, "field": "content", "length": 8192}
	var reconstructed strings.Builder
	for n := 0; n < 10; n++ {
		result := historyCall(t, host, "history", args)
		text := result["text"].(string)
		if !utf8.ValidString(text) || len(text) > 8192 {
			t.Fatal("invalid bounded read")
		}
		reconstructed.WriteString(text)
		if !result["truncated"].(bool) {
			break
		}
		args = result["next"].(map[string]any)
	}
	if reconstructed.String() != body {
		t.Fatal("message pagination lost content")
	}
	args = map[string]any{"query": "needle"}
	seen := map[int64]bool{}
	for n := 0; n < 10; n++ {
		result := historyCall(t, host, "search", args)
		for _, match := range result["matches"].([]map[string]any) {
			start := match["span"].(map[string]any)["start"].(int64)
			if seen[start] {
				t.Fatal("duplicate match", start)
			}
			seen[start] = true
		}
		if !result["truncated"].(bool) {
			break
		}
		args = result["next"].(map[string]any)
		args["query"] = "needle"
	}
	if len(seen) != 45 {
		t.Fatalf("matches=%d", len(seen))
	}
	// The same continuation must preserve a literal spanning the scan ceiling.
	raw[1].Content = strings.Repeat("x", maxSearchScan-2) + "needle beyond ceiling"
	if err := store.Save(root.ID(), 1, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	result := historyCall(t, host, "search", map[string]any{"query": "needle"})
	if result["stop_reason"] != "scan_limit" || result["truncated"] != true {
		t.Fatal(result)
	}
	args = result["next"].(map[string]any)
	args["query"] = "needle"
	result = historyCall(t, host, "search", args)
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["span"].(map[string]any)["start"] != int64(maxSearchScan-2) {
		t.Fatal(result)
	}
}

func TestHistoryErrorsAndMessageLimitAreExplicit(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://unused.invalid", ""), 1)
	raw := []llm.Message{{}}
	for n := 0; n < 130; n++ {
		raw = append(raw, llm.Message{Role: "user", Content: "no match"})
	}
	raw[130].Content = "find here"
	if err := store.Save(root.ID(), 1, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	result := historyCall(t, runtime.rootNode.host, "search", map[string]any{"query": "find"})
	if result["stop_reason"] != "message_limit" {
		t.Fatal(result)
	}
	args := result["next"].(map[string]any)
	args["query"] = "find"
	result = historyCall(t, runtime.rootNode.host, "search", args)
	if len(result["matches"].([]map[string]any)) != 1 || result["truncated"] != false {
		t.Fatal(result)
	}
	for _, invalid := range []any{1.5, "1", float64(1 << 54)} {
		if _, err := runtime.rootNode.host.Call(t.Context(), "context", "history", map[string]any{"seq": invalid}); err == nil {
			t.Fatalf("invalid history cursor accepted: %v", invalid)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := runtime.rootNode.host.Call(ctx, "context", "history", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := runtime.rootNode.host.Call(t.Context(), "context", "history", map[string]any{"through_seq": float64(131)}); err == nil {
		t.Fatal("unavailable upper sequence accepted")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.rootNode.host.Call(t.Context(), "context", "history", nil); err == nil {
		t.Fatal("closed storage looked empty")
	}
}

func TestCurrentTurnHistorySurvivesCompactionAndCommitsOnce(t *testing.T) {
	const needle = "unfocused original contract"
	original := strings.Repeat("prefix ", 2000) + needle + strings.Repeat(" suffix", 2000)
	var requests atomic.Int32
	var runtime *RecursiveRuntime
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream bool `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if !req.Stream {
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"summary without original"}}]}`)
			return
		}
		if requests.Add(1) == 1 {
			streamText(w, "round one")
			return
		}
		result, err := runtime.rootNode.host.Call(r.Context(), "context", "search", map[string]any{"query": needle})
		if err != nil {
			t.Error(err)
		} else {
			matches := result.(map[string]any)["matches"].([]map[string]any)
			if len(matches) != 1 || matches[0]["provisional"] != true || matches[0]["turn_id"] == "" {
				t.Errorf("provisional search=%+v", result)
			}
			// The source was folded into a summary, but the raw journal still has it.
			for _, m := range runtime.rootNode.agent.MessagesSnapshot() {
				if strings.Contains(m.Content, needle) {
					t.Error("test did not remove original from model view")
				}
			}
		}
		streamText(w, "finished")
	}))
	defer server.Close()
	store, root, value := openRecursiveRuntime(t, llm.New(server.URL, "key"), 1)
	runtime = value
	runtime.rootNode.agent.ContextLimit = 1000
	// Steer introduces a later whole-turn boundary that compaction can retain.
	runtime.rootNode.agent.SteerImages("continue after first response", nil)
	receipt, err := root.Submit(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	if done := waitReceipt(t, receipt); done.Err != nil {
		t.Fatal(done.Err)
	}
	if requests.Load() != 2 {
		t.Fatalf("model requests=%d", requests.Load())
	}
	result := historyCall(t, runtime.rootNode.host, "search", map[string]any{"query": needle})
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 || matches[0]["provisional"] != false {
		t.Fatal(result)
	}
	rows, err := store.ReadTranscript(t.Context(), root.ID(), root.ID(), 0, -1, 128)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows.Messages) != 4 || rows.Messages[0].Message.Content != original {
		t.Fatalf("journal rows=%d", len(rows.Messages))
	}
	// A fork reads the same original from its own durable source after reopen.
	fork, err := store.Fork(root.ID(), rows.ThroughSeq, "fork")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), fork); err != nil {
		t.Fatal(err)
	}
	forkNode := &AgentSession{id: fork, root: &Session{store: store, meta: sessionstore.Meta{ID: fork}}, agent: runtime.rootNode.agent}
	forkHost := &recursiveHost{session: forkNode}
	forkResult := historyCall(t, forkHost, "search", map[string]any{"query": needle})
	if len(forkResult["matches"].([]map[string]any)) != 1 {
		t.Fatal(forkResult)
	}
}
