package rlm_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type chatRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

type nativeEvaluation struct {
	ledger *store.Store
	runner *runner.Runner
	kernel *process.Kernel
	owner  session.Session
	turn   session.Turn
	cell   session.Cell
	host   *smokeHost
	prompt string
}

// The harness owns a restricted synthetic corpus host. All model dispatches,
// helper fan-out, budget admission and usage settlement use the shipping runner
// and SQLite ledger, not an evaluation-specific accounting implementation.
func newNativeEvaluation(t *testing.T, engineID string, provider runner.Provider, selected session.ModelSelection, spec comparisonSpec, host *smokeHost) *nativeEvaluation {
	t.Helper()
	ledger, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ledger.Close(); err != nil {
			t.Error(err)
		}
	})
	_, _, definition, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		t.Fatal(err)
	}
	_, owner, err := ledger.CreateTree(t.Context(), store.CreateTree{Definition: definition, Engine: session.Engine(engineID), WorkingDirectory: t.TempDir(), PermissionMode: new(session.PermissionAutomatic), Defaults: config.Default().Defaults, Overrides: session.ConfigPatch{Model: &selected, AutomaticTitle: new(false), GoalsEnabled: new(false), Modules: []string{"context", "models"}, Instructions: &session.Instructions{Text: "Answer the request exactly."}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.SetBudget(t.Context(), owner.ID, 0, session.BudgetLimit{Kind: session.BudgetModelCalls, Limit: new(int64(spec.MaxModelCalls))}); err != nil {
		t.Fatal(err)
	}
	value := &nativeEvaluation{ledger: ledger, owner: owner, host: host, prompt: "Answer the request exactly."}
	var executor runner.Executor
	if host != nil {
		value.prompt = codingPrompt(t, engineID, owner.WorkingDirectory, &corpusHandle{ID: host.contextHandle(), Size: len(host.corpus)})
		value.kernel = smokeKernel(t, engineID, host)
		host.evaluation = value
		executor = value
	}
	value.runner, err = runner.New(provider, ledger, ledger, nil, executor, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func (e *nativeEvaluation) Instructions(context.Context, session.Turn, session.Instructions) (string, error) {
	return e.prompt, nil
}

func (e *nativeEvaluation) Execute(ctx context.Context, turn session.Turn, message session.MessageID, call session.ToolCall) ([]session.Part, error) {
	var args struct {
		Code string `json:"code"`
	}
	if call.Name != "execute" {
		return nil, errors.New("unexpected fixture tool")
	}
	if err := json.Unmarshal(call.Arguments, &args); err != nil {
		return nil, err
	}
	cell, dispatch, err := e.ledger.BeginCell(ctx, session.CellSpec{ID: session.CellID("eval_" + string(message)), TurnID: turn.ID, CallMessageID: message, CallID: call.ID})
	if err != nil {
		return nil, err
	}
	if !dispatch {
		return nil, errors.New("fixture cell was already dispatched")
	}
	e.turn, e.cell = turn, cell
	result, runErr := e.kernel.Exec(ctx, process.Cell{Code: args.Code, CallID: string(cell.ID)})
	state := session.CellSucceeded
	if runErr != nil {
		state = session.CellFailed
	}
	// Keep the fixture's exact result envelope for evidence comparison. It is not
	// an alternative public protocol; canonical cell/output owns the transcript.
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	output := session.ToolResult{CallID: call.ID, Output: string(encoded), IsError: runErr != nil}
	if runErr != nil {
		output.Output = runErr.Error()
	}
	if _, err = e.ledger.SettleCell(ctx, cell.ID, state, output, nil); err != nil {
		return nil, err
	}
	return e.ledger.CellResultParts(ctx, turn.SessionID, cell.ID)
}

func (e *nativeEvaluation) batch(ctx context.Context, values []any) (any, error) {
	prompts := make([]string, len(values))
	for i, value := range values {
		var ok bool
		prompts[i], ok = value.(string)
		if !ok {
			return nil, errors.New("prompt is not a string")
		}
	}
	raw, err := json.Marshal(map[string]any{"prompts": prompts, "max_tokens": 256})
	if err != nil {
		return nil, err
	}
	operation, err := e.ledger.AdmitOperation(ctx, session.OperationSpec{ID: session.OperationID("batch_" + string(e.cell.ID)), CellID: e.cell.ID, RequestID: "batch", Capability: "models.batch", Resource: string(e.owner.TreeID), Arguments: raw})
	if err != nil {
		return nil, err
	}
	allowed, err := e.ledger.DispatchOperation(ctx, operation.ID)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, errors.New("fixture helper operation was already dispatched")
	}
	results, runErr := e.runner.CallModels(ctx, runner.ModelHelperRequest{Turn: e.turn, Model: e.owner.Config.Model, OperationID: operation.ID, Prompts: prompts, MaxTokens: new(int64(256))}, func(err error) bool { return errors.Is(err, store.ErrLimit) })
	encoded, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}
	settlement := session.OperationResult{State: session.OperationSucceeded, Value: encoded}
	if runErr != nil {
		settlement = session.OperationResult{State: session.OperationFailed, Failure: new("fixture model helpers failed")}
	}
	if _, err = e.ledger.SettleOperation(ctx, operation.ID, settlement); err != nil {
		return nil, err
	}
	if runErr != nil {
		return nil, runErr
	}
	for _, result := range results {
		if result.Failure != nil {
			return nil, errors.New(*result.Failure)
		}
	}
	return results, nil
}

func (e *nativeEvaluation) evaluate(ctx context.Context, spec comparisonSpec, task string) (evaluationMetrics, string, error) {
	started := time.Now()
	_, err := e.ledger.Admit(ctx, session.RequestIdentity{ClientID: "evaluation", RequestID: "input"}, store.Submission{SessionID: e.owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: task}}})
	if err != nil {
		return evaluationMetrics{}, "", err
	}
	claim, err := e.ledger.Claim(ctx, e.owner.ID)
	if err != nil {
		return evaluationMetrics{}, "", err
	}
	outcome, runErr := e.runner.Run(ctx, claim.Turn, claim.Configuration)
	if runErr != nil {
		outcome = runner.Failure(runErr)
	}
	if _, err = e.ledger.Finish(ctx, claim.Turn.ID, outcome.State, outcome.Failure, nil); err != nil {
		return evaluationMetrics{}, "", err
	}
	if outcome.Failure != nil {
		runErr = errors.New(*outcome.Failure)
	}
	attempts, err := e.ledger.ModelAttempts(ctx, claim.Turn.ID, "", 100)
	if err != nil {
		return evaluationMetrics{}, "", err
	}
	history, err := e.ledger.History(ctx, e.owner.ID, 0, 100)
	if err != nil {
		return evaluationMetrics{}, "", err
	}
	output := ""
	for _, message := range history {
		if message.Role == session.Assistant {
			for _, part := range message.Parts {
				if part.Type == "text" {
					output = part.Text
				}
			}
		}
	}
	expected := spec.ExpectedAnswer
	if expected == "" {
		expected = spec.Expected
	}
	metrics := evaluationMetrics{Correct: output == expected, Error: errorString(runErr), DurationMillis: time.Since(started).Milliseconds(), ModelCalls: len(attempts)}
	for _, attempt := range attempts {
		if bound := attempt.Request.InputTokenBound; bound != nil && (metrics.PeakDeclaredInputTokenBound == nil || *bound > *metrics.PeakDeclaredInputTokenBound) {
			metrics.PeakDeclaredInputTokenBound = new(*bound)
		}
		if attempt.DispatchedAt != nil && (attempt.Result == nil || attempt.Result.Usage.Input == nil || attempt.Result.Usage.Output == nil) {
			metrics.UnknownUsageCalls++
		}
		if attempt.Result != nil {
			usage := attempt.Result.Usage
			if usage.Input != nil {
				metrics.PromptTokens += int(*usage.Input)
				metrics.PeakCallPromptTokens = max(metrics.PeakCallPromptTokens, int(*usage.Input))
			}
			if usage.Output != nil {
				metrics.CompletionTokens += int(*usage.Output)
			}
		}
		if attempt.CostNanoUSD != nil {
			metrics.CostUSD += float64(*attempt.CostNanoUSD) / 1e9
		} else if attempt.DispatchedAt != nil {
			metrics.UnknownCostCalls++
		}
		if attempt.CostSource == "prices" {
			metrics.EstimatedCostCalls++
		}
	}
	metrics.CumulativeTokens = metrics.PromptTokens + metrics.CompletionTokens
	if e.host != nil {
		e.host.mu.Lock()
		metrics.HostCalls = len(e.host.calls)
		metrics.ModelFanout = e.host.modelFanout
		e.host.mu.Unlock()
	}
	return metrics, output, runErr
}

