package agent

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

	"github.com/context-labs/whip/internal/llm"
)

// toolPairs appends n assistant(tool_call)+tool exchanges of roughly
// toolChars characters each, the shape of a long agentic turn.
func toolPairs(msgs []llm.Message, n, toolChars int, tag string) []llm.Message {
	for i := range n {
		id := fmt.Sprintf("%s%d", tag, i)
		var call llm.ToolCall
		call.ID, call.Type = id, "function"
		call.Function.Name = "rlm_exec"
		call.Function.Arguments = `{"code":"print(files.read(path='f` + id + `'))"}`
		msgs = append(msgs,
			llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
			llm.Message{Role: "tool", ToolCallID: id, Content: strings.Repeat("x", toolChars)},
		)
	}
	return msgs
}

func assertNoOrphans(t *testing.T, msgs []llm.Message) {
	t.Helper()
	owners := map[string]bool{}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			owners[tc.ID] = true
		}
	}
	for _, m := range msgs {
		if m.Role == "tool" && !owners[m.ToolCallID] {
			t.Fatalf("orphaned tool result %q", m.ToolCallID)
		}
	}
}

func TestCompactTailStartSplitsOversizedNewestTurn(t *testing.T) {
	msgs := []llm.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "do the audit"}}
	msgs = toolPairs(msgs, 10, 4000, "t") // ~1000 tokens per pair
	start, split := compactTailStart(msgs, 3500)
	if !split {
		t.Fatal("a single turn over the budget must be split")
	}
	// The kept tail begins at an assistant message and fits the budget.
	if msgs[start].Role != "assistant" {
		t.Fatalf("boundary should be an assistant message, got %s at %d", msgs[start].Role, start)
	}
	if got := EstimateTokens(msgs[start:]); got > 3500 {
		t.Fatalf("kept tail %d tokens exceeds budget", got)
	}
	if got := EstimateTokens(msgs[start-2:]); got <= 3500 {
		t.Fatalf("boundary should be the oldest pair that still fits; one more pair (%d tokens) also fits", got)
	}

	// A newest turn that fits is kept whole, exactly as before.
	small := toolPairs([]llm.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q"}}, 2, 40, "s")
	if start, split := compactTailStart(small, 15000); split || start != 1 {
		t.Fatalf("fitting turn: start=%d split=%v", start, split)
	}

	// Even when the newest pair alone busts the budget it is kept.
	huge := toolPairs([]llm.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "q"}}, 2, 40000, "h")
	start, split = compactTailStart(huge, 2000)
	if !split || start != len(huge)-2 {
		t.Fatalf("newest pair must always be kept: start=%d split=%v len=%d", start, split, len(huge))
	}
}

func TestCompactFoldsInsideOversizedTurnAndPinsOpeningMessage(t *testing.T) {
	var summaryPrompts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req llm.Request
		json.NewDecoder(r.Body).Decode(&req)
		summaryPrompts = append(summaryPrompts, req.Messages[len(req.Messages)-1].Content)
		w.Write([]byte(`{"choices":[{"message":{"content":"folded pairs"}}]}`))
	}))
	defer srv.Close()

	ag := newTestAgent(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.ContextLimit = 16000 // usable 8000 → tail budget 2000 (the floor)
	orders := "Audit the 58 recovered files. Read-only. Do not launch anything."
	ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: orders})
	ag.Messages = toolPairs(ag.Messages, 12, 2000, "t") // ~6000 tokens in one turn
	before := EstimateTokens(ag.Messages)

	_, _, info, err := ag.compact(context.Background())
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if !info.Pinned {
		t.Fatal("split fold must record the retained opening message")
	}
	after := EstimateTokens(ag.Messages)
	if after >= before/2 {
		t.Fatalf("fold inside the turn should shrink the view substantially: before %d after %d", before, after)
	}
	if ag.Messages[0].Content != "sys" || !strings.HasPrefix(ag.Messages[1].Content, summaryPrefix) {
		t.Fatalf("view must start with system prompt then summary: %+v", ag.Messages[:2])
	}
	if ag.Messages[2].Role != "user" || ag.Messages[2].Content != orders {
		t.Fatalf("the turn's opening user message must be pinned verbatim after the summary, got %+v", ag.Messages[2])
	}
	if ag.Messages[3].Role != "assistant" {
		t.Fatalf("kept tail must begin with an assistant message, got %s", ag.Messages[3].Role)
	}
	assertNoOrphans(t, ag.Messages)
	if len(summaryPrompts) != 1 || !strings.Contains(summaryPrompts[0], orders) {
		t.Fatalf("the summary should still see the opening message for context: %d prompts", len(summaryPrompts))
	}

	// The turn keeps going and grows; a second fold merges into the running
	// summary and re-pins the same opening message — no special cases.
	ag.Messages = toolPairs(ag.Messages, 12, 2000, "u")
	if _, _, _, err := ag.compact(context.Background()); err != nil {
		t.Fatalf("second compact: %v", err)
	}
	summaries := 0
	for _, m := range ag.Messages {
		if m.Role == "system" && strings.HasPrefix(m.Content, summaryPrefix) {
			summaries++
		}
	}
	if summaries != 1 {
		t.Fatalf("exactly one running summary expected, got %d", summaries)
	}
	if ag.Messages[2].Role != "user" || ag.Messages[2].Content != orders {
		t.Fatalf("opening message must stay pinned across folds, got %+v", ag.Messages[2])
	}
	if !strings.Contains(summaryPrompts[1], "running summary") {
		t.Fatal("second fold should merge into the prior summary (incremental prompt)")
	}
	assertNoOrphans(t, ag.Messages)
}

