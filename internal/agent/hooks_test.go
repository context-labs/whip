package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/hooks"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

func hookToolCall(id, name, args string) llm.ToolCall {
	call := llm.ToolCall{ID: id, Type: "function"}
	call.Function.Name = name
	call.Function.Arguments = args
	return call
}

type recordingHookRunner struct {
	mu       sync.Mutex
	requests []hooks.Request
	run      func(hooks.Request) hooks.Outcome
}

func (r *recordingHookRunner) Run(_ context.Context, req hooks.Request) hooks.Outcome {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	fn := r.run
	r.mu.Unlock()
	if fn != nil {
		return fn(req)
	}
	return hooks.Outcome{}
}

func (r *recordingHookRunner) snapshot() []hooks.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hooks.Request(nil), r.requests...)
}

func TestPromptHookAddsContextAndReceivesSession(t *testing.T) {
	var providerUser string
	srv := textServer(t, func(_ int, req llm.Request) string {
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				providerUser = msg.Content
			}
		}
		return "done"
	})
	defer srv.Close()

	runner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
		if req.Event == hooks.UserPromptSubmit {
			return hooks.Outcome{AdditionalContext: "follow the repository policy", Ran: 1}
		}
		return hooks.Outcome{}
	}}
	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.SetHooks(runner)
	ag.SetWorkingDir("/workspace/project")
	ag.SetSessionID("session-7")

	var events []hooks.Event
	final, err := ag.Turn(t.Context(), "ship it", Events{
		OnHook: func(event hooks.Event, _ hooks.Outcome) { events = append(events, event) },
	})
	if err != nil || final != "done" {
		t.Fatalf("Turn = %q, %v", final, err)
	}
	if !strings.Contains(providerUser, "ship it") || !strings.Contains(providerUser, "follow the repository policy") {
		t.Fatalf("provider user message did not include hook context: %q", providerUser)
	}
	requests := runner.snapshot()
	if len(requests) != 2 || requests[0].Event != hooks.UserPromptSubmit || requests[1].Event != hooks.Stop {
		t.Fatalf("hook sequence = %+v", requests)
	}
	if requests[0].SessionID != "session-7" || requests[0].WorkingDir != "/workspace/project" {
		t.Fatalf("hook scope = %+v", requests[0])
	}
	if len(events) != 1 || events[0] != hooks.UserPromptSubmit {
		t.Fatalf("event callback sequence = %v", events)
	}
}

func TestPromptHookBlocksBeforeConversationMutation(t *testing.T) {
	runner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
		return hooks.Outcome{Blocked: true, Reason: "prompt rejected", Ran: 1}
	}}
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	ag.SetHooks(runner)

	_, err := ag.Turn(t.Context(), "do not send", Events{})
	if err == nil || !strings.Contains(err.Error(), "prompt rejected") {
		t.Fatalf("blocked prompt error = %v", err)
	}
	if len(ag.Messages) != 1 {
		t.Fatalf("blocked prompt mutated conversation: %+v", ag.Messages)
	}
}

