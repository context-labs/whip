package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// The budget notice rides on every request, so under a finite cap its bytes
// must not change as spend accrues, and it must travel as the last message so
// the provider's cached prefix (system prompt and history) survives it.
func TestBudgetNoticeIsByteStableAndRidesLast(t *testing.T) {
	var mu sync.Mutex
	var last []llm.Message
	store, root, runtime := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		request, ok := modelAccountingRequest(t, w, r)
		if !ok {
			return
		}
		mu.Lock()
		last = request.Messages
		mu.Unlock()
		modelAccountingReply(w, request, "done", modelAccountingUsage())
	}, nil, nil)
	if err := store.SetBudgetLimit(t.Context(), root.ID(), "", session.BudgetCost, 5_000_000); err != nil {
		t.Fatal(err)
	}
	before, err := runtime.rootNode.modelBudgetNotice(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(before, "finite cost") || strings.Contains(before, "$") {
		t.Fatalf("notice must name the finite budget without an amount: %q", before)
	}
	for range 2 {
		receipt, err := root.Submit(t.Context(), "spend a little")
		if err != nil || waitReceipt(t, receipt).Err != nil {
			t.Fatalf("turn: %v", err)
		}
	}
	after, err := runtime.rootNode.modelBudgetNotice(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("notice changed with spend:\n%s\n%s", before, after)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(last) < 3 || last[0].Role != "system" || last[1].Role == "system" {
		t.Fatalf("request must open with the system prompt and history, got %d messages", len(last))
	}
	tail := last[len(last)-1]
	if tail.Role != "system" || !strings.Contains(tail.Content, "Model budgets") {
		t.Fatalf("notice must be the last message: %+v", tail)
	}
	// The call's span points at exactly the notice it sent.
	spans, err := store.SpansForTrace(t.Context(), root.ID(), "")
	if err != nil {
		t.Fatal(err)
	}
	var latest session.SpanRecord
	for _, span := range spans {
		if span.Kind == session.SpanKindLLM && span.StartNS > latest.StartNS {
			latest = span
		}
	}
	attrs := map[string]any{}
	if err := json.Unmarshal(latest.Attrs, &attrs); err != nil {
		t.Fatal(err)
	}
	ref, _ := attrs["ephemeral_ref"].(string)
	body, _, err := store.ReadContent(t.Context(), ref, root.ID(), root.AgentID(), 0, session.MaxContentRead)
	if err != nil || string(body) != tail.Content || attrs["ephemeral_bytes"] != float64(len(tail.Content)) {
		t.Fatalf("span ephemeral body=%q err=%v attrs=%v", body, err, attrs)
	}
}
