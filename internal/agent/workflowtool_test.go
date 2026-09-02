package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

// workflowServer answers: the parent's first request with a workflow tool
// call; every workflow subagent request with plain text (no tool calls, so
// the subagent turn ends immediately); the parent's follow-up (after the
// steered workflow completion) with a final answer.
func workflowServer(t *testing.T, subCalls *atomic.Int64) *httptest.Server {
	t.Helper()
	script := `export const meta = { name: 'demo', description: 'd' }
const r = await parallel([0, 1].map(i => () => agent('task ' + i, { label: 'w' + i })))
return 'workflow done: ' + r.filter(Boolean).length + ' agents'`
	// Build the tool-call SSE payload with real JSON marshaling — hand-escaping
	// corrupts the arguments string (the inner \n escapes need re-escaping).
	callArgs, _ := json.Marshal(map[string]any{"script": script})
	delta := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
		"tool_calls": []any{map[string]any{
			"index": 0, "id": "w1", "type": "function",
			"function": map[string]any{"name": "workflow", "arguments": string(callArgs)},
		}},
	}}}}
	callLine, _ := json.Marshal(delta)
	var parentCalls atomic.Int64
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")

		// Workflow subagent turn: its messages carry the workflow subagent
		// system prompt. Answer with bare text so the subagent turn ends.
		isSub := len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "You are a subagent inside whip")
		if isSub {
			subCalls.Add(1)
			fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"sub report"},"finish_reason":"stop"}]}`+"\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}

		// Parent turn.
		n := parentCalls.Add(1)
		if n == 1 {
			fmt.Fprintf(w, "data: %s\n\n", callLine)
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		// Follow-up after the steered workflow completion.
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"parent final"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func TestWorkflowToolEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHIP_HOME", home)

	var subCalls atomic.Int64
	srv := workflowServer(t, &subCalls)
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100000, "sys")
	// The workflow tool must be registered by agent.New.
	found := false
	for i := range ag.Tools {
		if ag.Tools[i].Def.Function.Name == "workflow" {
			found = true
		}
	}
	if !found {
		t.Fatal("workflow tool not registered in agent.New")
	}

	// Run the parent's turn; the model calls the workflow tool, which starts a
	// background run. Completion steers back and the parent answers.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var steered []string
	final, err := ag.Turn(ctx, "run a workflow", Events{
		OnSteer: func(s string) { steered = append(steered, s) },
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the background run's completion to steer back (the turn may
	// end before the workflow settles).
	deadline := time.Now().Add(10 * time.Second)
	for subCalls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if subCalls.Load() != 2 {
		t.Fatalf("expected 2 workflow subagent calls, got %d", subCalls.Load())
	}
	_ = final

	// The workflow result must have steered back into the parent.
	mgr := ag.Workflows()
	deadline = time.Now().Add(5 * time.Second)
	for {
		runs := mgr.List()
		if len(runs) == 1 && runs[0].Status == "complete" {
			snap, _ := mgr.Snapshot(runs[0].ID)
			if snap.Result == nil || !strings.Contains(fmt.Sprint(snap.Result), "workflow done: 2 agents") {
				t.Fatalf("workflow result: %v", snap.Result)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("workflow did not complete: runs=%+v", runs)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestResolveWorkflowScript(t *testing.T) {
	// Inline script passes through.
	s, err := resolveWorkflowScript("export const meta = {}", "")
	if err != nil || s != "export const meta = {}" {
		t.Fatalf("inline: %q %v", s, err)
	}
	// Fences are stripped.
	s, err = resolveWorkflowScript("```js\nexport const meta = {}\n```", "")
	if err != nil || s != "export const meta = {}" {
		t.Fatalf("fenced: %q %v", s, err)
	}
	// scriptPath reads the file.
	dir := t.TempDir()
	p := dir + "/wf.js"
	if err := writeFile(p, "export const meta = {name:'x'}"); err != nil {
		t.Fatal(err)
	}
	s, err = resolveWorkflowScript("", p)
	if err != nil || !strings.Contains(s, "name:'x'") {
		t.Fatalf("path: %q %v", s, err)
	}
	// Neither is an error.
	if _, err = resolveWorkflowScript("", ""); err == nil {
		t.Fatal("expected error with neither script nor scriptPath")
	}
}

func writeFile(p, content string) error {
	return os.WriteFile(p, []byte(content), 0o600)
}
