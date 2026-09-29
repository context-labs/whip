package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type runControlExecutor struct{ calls int }

func (*runControlExecutor) Instructions(context.Context, session.Turn, session.Instructions) (string, error) {
	return "system", nil
}

func (e *runControlExecutor) Execute(_ context.Context, _ session.Turn, _ session.MessageID, call session.ToolCall) (session.ToolResult, error) {
	e.calls++
	return session.ToolResult{CallID: call.ID, Output: "observed"}, nil
}

func TestRunControlUncappedAndFinalWithoutTools(t *testing.T) {
	for _, maximum := range []int{0, 2} {
		t.Run(strconv.Itoa(maximum), func(t *testing.T) {
			ledger := newOutputLedger()
			executor := &runControlExecutor{}
			calls := 0
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				calls++
				if request.SessionID != "owner" || request.CacheKey != "explicit cache" {
					t.Fatal("cache replaced provenance", request)
				}
				if maximum > 0 && calls > maximum {
					if len(request.Tools) != 0 || request.Purpose != "final" || !strings.Contains(request.Instructions, "tool-call limit") {
						t.Fatal("final did not remove tools", request)
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "final answer"}}}, nil
				}
				if maximum == 0 && calls == 35 {
					return model.Response{Parts: []session.Part{{Type: "text", Text: "uncapped answer"}}}, nil
				}
				if len(request.Tools) != 1 {
					t.Fatal("ordinary round lost tools")
				}
				return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: fmt.Sprintf("call_%d", calls), Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}}}}, nil
			})
			runner, err := New(provider, ledger, ledger, nil, executor, nil, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := runner.Run(t.Context(), session.Turn{ID: "turn", SessionID: "owner"}, session.Configuration{Run: &session.RunConfiguration{MaxTurns: maximum, CacheKey: "explicit cache"}})
			want := 3
			if maximum == 0 {
				want = 35
			}
			if err != nil || outcome.State != session.Succeeded || calls != want || executor.calls != want-1 || len(ledger.specs) != want {
				t.Fatal(outcome, err, calls, executor.calls, len(ledger.specs))
			}
		})
	}
}

func TestRunControlFinalRejectsUnsolicitedToolAndCancellation(t *testing.T) {
	ledger := newOutputLedger()
	executor := &runControlExecutor{}
	calls := 0
	runner, err := New(providerFunc(func(_ context.Context, _ model.Request) (model.Response, error) {
		calls++
		return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: strconv.Itoa(calls), Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}}}}, nil
	}), ledger, ledger, nil, executor, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	result, err := runner.Run(t.Context(), session.Turn{ID: "turn", SessionID: "owner"}, session.Configuration{Run: &session.RunConfiguration{MaxTurns: 1}})
	if err != nil || result.State != session.Failed || executor.calls != 1 || calls != 2 {
		t.Fatal(result, err, executor.calls, calls)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := runner.Run(ctx, session.Turn{ID: "cancel", SessionID: "owner"}, session.Configuration{Run: &session.RunConfiguration{}}); err == nil {
		t.Fatal("uncapped cancelled run continued")
	}
}
