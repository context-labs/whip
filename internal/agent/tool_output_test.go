package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

// TestOnToolOutputStreamsBash: a slow bash tool call fires OnToolOutput with
// the tool-call id and partial output while the command is still running
// (roadmap: "streamed partial tool output"). The event never fires for
// non-bash tools.
func TestOnToolOutputStreamsBash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		// Only tool call: bash with staged output so snapshots exist mid-run.
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "tool" {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		} else {
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"tc1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo early; sleep 0.3; echo late\"}"}}]}}]}`+"\n\n")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	bindTestAgent(t, ag, t.TempDir())

	var mu sync.Mutex
	var ids []string
	var snaps []string
	final, err := ag.Turn(context.Background(), "go", Events{
		OnToolOutput: func(id, soFar string) {
			mu.Lock()
			ids = append(ids, id)
			snaps = append(snaps, soFar)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if final != "done" {
		t.Fatalf("final: %q", final)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(snaps) == 0 {
		t.Fatal("OnToolOutput never fired for a slow bash call")
	}
	for _, id := range ids {
		if id != "tc1" {
			t.Fatalf("OnToolOutput carried the wrong tool-call id: %q", id)
		}
	}
	if !strings.Contains(strings.Join(snaps, ""), "early") {
		t.Fatalf("snapshots missed the in-flight output: %v", snaps)
	}
}