func TestToolHooksBlockAndAnnotateResults(t *testing.T) {
	var ran atomic.Int32
	testTool := tools.Tool{
		Def: llm.NewTool("probe", "probe", `{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) {
			ran.Add(1)
			return "tool output", nil
		},
	}

	t.Run("pre policy blocks execution", func(t *testing.T) {
		ran.Store(0)
		runner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
			if req.Event == hooks.PreToolUse {
				return hooks.Outcome{Blocked: true, Reason: "unsafe command", Ran: 1}
			}
			return hooks.Outcome{}
		}}
		ag := New(nil, "m", 100, "sys")
		ag.Tools = []tools.Tool{testTool}
		ag.SetHooks(runner)
		out := ag.runTools(t.Context(), []llm.ToolCall{hookToolCall("call-1", "probe", `{}`)}, Events{})
		if ran.Load() != 0 {
			t.Fatal("blocked tool executed")
		}
		if len(out) != 1 || !strings.Contains(out[0], "unsafe command") {
			t.Fatalf("blocked output = %v", out)
		}
		requests := runner.snapshot()
		if len(requests) != 1 || requests[0].Event != hooks.PreToolUse {
			t.Fatalf("blocked hook sequence = %+v", requests)
		}
	})

	t.Run("pre policy block stays bounded", func(t *testing.T) {
		t.Setenv("TMPDIR", t.TempDir())
		ran.Store(0)
		reason := strings.Repeat("x", 60_000)
		runner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
			if req.Event == hooks.PreToolUse {
				return hooks.Outcome{Blocked: true, Reason: reason, Ran: 1}
			}
			return hooks.Outcome{}
		}}
		ag := New(nil, "m", 100, "sys")
		ag.Tools = []tools.Tool{testTool}
		ag.SetHooks(runner)
		out := ag.runTools(t.Context(), []llm.ToolCall{hookToolCall("bounded-block", "probe", `{}`)}, Events{})
		if ran.Load() != 0 {
			t.Fatal("blocked tool executed")
		}
		if len(out) != 1 {
			t.Fatalf("tool results = %d, want 1", len(out))
		}
		if !strings.Contains(out[0], "bytes elided from the middle") || len(out[0]) >= len(reason) {
			t.Fatalf("blocked tool result was not bounded: lengths=%d", len(out[0]))
		}
	})

	t.Run("pre and post context reaches model", func(t *testing.T) {
		ran.Store(0)
		runner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
			switch req.Event {
			case hooks.PreToolUse:
				return hooks.Outcome{AdditionalContext: "pre context", Ran: 1}
			case hooks.PostToolUse:
				return hooks.Outcome{AdditionalContext: "post context", Ran: 1}
			default:
				return hooks.Outcome{}
			}
		}}
		ag := New(nil, "m", 100, "sys")
		ag.Tools = []tools.Tool{testTool}
		ag.SetHooks(runner)
		out := ag.runTools(t.Context(), []llm.ToolCall{hookToolCall("call-2", "probe", `{}`)}, Events{})
		if ran.Load() != 1 {
			t.Fatalf("tool runs = %d", ran.Load())
		}
		if !strings.Contains(out[0], "tool output") || !strings.Contains(out[0], "pre context") || !strings.Contains(out[0], "post context") {
			t.Fatalf("annotated output = %q", out[0])
		}
		requests := runner.snapshot()
		if len(requests) != 2 || requests[1].Event != hooks.PostToolUse || requests[1].ToolResponse != "tool output" {
			t.Fatalf("success hook sequence = %+v", requests)
		}
	})
}

func TestFailedToolFiresPostToolUseFailure(t *testing.T) {
	runner := &recordingHookRunner{}
	ag := New(nil, "m", 100, "sys")
	ag.Tools = []tools.Tool{{
		Def: llm.NewTool("fail", "fail", `{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) {
			return "", errors.New("boom")
		},
	}}
	ag.SetHooks(runner)
	out := ag.runTools(t.Context(), []llm.ToolCall{hookToolCall("call-fail", "fail", `{}`)}, Events{})
	if len(out) != 1 || out[0] != "Error: boom" {
		t.Fatalf("failure output = %v", out)
	}
	requests := runner.snapshot()
	if len(requests) != 2 || requests[1].Event != hooks.PostToolUseFailure || requests[1].ToolError != "Error: boom" {
		t.Fatalf("failure hook sequence = %+v", requests)
	}
}