func TestMaybeCompactStallsInsteadOfRefolding(t *testing.T) {
	var summaryCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		summaryCalls++
		w.Write([]byte(`{"choices":[{"message":{"content":"folded"}}]}`))
	}))
	defer srv.Close()

	// A floor that cannot get under the threshold: big system prompt, tiny window.
	ag := newTestAgent(llm.New(srv.URL, "k"), "m", 100, strings.Repeat("s", 12000)) // ~3000 tokens
	ag.ContextLimit = 5000                                                          // threshold 2500 < floor
	ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: "go"})
	ag.Messages = toolPairs(ag.Messages, 6, 1200, "t")
	ag.lastPrompt = 4000 // provider says we are over the threshold

	if err := ag.maybeCompact(context.Background(), Events{}); err != nil {
		t.Fatalf("first fold: %v", err)
	}
	if summaryCalls != 1 {
		t.Fatalf("first fold should run one summary call, got %d", summaryCalls)
	}
	if !ag.compactStalled {
		t.Fatal("a fold that ends over the threshold must stall further proactive folds this turn")
	}
	// The turn grows and the provider keeps reporting over-threshold prompts.
	ag.Messages = toolPairs(ag.Messages, 3, 1200, "u")
	ag.lastPrompt = 4500
	for range 3 {
		if err := ag.maybeCompact(context.Background(), Events{}); err != nil {
			t.Fatalf("stalled fold: %v", err)
		}
	}
	if summaryCalls != 1 {
		t.Fatalf("no further summary calls while stalled, got %d", summaryCalls)
	}
}

func TestMaybeCompactNothingToFoldMakesNoModelCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("no summary call expected when there is nothing to fold")
	}))
	defer srv.Close()
	ag := newTestAgent(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.ContextLimit = 10000
	ag.Messages = append(ag.Messages,
		llm.Message{Role: "system", Content: summaryPrefix + "everything so far"},
		llm.Message{Role: "user", Content: "continue"},
		llm.Message{Role: "assistant", Content: "working"},
	)
	ag.lastPrompt = 9000
	for range 3 {
		if err := ag.maybeCompact(t.Context(), Events{}); err != nil {
			t.Fatalf("expected a quiet no-op, got %v", err)
		}
	}
	if ag.compactStalled {
		t.Fatal("nothing foldable yet must not stall later rounds")
	}
}

func TestMaybeCompactRechecksHistoryAfterTurnGrows(t *testing.T) {
	var summaryCalls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		summaryCalls.Add(1)
		w.Write([]byte(`{"choices":[{"message":{"content":"folded older pair"}}]}`))
	}))
	defer srv.Close()
	ag := newTestAgent(llm.New(srv.URL, "k"), "m", 100, strings.Repeat("s", 10000))
	ag.ContextLimit, ag.CompactThreshold = 10000, 0.5
	ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: "continue"})
	ag.Messages = toolPairs(ag.Messages, 1, 12000, "early")
	if EstimateTokens(ag.Messages) < int(ag.threshold()*float64(ag.ContextLimit)) {
		t.Fatal("fixture must exceed the proactive threshold before any history is foldable")
	}
	// The only pair exceeds the tail budget and must remain, so no model
	// call is useful yet, even though the context is over the threshold.
	for range 2 {
		if err := ag.maybeCompact(t.Context(), Events{}); err != nil {
			t.Fatal(err)
		}
	}
	if calls := summaryCalls.Load(); calls != 0 {
		t.Fatalf("no history to fold: summary calls=%d", calls)
	}
	ag.Messages = toolPairs(ag.Messages, 1, 12000, "later")
	before := EstimateTokens(ag.Messages)
	if err := ag.maybeCompact(t.Context(), Events{}); err != nil {
		t.Fatal(err)
	}
	if calls := summaryCalls.Load(); calls != 1 {
		t.Fatalf("older pair must become foldable in the same turn: summary calls=%d", calls)
	}
	if EstimateTokens(ag.Messages) >= before {
		t.Fatal("folding the older pair must shrink the context")
	}
	if !ag.compacted || !ag.compactStalled {
		t.Fatal("a real fold still above the threshold must retain the stall guard")
	}
	assertNoOrphans(t, ag.Messages)
}

func TestTurnFailsWithCompactionExhaustedWhenNothingFolds(t *testing.T) {
	// The provider rejects the request for size and there is nothing left to
	// fold: the turn must fail with the cause named, not with a bare
	// "not enough history" or an endless retry.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":"context_length_exceeded"}}`, http.StatusBadRequest)
	}))
	defer srv.Close()
	ag := newTestAgent(llm.New(srv.URL, "k"), "m", 100, "sys")
	for range 2 {
		_, err := ag.Turn(context.Background(), "a single enormous prompt", Events{})
		if !errors.Is(err, ErrCompactionExhausted) {
			t.Fatalf("expected ErrCompactionExhausted on each turn, got %v", err)
		}
	}
}

func TestStalledTurnStillGetsOneOverflowRetry(t *testing.T) {
	// A proactive fold earlier in the turn must not consume the reactive
	// retry: at the real edge the agent folds the pairs added since and
	// retries once.
	srv, pcall := compactionServer(t)
	defer srv.Close()
	ag := newTestAgent(llm.New(srv.URL, "k"), "m", 100, "sys")
	ag.compacted, ag.compactStalled = true, true // as left by an earlier proactive fold
	ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: "q"})
	ag.Messages = toolPairs(ag.Messages, 8, 400, "t")
	final, err := ag.Turn(context.Background(), "go", Events{})
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if final != "recovered" || *pcall != 3 {
		t.Fatalf("expected fail+summary+retry, got final=%q calls=%d", final, *pcall)
	}
}
