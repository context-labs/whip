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
	"github.com/context-labs/whip/internal/workflow"
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

	ag := New(llm.New(srv.URL, "k"), "m", 100000, "sys", WithExperimental([]string{FeatureWorkflows}))
	// The workflow tool must be registered by agent.New when opted in.
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

// schemaServer answers a workflow subagent turn by calling the
// structured_output tool once, so runWorkflowAgent's schema path returns the
// validated object (and its no-call repair branch gets coverage).
func schemaServer(t *testing.T, calls *atomic.Int64, callArgs string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		isSub := len(req.Messages) > 0 && strings.Contains(req.Messages[0].Content, "You are a subagent inside whip")
		if !isSub {
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		n := calls.Add(1)
		if n == 1 {
			// First turn: emit a structured_output tool call whose args ARE
			// the validated object.
			delta := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
				"tool_calls": []any{map[string]any{
					"index": 0, "id": "s1", "type": "function",
					"function": map[string]any{"name": "structured_output", "arguments": callArgs},
				}},
			}}}}
			line, _ := json.Marshal(delta)
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", line)
			return
		}
		// Any repair pass: plain text (no structured_output) so the repair
		// branch's "still didn't call it" path is covered.
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"no tool"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

// TestRunWorkflowAgentStructuredOutput drives the schema path of
// runWorkflowAgent directly: a workflow agent() call with a schema makes the
// subagent call structured_output, and runWorkflowAgent returns the parsed
// args as the result.
func TestRunWorkflowAgentStructuredOutput(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	var calls atomic.Int64
	wantArgs := `{"verdict":"pass","confidence":0.9}`
	srv := schemaServer(t, &calls, wantArgs)
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100000, "sys", WithExperimental([]string{FeatureWorkflows}))
	res, _, err := ag.runWorkflowAgent(context.Background(), workflow.AgentRequest{
		Prompt:  "produce a verdict",
		Options: workflow.AgentOptions{Schema: json.RawMessage(`{"type":"object","properties":{"verdict":{"type":"string"},"confidence":{"type":"number"}},"required":["verdict"]}`)},
	})
	if err != nil {
		t.Fatalf("runWorkflowAgent schema path errored: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["verdict"] != "pass" {
		t.Fatalf("schema result = %#v, want {verdict:pass}", res)
	}
	// 2 HTTP turns: the structured_output tool call, then the follow-up after
	// the agent sends the tool result back (the model then stops).
	if calls.Load() != 2 {
		t.Fatalf("expected 2 sub turns (tool call + follow-up), got %d", calls.Load())
	}
}

// TestRunWorkflowAgentStructuredOutputRepair drives the repair branch: the
// subagent doesn't call structured_output on the first turn, so runWorkflowAgent
// re-prompts; the repair also doesn't call it, so the function returns the
// "did not produce valid structured_output" error.
func TestRunWorkflowAgentStructuredOutputRepair(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	var calls atomic.Int64
	// A server that never calls structured_output (always plain text).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		calls.Add(1)
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"just text"},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	ag := New(llm.New(srv.URL, "k"), "m", 100000, "sys", WithExperimental([]string{FeatureWorkflows}))
	_, _, err := ag.runWorkflowAgent(context.Background(), workflow.AgentRequest{
		Prompt:  "produce a verdict",
		Options: workflow.AgentOptions{Schema: json.RawMessage(`{"type":"object"}`)},
	})
	if err == nil || !strings.Contains(err.Error(), "did not produce valid structured_output") {
		t.Fatalf("expected structured_output error, got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 sub turns (initial + repair), got %d", calls.Load())
	}
}

// TestRunWorkflowAgentModelOverrideError covers the model-override error
// branch: a request with a model the agent can't resolve returns an error
// before any subagent is spawned.
func TestRunWorkflowAgentModelOverrideError(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys", WithExperimental([]string{FeatureWorkflows}))
	// No ResolveModel set → a non-empty model override errors.
	_, _, err := ag.runWorkflowAgent(context.Background(), workflow.AgentRequest{
		Prompt: "x", Model: "unresolvable-model",
	})
	if err == nil || !strings.Contains(err.Error(), "model override") {
		t.Fatalf("expected model override error, got %v", err)
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

// hasTool reports whether the agent exposes a tool with the given name.
func hasTool(a *Agent, name string) bool {
	for i := range a.Tools {
		if a.Tools[i].Def.Function.Name == name {
			return true
		}
	}
	return false
}

// TestWorkflowToolGatedByExperimental pins the gating posture: the workflow
// tool is absent by default and present only when "workflows" is in the
// experimental set (a different name must NOT enable it).
func TestWorkflowToolGatedByExperimental(t *testing.T) {
	c := llm.New("http://unused", "k")

	// Default (no experimental set): workflow tool is not built.
	def := New(c, "m", 100, "sys")
	if hasTool(def, "workflow") {
		t.Fatal("workflow tool built without experimental opt-in")
	}
	if got := def.Experimental(); len(got) != 0 {
		t.Fatalf("default Experimental() = %v, want empty", got)
	}

	// Opted in: workflow tool is built.
	on := New(c, "m", 100, "sys", WithExperimental([]string{FeatureWorkflows}))
	if !hasTool(on, "workflow") {
		t.Fatal("workflow tool not built with experimental opt-in")
	}
	if got := on.Experimental(); len(got) != 1 || got[0] != FeatureWorkflows {
		t.Fatalf("Experimental() = %v, want [workflows]", got)
	}

	// A different name must NOT enable workflows (the check is name-specific).
	other := New(c, "m", 100, "sys", WithExperimental([]string{"future-thing"}))
	if hasTool(other, "workflow") {
		t.Fatal("workflow tool built by an unrelated experimental name")
	}
}