func TestHookContextKeepsToolResultBounded(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	additional := strings.Repeat("x", 60_000)
	runner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
		if req.Event == hooks.PostToolUse {
			return hooks.Outcome{AdditionalContext: additional, Ran: 1}
		}
		return hooks.Outcome{}
	}}
	ag := New(nil, "m", 100, "sys")
	ag.Tools = []tools.Tool{{
		Def: llm.NewTool("probe", "probe", `{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) {
			return "tool output", nil
		},
	}}
	ag.SetHooks(runner)

	out := ag.runTools(t.Context(), []llm.ToolCall{hookToolCall("bounded", "probe", `{}`)}, Events{})
	if len(out) != 1 {
		t.Fatalf("tool results = %d, want 1", len(out))
	}
	if !strings.Contains(out[0], "bytes elided from the middle") {
		t.Fatalf("hook-annotated tool result was not bounded: lengths=%d", len(out[0]))
	}
	if len(out[0]) >= len(additional) {
		t.Fatalf("bounded result length = %d, want less than original context %d", len(out[0]), len(additional))
	}
}

func TestFailedToolStatusSurvivesLargeHookContext(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	runner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
		if req.Event == hooks.PostToolUseFailure {
			return hooks.Outcome{AdditionalContext: strings.Repeat("x", 60_000), Ran: 1}
		}
		return hooks.Outcome{}
	}}
	ag := New(nil, "m", 100, "sys")
	ag.Tools = []tools.Tool{{
		Def: llm.NewTool("failed-command", "failed command", `{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) {
			return "partial\n((exit: exit status 2))", nil
		},
	}}
	ag.SetHooks(runner)
	calls := []llm.ToolCall{hookToolCall("failed-with-context", "failed-command", `{}`)}
	out := ag.runTools(t.Context(), calls, Events{})

	if calls[0].ExitCode != 1 {
		t.Fatalf("tool exit code = %d, want failure after hook annotation", calls[0].ExitCode)
	}
	if len(out) != 1 {
		t.Fatalf("tool results = %d, want 1", len(out))
	}
	if !strings.Contains(out[0], "bytes elided from the middle") {
		t.Fatalf("failed hook-annotated result was not bounded: lengths=%d", len(out[0]))
	}
	requests := runner.snapshot()
	if len(requests) != 2 || requests[1].Event != hooks.PostToolUseFailure {
		t.Fatalf("failure hook sequence = %+v", requests)
	}
}

func TestToolExitCodeClassifiesBashFailures(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want int
	}{
		{name: "success", out: "ok", want: 0},
		{name: "tool error", out: "Error: boom", want: 1},
		{name: "non-interactive exit", out: "oops\n((exit: exit status 2))", want: 1},
		{name: "non-interactive timeout", out: "partial\n(command timed out)", want: 1},
		{name: "interactive timeout", out: "partial\n(timed out)", want: 1},
		{name: "interactive cancellation", out: "partial\n(cancelled)", want: 1},
		{name: "interactive inactivity timeout", out: "partial\n(timed out waiting for input)", want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toolExitCode(tt.out); got != tt.want {
				t.Fatalf("toolExitCode(%q) = %d, want %d", tt.out, got, tt.want)
			}
		})
	}
}

func TestStopHookRetriesAreBounded(t *testing.T) {
	srv := textServer(t, func(n int, _ llm.Request) string { return fmt.Sprintf("draft-%d", n) })
	defer srv.Close()

	var stops atomic.Int32
	runner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
		if req.Event == hooks.Stop {
			stops.Add(1)
			return hooks.Outcome{Blocked: true, Reason: "needs revision", Ran: 1}
		}
		return hooks.Outcome{}
	}}
	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.SetHooks(runner)
	final, err := ag.Turn(t.Context(), "answer", Events{})
	if err != nil {
		t.Fatal(err)
	}
	if final != "draft-4" || stops.Load() != maxStopHookRetries {
		t.Fatalf("bounded stop result = %q, calls = %d", final, stops.Load())
	}
	feedback := 0
	for _, msg := range ag.Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "Stop hook blocked completion") {
			feedback++
		}
	}
	if feedback != maxStopHookRetries {
		t.Fatalf("stop feedback messages = %d, want %d", feedback, maxStopHookRetries)
	}
}

