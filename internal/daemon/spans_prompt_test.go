package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// A mid-turn fold leaves a trace a reader can follow: every model call span
// of the turn points at the system prompt it sent, and the summary call is
// named compaction and carries the summary it produced with the raw cutoff.
func TestTraceSpansPointAtThePromptAndTheFoldSummary(t *testing.T) {
	history := []llm.Message{{Role: "system", Content: "system"}}
	for turn := range 8 {
		history = append(history,
			llm.Message{Role: "user", Authored: true, Content: fmt.Sprintf("turn %d %s", turn, strings.Repeat("history ", 1000))},
			llm.Message{Role: "assistant", Content: strings.Repeat("response ", 1000)},
		)
	}
	const summary = "Retain the outstanding task and its decisions."
	store, root, runtime := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		request, ok := modelAccountingRequest(t, w, r)
		if !ok {
			return
		}
		output := "continued task"
		if request.Model == "compact-model" {
			output = summary
		}
		modelAccountingReply(w, request, output, modelAccountingUsage())
	}, history, func(value *agent.Agent) {
		value.ContextLimit = 4000
		value.CompactClient, value.CompactModel, value.CompactProvider = value.Client, "compact-model", "compact-provider"
		value.CompactPricing = llm.Pricing{Prompt: "0.000007", Completion: "0.000011"}
	})
	receipt, err := root.Submit(t.Context(), "continue the task")
	if err != nil || waitReceipt(t, receipt).Err != nil {
		t.Fatalf("turn with a fold: %v", err)
	}
	spans, err := store.SpansForTrace(t.Context(), root.ID(), "")
	if err != nil {
		t.Fatal(err)
	}
	var calls, folds int
	var promptRef string
	for _, span := range spans {
		if span.Kind != session.SpanKindLLM {
			continue
		}
		attrs := map[string]any{}
		if err := json.Unmarshal(span.Attrs, &attrs); err != nil {
			t.Fatal(err)
		}
		calls++
		ref, _ := attrs["system_prompt_ref"].(string)
		if ref == "" || promptRef != "" && ref != promptRef {
			t.Fatalf("every call of the turn must point at the one system prompt: %v", attrs)
		}
		promptRef = ref
		_, hasNotice := attrs["ephemeral_ref"]
		if attrs["purpose"] != "compaction" {
			if !hasNotice {
				t.Fatalf("a turn call must point at the notice it sent: %v", attrs)
			}
			continue
		}
		if hasNotice {
			t.Fatalf("the compaction call sends no notice and must not claim one: %v", attrs)
		}
		folds++
		if span.Name != "compaction" {
			t.Fatalf("compaction span named %q", span.Name)
		}
		outputRef, _ := attrs["output_ref"].(string)
		body, _, err := store.ReadContent(t.Context(), outputRef, root.ID(), root.AgentID(), 0, session.MaxContentRead)
		if err != nil || string(body) != summary {
			t.Fatalf("compaction output=%q err=%v attrs=%v", body, err, attrs)
		}
		if cutoff, _ := attrs["raw_cutoff"].(float64); cutoff < 1 {
			t.Fatalf("compaction span lacks a raw cutoff: %v", attrs)
		}
	}
	if calls != 2 || folds != 1 {
		t.Fatalf("calls=%d folds=%d", calls, folds)
	}
	body, _, err := store.ReadContent(t.Context(), promptRef, root.ID(), root.AgentID(), 0, session.MaxContentRead)
	if err != nil || len(body) == 0 || string(body) != runtime.rootNode.prompt.Prompt {
		t.Fatalf("system prompt body=%d bytes err=%v", len(body), err)
	}
	// WHIP_OTLP_DUMP=<path> keeps this session's export for a manual oracle
	// run against HALO or inference.net: a real fold with no provider spend.
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
