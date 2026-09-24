package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// A user command that calls the model outside a turn (/compact, goal from
// context) is its own trace: a root named for the command, the model call
// under it with its bodies, and nothing left on the node afterwards.
func TestUserCommandsOutsideATurnGetTheirOwnTrace(t *testing.T) {
	history := []llm.Message{{Role: "system", Content: "system"}}
	for turn := range 8 {
		history = append(history,
			llm.Message{Role: "user", Authored: true, Content: fmt.Sprintf("turn %d %s", turn, strings.Repeat("history ", 1000))},
			llm.Message{Role: "assistant", Content: strings.Repeat("response ", 1000)},
		)
	}
	const summary = "Retain the outstanding task and its decisions."
	const systemPrompt = "You are the test agent."
	var failCompaction atomic.Bool
	store, root, runtime := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		request, ok := modelAccountingRequest(t, w, r)
		if !ok {
			return
		}
		output := "continued task"
		if request.Model == "compact-model" {
			if failCompaction.Load() {
				http.Error(w, "summarizer down", http.StatusInternalServerError)
				return
			}
			output = summary
		}
		modelAccountingReply(w, request, output, modelAccountingUsage())
	}, history, func(value *agent.Agent) {
		// The fixture saves history from index 1, so give the agent the
		// system message its compaction request will send.
		value.SetSystemPrompt(systemPrompt)
		value.ContextLimit = 4000
		value.CompactClient, value.CompactModel, value.CompactProvider = value.Client, "compact-model", "compact-provider"
		value.CompactPricing = llm.Pricing{Prompt: "0.000007", Completion: "0.000011"}
	})
	spansByKind := func() (roots, calls []session.SpanRecord) {
		t.Helper()
		spans, err := store.SpansForTrace(t.Context(), root.ID(), "")
		if err != nil {
			t.Fatal(err)
		}
		for _, span := range spans {
			switch {
			case span.Kind == session.SpanKindAgent && span.ParentID == "":
				roots = append(roots, span)
			case span.Kind == session.SpanKindLLM:
				calls = append(calls, span)
			}
		}
		return roots, calls
	}
	attrsOf := func(span session.SpanRecord) map[string]any {
		t.Helper()
		attrs := map[string]any{}
		if err := json.Unmarshal(span.Attrs, &attrs); err != nil {
			t.Fatal(err)
		}
		return attrs
	}
	body := func(ref any) string {
		t.Helper()
		id, _ := ref.(string)
		data, _, err := store.ReadContent(t.Context(), id, root.ID(), root.AgentID(), 0, session.MaxContentRead)
		if err != nil {
			t.Fatalf("content %q: %v", id, err)
		}
		return string(data)
	}

	// /compact with no turn before it: a root named compact and the fold under it.
	if result := clientCommand(t, root, "tui", "compact-1", "history.compact", protocol.EmptyParams{}); result.Status != "succeeded" {
		t.Fatalf("manual compaction: %+v", result)
	}
	roots, calls := spansByKind()
	if len(roots) != 1 || roots[0].Name != "compact" || roots[0].Status != session.SpanStatusOK {
		t.Fatalf("roots after /compact = %+v", roots)
	}
	compactRoot := roots[0]
	if attrs := attrsOf(compactRoot); attrs["trigger"] != "command" || attrs["command"] != "history.compact" || attrs["input"] != "/compact" || attrs["output"] != summary {
		t.Fatalf("compact root attrs = %v", attrs)
	}
	if len(calls) != 1 || calls[0].ParentID != compactRoot.ID || calls[0].TraceID != compactRoot.TraceID || calls[0].Name != "compaction" {
		t.Fatalf("fold call must sit under the compact root: %+v", calls)
	}
	fold := attrsOf(calls[0])
	if body(fold["output_ref"]) != summary || fold["raw_cutoff"] == nil || body(fold["system_prompt_ref"]) != systemPrompt {
		t.Fatalf("fold call attrs = %v", fold)
	}
	if _, _, ok := runtime.rootNode.traceContext(); ok {
		t.Fatal("the command's identity must not outlive it")
	}

	// A turn, then a failing /compact: the failure closes its own root as an
	// error, as a second compact trace beside the turn's.
	receipt, err := root.Submit(t.Context(), "continue the task")
	if err != nil || waitReceipt(t, receipt).Err != nil {
		t.Fatalf("turn: %v", err)
	}
	failCompaction.Store(true)
	if result := clientCommand(t, root, "tui", "compact-2", "history.compact", protocol.EmptyParams{}); result.Status == "succeeded" {
		t.Fatalf("compaction must fail while the summarizer is down: %+v", result)
	}
	roots, _ = spansByKind()
	var compacts []session.SpanRecord
	var turnRoot session.SpanRecord
	for _, span := range roots {
		if span.Name == "compact" {
			compacts = append(compacts, span)
		} else {
			turnRoot = span
		}
	}
	if len(compacts) != 2 || compacts[0].TraceID == compacts[1].TraceID || turnRoot.ID == "" {
		t.Fatalf("each /compact is its own trace beside the turn: %+v", roots)
	}
	var failed session.SpanRecord
	for _, span := range compacts {
		if span.ID != compactRoot.ID {
			failed = span
		}
	}
	if failed.Status != session.SpanStatusError || attrsOf(failed)["error"] == nil || failed.TraceID == turnRoot.TraceID {
		t.Fatalf("failed compact root = %+v %v", failed, attrsOf(failed))
	}
	if _, _, ok := runtime.rootNode.traceContext(); ok {
		t.Fatal("a failed command must also clear its identity")
	}

	// A goal from context is its own trace too; its success enqueues a goal
	// turn, which is why it comes last here.
	if result := clientCommand(t, root, "tui", "goal-1", "goal.from-context", map[string]any{"window": 2}); result.Status != "succeeded" {
		t.Fatalf("goal from context: %+v", result)
	}
	roots, calls = spansByKind()
	var goalRoot session.SpanRecord
	for _, span := range roots {
		if span.Name == "goal" {
			goalRoot = span
		}
	}
	if goalRoot.ID == "" || goalRoot.TraceID == turnRoot.TraceID {
		t.Fatalf("goal must be its own trace beside the turn: %+v", roots)
	}
	if attrs := attrsOf(goalRoot); attrs["command"] != "goal.from-context" || attrs["output"] != "continued task" || attrs["input"] != "/goal-from-context 2" {
		t.Fatalf("goal root attrs = %v", attrs)
	}
	var goalCalls int
	for _, call := range calls {
		if call.ParentID == goalRoot.ID {
			goalCalls++
			if call.TraceID != goalRoot.TraceID || attrsOf(call)["purpose"] != "helper" {
				t.Fatalf("goal call = %+v", call)
			}
		}
	}
	if goalCalls != 1 {
		t.Fatalf("goal calls under the goal root = %d", goalCalls)
	}
	// WHIP_OTLP_DUMP=<path> keeps this session's export for a manual oracle run.
	if path := os.Getenv("WHIP_OTLP_DUMP"); path != "" {
		data, _, err := store.ExportOTLP(t.Context(), root.ID(), session.ExportOptions{ServiceVersion: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