func TestSubagentInheritsHookRunnerAndScope(t *testing.T) {
	runner := &recordingHookRunner{}
	ag := New(llm.New("http://unused", "k"), "m", 100, "sys")
	ag.SetHooks(runner)
	ag.SetWorkingDir("/workspace/project")
	ag.SetSessionID("session-parent")

	sub := ag.newSub(SubModel{})
	gotRunner, gotDir := sub.hookSnapshot()
	if gotRunner != runner || gotDir != "/workspace/project" {
		t.Fatalf("subagent hook scope = %T, %q", gotRunner, gotDir)
	}
	if sub.SessionIDValue() != "session-parent" {
		t.Fatalf("subagent session = %q", sub.SessionIDValue())
	}
}

func TestBackgroundSubagentHooksUseWorktreeDirectory(t *testing.T) {
	srv := textServer(t, func(_ int, _ llm.Request) string { return "done" })
	defer srv.Close()

	runner := &recordingHookRunner{}
	ag := New(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.SetHooks(runner)
	ag.SetWorkingDir("/workspace/project")
	worktree := t.TempDir()
	task := ag.RegisterBackground("worktree scope", "inspect", SubModel{})
	ag.LaunchBackground(task, worktree)

	select {
	case <-task.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("background subagent did not finish")
	}
	requests := runner.snapshot()
	if len(requests) != 2 || requests[0].Event != hooks.UserPromptSubmit || requests[1].Event != hooks.Stop {
		t.Fatalf("background hook sequence = %+v", requests)
	}
	for _, req := range requests {
		if req.WorkingDir != worktree {
			t.Fatalf("background hook working directory = %q, want %q", req.WorkingDir, worktree)
		}
	}
}

func TestHookManagerSwapDoesNotWaitForHookIO(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	oldRunner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
		if req.Event == hooks.PreToolUse {
			close(started)
			<-release
			return hooks.Outcome{AdditionalContext: "old manager", Ran: 1}
		}
		return hooks.Outcome{}
	}}
	newRunner := &recordingHookRunner{run: func(req hooks.Request) hooks.Outcome {
		if req.Event == hooks.PostToolUse {
			return hooks.Outcome{AdditionalContext: "new manager", Ran: 1}
		}
		return hooks.Outcome{}
	}}
	ag := New(nil, "m", 100, "sys")
	ag.Tools = []tools.Tool{{
		Def: llm.NewTool("probe", "probe", `{"type":"object"}`),
		Run: func(context.Context, json.RawMessage) (string, error) { return "done", nil },
	}}
	ag.SetHookScope(oldRunner, "/old")

	result := make(chan []string, 1)
	go func() {
		result <- ag.runTools(t.Context(), []llm.ToolCall{hookToolCall("swap", "probe", `{}`)}, Events{})
	}()
	select {
	case <-started:
	case <-t.Context().Done():
		t.Fatal("pre-hook did not start")
	}

	swapped := make(chan struct{})
	go func() {
		ag.SetHookScope(newRunner, "/new")
		close(swapped)
	}()
	select {
	case <-swapped:
	case <-time.After(time.Second):
		t.Fatal("manager swap waited for hook subprocess I/O")
	}
	close(release)

	select {
	case out := <-result:
		if len(out) != 1 || !strings.Contains(out[0], "old manager") || !strings.Contains(out[0], "new manager") {
			t.Fatalf("result across manager swap = %v", out)
		}
	case <-time.After(time.Second):
		t.Fatal("tool did not finish after releasing pre-hook")
	}
	oldRequests := oldRunner.snapshot()
	newRequests := newRunner.snapshot()
	if len(oldRequests) != 1 || oldRequests[0].WorkingDir != "/old" {
		t.Fatalf("old manager scope = %+v", oldRequests)
	}
	if len(newRequests) != 1 || newRequests[0].WorkingDir != "/new" {
		t.Fatalf("new manager scope = %+v", newRequests)
	}
}