func fixturePrices(input, output float64) session.ModelPrices {
	in, out := int64(math.Round(input*1e15)), int64(math.Round(output*1e15))
	return session.ModelPrices{Input: &in, CachedInput: &in, Output: &out, Reasoning: &out, CachedOutput: &out}
}

func fixtureProvider(url string, spec comparisonSpec) model.OpenAI {
	return model.OpenAI{Resolve: func(context.Context, session.ModelSelection) (model.Route, error) {
		return model.Route{URL: url, Credential: "scripted", MaxOutputTokens: int64(spec.MaxOutputTokens), TimeoutMillis: 10000, MaxAttempts: 1, ContextWindowTokens: new(int64(spec.RootContextTokens)), Prices: fixturePrices(spec.InputPrice, spec.OutputPrice)}, nil
	}}
}

func probeToolResult(ctx context.Context, url, content string) (string, error) {
	provider := fixtureProvider(url, comparisonSpec{MaxOutputTokens: 256, RootContextTokens: 16384})
	prepared, err := provider.Prepare(ctx, model.Request{Selection: session.ModelSelection{Provider: "fixture", Name: "scripted"}, Messages: []model.Message{{Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute-1", Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}}}}, {Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: "execute-1", Output: content}}}}}})
	if err != nil {
		return "", err
	}
	result, err := prepared.Execute(ctx, func(model.Chunk) {})
	var output strings.Builder
	for _, part := range result.Parts {
		if part.Type == "text" {
			output.WriteString(part.Text)
		}
	}
	return output.String(), err
}

func liveProvider(t *testing.T) (model.OpenAI, session.ModelSelection) {
	t.Helper()
	directory := os.Getenv("WHIP_RLM_EVAL_DIRECTORY")
	if directory == "" {
		t.Fatal("live evaluation requires explicit WHIP_RLM_EVAL_DIRECTORY containing native host.json; no installed runtime is discovered")
	}
	host, err := config.Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	selected := host.Defaults.Model
	if name := os.Getenv("WHIP_RLM_EVAL_MODEL"); name != "" {
		selected.Name = name
	}
	if provider := os.Getenv("WHIP_RLM_EVAL_PROVIDER"); provider != "" {
		selected.Provider = provider
	}
	if err := selected.Validate(); err != nil {
		t.Fatal(err)
	}
	declaration, exists := host.Providers[selected.Provider]
	if !exists {
		t.Fatal("evaluation provider is not declared")
	}
	if declaration.Kind == "openai-codex" || declaration.CredentialSource == "inference-net" {
		t.Fatal("restricted live eval requires an explicit API route; account-managed product flows are covered by host acceptance")
	}
	provider := model.OpenAI{Resolve: func(ctx context.Context, selection session.ModelSelection) (model.Route, error) {
		if !selection.Equal(selected) {
			return model.Route{}, errors.New("evaluation selection changed")
		}
		settings, err := declaration.Models[selected.Name].Resolve()
		if err != nil {
			return model.Route{}, err
		}
		credential, err := declaration.Credential(ctx, os.LookupEnv)
		if err != nil {
			return model.Route{}, err
		}
		return model.Route{Kind: declaration.Kind, URL: declaration.BaseURL, Credential: credential, Prices: settings.Prices, MaxOutputTokens: settings.MaxOutputTokens, TimeoutMillis: settings.TimeoutMillis, MaxAttempts: settings.MaxAttempts, ContextWindowTokens: settings.ContextWindowTokens}, nil
	}}
	return provider, selected
}

func TestNativeEvaluationEnforcesSharedAttemptBudget(t *testing.T) {
	for _, engineID := range []string{process.EngineStarlark, process.EngineQuickJS} {
		t.Run(engineID, func(t *testing.T) {
			spec, corpus, task := loadComparison(t)
			spec.MaxModelCalls = 2 // One root call leaves room for only one of two reviewers.
			server := comparisonServer(t, engineID, spec, comparisonEvidence(spec, corpus))
			defer server.Close()
			host := &smokeHost{corpus: corpus, handle: "comparison-corpus"}
			value := newNativeEvaluation(t, engineID, fixtureProvider(server.URL, spec), session.ModelSelection{Provider: "fixture", Name: "scripted"}, spec, host)
			metrics, _, err := value.evaluate(t.Context(), spec, task)
			if err == nil || metrics.Correct || metrics.ModelCalls != 2 || metrics.ModelFanout != 2 {
				t.Fatalf("budget failure metrics=%+v error=%v", metrics, err)
			}
		})
	}
}
