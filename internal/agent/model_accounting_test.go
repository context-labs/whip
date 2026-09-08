package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/tools"
)

type attemptBudgetFunc func(context.Context, llm.ModelAttempt) (llm.ModelPermit, error)

func (f attemptBudgetFunc) BeginModelAttempt(ctx context.Context, attempt llm.ModelAttempt) (llm.ModelPermit, error) {
	return f(ctx, attempt)
}

func TestModelAccountingPreservesAnswerAndUnexecutedTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: "+`{"choices":[{"delta":{"content":"Completed analysis","tool_calls":[{"index":0,"id":"pending","function":{"name":"echo","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`+"\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	ag := NewRuntime(llm.New(srv.URL, "key"), "m", 128, "system", tools.NewServices())
	defer ag.Services.Close()
	ran := false
	ag.Tools = []tools.Tool{{Def: llm.NewTool("echo", "echo", `{"type":"object"}`), Run: func(context.Context, json.RawMessage) (string, error) { ran = true; return "side effect", nil }}}
	ag.SetModelCallBudget(attemptBudgetFunc(func(_ context.Context, a llm.ModelAttempt) (llm.ModelPermit, error) {
		return llm.ModelPermit{MaxTokens: a.MaxTokens, Timeout: a.Timeout, Settle: func(llm.ModelAttemptResult) error { return errors.New("injected accounting failure") }}, nil
	}))
	var journal []llm.Message
	answer, err := ag.Turn(t.Context(), "work", Events{OnMessage: func(m llm.Message) int { journal = append(journal, m); return len(journal) }})
	if !llm.IsCompletedAccountingError(err) || answer != "Completed analysis" || ran {
		t.Fatalf("answer=%q err=%v ran=%v", answer, err, ran)
	}
	if len(journal) != 3 || journal[1].Content != answer || len(journal[1].ToolCalls) != 1 || journal[2].ToolCallID != "pending" || !strings.Contains(journal[2].Content, "Not executed") {
		t.Fatalf("journal=%+v", journal)
	}
}

func TestModelAccountingFinalAdmissionCannotBypassBudget(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, "data: "+`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"echo","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	ag := NewRuntime(llm.New(srv.URL, "key"), "m", 128, "system", tools.NewServices())
	defer ag.Services.Close()
	ag.MaxTurns = 1
	ag.Tools = []tools.Tool{echoTool()}
	var purposes []string
	ag.SetModelCallBudget(attemptBudgetFunc(func(_ context.Context, a llm.ModelAttempt) (llm.ModelPermit, error) {
		purposes = append(purposes, a.Purpose)
		if a.Purpose == "final" {
			return llm.ModelPermit{}, errors.New("no budget")
		}
		return llm.ModelPermit{MaxTokens: 32, Timeout: a.Timeout, Settle: func(llm.ModelAttemptResult) error { return nil }}, nil
	}))
	_, err := ag.Turn(t.Context(), "work", Events{})
	if !llm.IsAccountingError(err) || requests != 1 || strings.Join(purposes, ",") != "turn,final" {
		t.Fatalf("requests=%d purposes=%v err=%v", requests, purposes, err)
	}
}

func TestModelAccountingCompactionJournalsSummaryBeforeStopping(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"Retained summary"}}],"usage":{"prompt_tokens":10,"completion_tokens":2}}`)
	}))
	defer srv.Close()
	ag := NewRuntime(llm.New(srv.URL, "key"), "m", 128, "system", tools.NewServices())
	defer ag.Services.Close()
	ag.ContextLimit = 8192
	for range 6 {
		ag.Messages = append(ag.Messages, llm.Message{Role: "user", Content: "work"}, llm.Message{Role: "assistant", Content: strings.Repeat("old completed work ", 600)})
	}
	ag.SetModelCallBudget(attemptBudgetFunc(func(_ context.Context, a llm.ModelAttempt) (llm.ModelPermit, error) {
		if a.Purpose != "compaction" {
			t.Fatalf("purpose=%s", a.Purpose)
		}
		return llm.ModelPermit{MaxTokens: a.MaxTokens, Timeout: a.Timeout, Settle: func(llm.ModelAttemptResult) error { return errors.New("accounting stopped") }}, nil
	}))
	journaled := ""
	err := ag.ManualCompact(t.Context(), Events{OnCompaction: func(summary string, _ int, _ []llm.Message) { journaled = summary }})
	if !llm.IsCompletedAccountingError(err) || journaled != "Retained summary" || !strings.Contains(ag.Messages[1].Content, journaled) {
		t.Fatalf("summary=%q err=%v messages=%+v", journaled, err, ag.Messages)
	}
}
