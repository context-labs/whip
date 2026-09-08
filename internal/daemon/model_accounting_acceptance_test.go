package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

// This fixture uses the production recursive runtime, SQLite accounting, and
// HTTP transport. Its provider is local and never sends a billable request.
func modelAccountingRuntime(t *testing.T, handler http.HandlerFunc, history []llm.Message, configure func(*agent.Agent)) (*session.Store, *Session, *RecursiveRuntime) {
	t.Helper()
	return modelAccountingRuntimeAt(t, filepath.Join(t.TempDir(), "sessions.db"), handler, history, configure)
}

func modelAccountingRuntimeAt(t *testing.T, path string, handler http.HandlerFunc, history []llm.Message, configure func(*agent.Agent)) (*session.Store, *Session, *RecursiveRuntime) {
	t.Helper()
	provider := httptest.NewServer(handler)
	t.Cleanup(provider.Close)
	store := openStore(t, path)
	t.Cleanup(func() { _ = store.Close() })
	rootID := createRoot(t, store)
	if len(history) > 0 {
		if err := store.Save(rootID, 1, history, "model", "provider"); err != nil {
			t.Fatal(err)
		}
	}
	var runtime *RecursiveRuntime
	owner, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
		client := llm.New(provider.URL, "local-test-key")
		client.MaxRetries = 1
		value := agent.NewRuntime(client, "root-model", 128, rlm.BuildPrompt(meta.CWD, nil), tools.NewServices())
		value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
		value.Pricing = llm.Pricing{Prompt: "0.000002", Completion: "0.000005", InputCacheRead: "0"}
		if configure != nil {
			configure(value)
		}
		limits := rlm.DefaultLimits()
		limits.MaxWorkers = 4
		var err error
		runtime, err = NewRecursiveRuntime(RecursiveRuntimeOptions{
			Agent: value, History: history, Limits: limits, Kernels: rlm.NewManager(4), KernelCommand: recursiveKernelCommand,
		})
		if err != nil {
			return Components{}, err
		}
		return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	return store, root, runtime
}

func modelAccountingReply(w http.ResponseWriter, request llm.Request, output string, usage map[string]any) {
	if !request.Stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": output}}}, "usage": usage,
		})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	chunk, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": map[string]any{"content": output}, "finish_reason": "stop"}}, "usage": usage,
	})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
}

func modelAccountingToolReply(w http.ResponseWriter, id, code, text string, usage map[string]any) {
	arguments, _ := json.Marshal(map[string]string{"code": code})
	call := map[string]any{
		"index": 0, "id": id, "type": "function",
		"function": map[string]string{"name": "rlm_exec", "arguments": string(arguments)},
	}
	chunk, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"delta": map[string]any{"content": text, "tool_calls": []any{call}}, "finish_reason": "tool_calls",
		}}, "usage": usage,
	})
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
}

func modelAccountingRequest(t *testing.T, w http.ResponseWriter, r *http.Request) (llm.Request, bool) {
	t.Helper()
	var request llm.Request
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Error(err)
		http.Error(w, "invalid fixture request", http.StatusBadRequest)
		return request, false
	}
	return request, true
}

func modelAccountingUsage() map[string]any {
	return map[string]any{"prompt_tokens": 10, "completion_tokens": 5}
}

func modelAccountingSummary(t *testing.T, store *session.Store, root *Session, agentID string, subtree bool) session.ModelAccounting {
	t.Helper()
	summary, err := store.ModelAccounting(t.Context(), root.ID(), agentID, subtree)
	if err != nil {
		t.Fatal(err)
	}
	if summary.PendingCalls != 0 {
		t.Fatalf("completed calls retained reservations: %+v", summary)
	}
	return summary
}

func assertModelAccountingUnchanged(t *testing.T, before, after session.ModelAccounting) {
	t.Helper()
	if after.Revision < before.Revision {
		t.Fatalf("accounting revision moved backward: before=%+v after=%+v", before, after)
	}
	before.Revision = after.Revision
	if before != after {
		t.Fatalf("accounting totals changed: before=%+v after=%+v", before, after)
	}
}

func TestModelAccountingAcceptanceTurnsAndHelpers(t *testing.T) {
	store, root, runtime := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		request, ok := modelAccountingRequest(t, w, r)
		if !ok {
			return
		}
		usage := modelAccountingUsage()
		switch request.Messages[len(request.Messages)-1].Content {
		case "free helper":
			usage["cost"] = 0
		case "batch reported":
			usage["cost"] = 0.0001
		case "batch estimated":
			usage["prompt_tokens_details"] = map[string]int{"cached_tokens": 8}
		default:
			usage["cost"] = 0.00031
		}
		modelAccountingReply(w, request, "accounted result", usage)
	}, nil, nil)
	receipt, err := root.Submit(t.Context(), "normal root turn")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	result, err := runtime.rootNode.host.Call(t.Context(), "models", "call", map[string]any{"prompt": "free helper"})
	if err != nil || result.(map[string]any)["error"] != nil {
		t.Fatalf("models.call = %+v, %v", result, err)
	}
	batch, err := runtime.rootNode.host.Call(t.Context(), "models", "batch", map[string]any{
		"prompts": []any{"batch reported", "batch estimated"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range batch.([]map[string]any) {
		if result["error"] != nil {
			t.Fatalf("models.batch item = %+v", result)
		}
	}
	summary := modelAccountingSummary(t, store, root, "", true)
	if summary.ReportedCalls != 4 || summary.EstimatedCalls != 0 || summary.ReportedCostMicros != 410 || summary.EstimatedCostMicros != 29 || summary.UnknownCostCalls != 0 {
		t.Fatalf("root + helper + batch accounting = %+v", summary)
	}
	snapshot, err := root.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	assertModelAccountingUnchanged(t, summary, snapshot.Accounting)
}

func TestModelAccountingAcceptanceUnknownUsageAndCost(t *testing.T) {
	for _, test := range []struct {
		name                         string
		usage                        map[string]any
		reported, estimated, unknown int64
		cost                         int64
	}{
		{name: "missing usage and price", estimated: 1, unknown: 1},
		{name: "reported usage without price", usage: modelAccountingUsage(), reported: 1, unknown: 1},
		{name: "provider charge without price", usage: map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "cost": 0.00019}, reported: 1, cost: 190},
		{name: "provider charge without usage", usage: map[string]any{"cost": 0.00019}, estimated: 1, cost: 190},
		{name: "explicit zero usage and charge", usage: map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "cost": 0}, reported: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, root, runtime := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
				request, ok := modelAccountingRequest(t, w, r)
				if !ok {
					return
				}
				modelAccountingReply(w, request, "unpriced result", test.usage)
			}, nil, func(value *agent.Agent) { value.Pricing = llm.Pricing{} })
			result, err := runtime.rootNode.host.Call(t.Context(), "models", "call", map[string]any{"prompt": "unknown price"})
			if err != nil || result.(map[string]any)["error"] != nil {
				t.Fatalf("unpriced model was denied: %+v, %v", result, err)
			}
			summary := modelAccountingSummary(t, store, root, "", true)
			if summary.UnknownCostCalls != test.unknown || summary.ReportedCostMicros != test.cost || summary.EstimatedCostMicros != 0 {
				t.Fatalf("unknown price was presented as a known charge: %+v", summary)
			}
			if summary.ReportedCalls != test.reported || summary.EstimatedCalls != test.estimated {
				t.Fatalf("usage presence lost: %+v", summary)
			}
		})
	}
}

func TestModelAccountingAcceptanceRetryChargesEveryAttempt(t *testing.T) {
	var requests atomic.Int32
	store, root, runtime := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		request, ok := modelAccountingRequest(t, w, r)
		if !ok {
			return
		}
		if requests.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":{"message":"transient provider failure"},"usage":{"prompt_tokens":3,"completion_tokens":1,"cost":0.00003}}`)
			return
		}
		usage := modelAccountingUsage()
		usage["cost"] = 0.00005
		modelAccountingReply(w, request, "retry succeeded", usage)
	}, nil, func(value *agent.Agent) { value.Client.MaxRetries = 2 })
	result, err := runtime.rootNode.host.Call(t.Context(), "models", "call", map[string]any{"prompt": "retry model helper"})
	if err != nil || result.(map[string]any)["error"] != nil {
		t.Fatalf("retry result = %+v, %v", result, err)
	}
	summary := modelAccountingSummary(t, store, root, "", true)
	if requests.Load() != 2 || summary.ReportedCalls != 2 || summary.ReportedCostMicros != 80 || summary.EstimatedCostMicros != 0 {
		t.Fatalf("retry lost or duplicated a charge: requests=%d summary=%+v", requests.Load(), summary)
	}
}

func TestModelAccountingAcceptanceOverageRetainsResponseAndStopsEffects(t *testing.T) {
	var requests atomic.Int32
	store, root, _ := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := modelAccountingRequest(t, w, r); !ok {
			return
		}
		requests.Add(1)
		usage := modelAccountingUsage()
		usage["cost"] = 3.25
		modelAccountingToolReply(w, "unexecuted-overage", `state.private_set(key="accounting-unexecuted", value="executed")`, "Completed analysis before the budget overage.", usage)
	}, nil, nil)
	if err := store.SetBudgetLimit(t.Context(), root.ID(), "", session.BudgetCost, 2_000_000); err != nil {
		t.Fatal(err)
	}
	receipt, err := root.Submit(t.Context(), "analyze and update private state")
	if err != nil {
		t.Fatal(err)
	}
	completion := waitReceipt(t, receipt)
	if completion.Err == nil || completion.Output != "Completed analysis before the budget overage." {
		t.Fatalf("overage did not retain its completed answer: %+v", completion)
	}
	_, history, err := store.Load(root.ID())
	if err != nil {
		t.Fatal(err)
	}
	var retained, unexecuted bool
	for _, message := range history {
		if message.Role == "assistant" && message.Content == completion.Output {
			retained = message.Usage != nil && message.Usage.Cost != nil && *message.Usage.Cost == 3.25 && len(message.ToolCalls) == 1
		}
		if message.Role == "tool" && message.ToolCallID == "unexecuted-overage" && strings.Contains(message.Content, "Not executed") {
			unexecuted = true
		}
	}
	if !retained || !unexecuted {
		t.Fatalf("raw transcript lost response usage or pending tool status: %+v", history)
	}
	if _, err := store.GetPrivateState(t.Context(), root.ID(), root.AgentID(), "accounting-unexecuted"); !errors.Is(err, session.ErrStateNotFound) {
		t.Fatalf("tool changed state after budget exhaustion: %v", err)
	}
	budgets, err := root.InspectBudgets(t.Context(), root.AgentID(), root.AgentID())
	if err != nil {
		t.Fatal(err)
	}
	var costBudget *session.BudgetState
	for i := range budgets {
		if budgets[i].Kind == session.BudgetCost {
			costBudget = &budgets[i]
		}
	}
	if costBudget == nil || costBudget.Used != 3_250_000 || (costBudget.Remaining == nil || *costBudget.Remaining != 0) || costBudget.Reserved != 0 {
		t.Fatalf("actual overage or reservation release lost: %+v", costBudget)
	}
	summary := modelAccountingSummary(t, store, root, "", true)
	if summary.ReportedCalls != 1 || summary.ReportedCostMicros != 3_250_000 || summary.EstimatedCostMicros != 0 {
		t.Fatalf("reported overage accounting = %+v", summary)
	}
	receipt, err = root.Submit(t.Context(), "try another model call")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, receipt); completion.Err == nil {
		t.Fatal("a subsequent model call ignored the exhausted monetary budget")
	}
	if requests.Load() != 1 {
		t.Fatal("denied follow-up reached the provider")
	}
	assertModelAccountingUnchanged(t, summary, modelAccountingSummary(t, store, root, "", true))
}

func TestModelAccountingAcceptanceHelperFailureStopsCurrentCell(t *testing.T) {
	for _, failure := range []string{"overage", "settlement"} {
		for _, operation := range []string{"call", "batch"} {
			t.Run(failure+"/"+operation, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "sessions.db")
				var requests atomic.Int32
				call := `models.call(prompt="nested helper")`
				if operation == "batch" {
					call = `models.batch(prompts=["nested helper"])[0]`
				}
				code := "result = " + call + "\nprint(result[\"output\"])\n" + `state.private_set(key="helper-budget-write", value="must not exist")`
				store, root, _ := modelAccountingRuntimeAt(t, path, func(w http.ResponseWriter, r *http.Request) {
					request, ok := modelAccountingRequest(t, w, r)
					if !ok {
						return
					}
					count := requests.Add(1)
					usage := modelAccountingUsage()
					usage["cost"] = 0
					if count == 1 {
						modelAccountingToolReply(w, "nested-helper-cell", code, "", usage)
						return
					}
					if count == 2 {
						if failure == "settlement" {
							db, err := sql.Open("sqlite", path)
							if err != nil {
								t.Error(err)
								return
							}
							defer db.Close()
							if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER helper_settlement_failure BEFORE UPDATE ON model_calls BEGIN SELECT RAISE(ABORT,'injected helper settlement failure'); END`); err != nil {
								t.Error(err)
								return
							}
							usage["cost"] = 0.00019
						} else {
							usage["cost"] = 3.25
						}
						modelAccountingReply(w, request, "Completed helper output remains available.", usage)
						return
					}
					modelAccountingReply(w, request, "unexpected model continuation", usage)
				}, nil, nil)
				if failure == "settlement" {
					// Remove the injector before shutdown so cleanup can retry only the
					// saved settlement, without a provider replay.
					t.Cleanup(func() {
						db, err := sql.Open("sqlite", path)
						if err != nil {
							t.Error(err)
							return
						}
						defer db.Close()
						// Test contexts are already cancelled when cleanup begins.
						cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
						defer cancel()
						if _, err := db.ExecContext(cleanupCtx, `DROP TRIGGER IF EXISTS helper_settlement_failure`); err != nil {
							t.Error(err)
						}
					})
				}
				if err := store.SetBudgetLimit(t.Context(), root.ID(), "", session.BudgetCost, 2_000_000); err != nil {
					t.Fatal(err)
				}
				receipt, err := root.Submit(t.Context(), "run the helper and then update state")
				if err != nil {
					t.Fatal(err)
				}
				if completion := waitReceipt(t, receipt); completion.Err == nil {
					t.Fatal("helper accounting failure did not stop the model turn")
				}
				if requests.Load() != 2 {
					t.Fatalf("model continued after helper accounting failure: %d HTTP requests", requests.Load())
				}
				if _, err := store.GetPrivateState(t.Context(), root.ID(), root.AgentID(), "helper-budget-write"); !errors.Is(err, session.ErrStateNotFound) {
					t.Fatalf("the current cell executed an effect after helper accounting failed: %v", err)
				}
				_, history, err := store.Load(root.ID())
				if err != nil {
					t.Fatal(err)
				}
				var retained bool
				for _, message := range history {
					retained = retained || message.Role == "tool" && strings.Contains(message.Content, "Completed helper output remains available.")
				}
				if !retained {
					t.Fatalf("completed helper output was discarded from the interrupted cell: %+v", history)
				}
				if failure == "settlement" {
					db, err := sql.Open("sqlite", path)
					if err != nil {
						t.Fatal(err)
					}
					_, err = db.ExecContext(t.Context(), `DROP TRIGGER helper_settlement_failure`)
					_ = db.Close()
					if err != nil {
						t.Fatal(err)
					}
					root.Stop()
					summary := modelAccountingSummary(t, store, root, "", true)
					if summary.ReportedCalls != 2 || summary.EstimatedCalls != 0 || summary.ReportedCostMicros != 190 || requests.Load() != 2 {
						t.Fatalf("shutdown lost the retained actual settlement or replayed the provider: %+v, requests=%d", summary, requests.Load())
					}
				}
			})
		}
	}
}

func TestModelAccountingAcceptanceRecursiveRoutePrices(t *testing.T) {
	store, root, runtime := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		request, ok := modelAccountingRequest(t, w, r)
		if ok {
			modelAccountingReply(w, request, "route complete", modelAccountingUsage())
		}
	}, nil, func(value *agent.Agent) {
		value.ResolveModel = func(model, provider string) (agent.ModelRoute, error) {
			pricing := llm.Pricing{Prompt: "0.000004", Completion: "0.000008"}
			if model == "grandchild-model" {
				pricing = llm.Pricing{Prompt: "0.000008", Completion: "0.000016"}
			}
			return agent.ModelRoute{Client: value.Client, ModelName: model, Model: model, Provider: "child-provider", MaxTokens: 128, Pricing: pricing}, nil
		}
	})
	runs := &sync.Map{}
	runtime.setRunTurnHook(observeRunTurn(runs))
	receipt, err := root.Submit(t.Context(), "root task")
	if err != nil || waitReceipt(t, receipt).Err != nil {
		t.Fatalf("root task: %v", err)
	}
	spawn := func(parent *AgentSession, name string) *AgentSession {
		t.Helper()
		result, err := parent.host.Call(t.Context(), "agents", "spawn", map[string]any{
			"name": name, "model": name + "-model", "prompt": "route task", "report": "message",
		})
		if err != nil {
			t.Fatal(err)
		}
		id := result.(map[string]any)["id"].(string)
		waitRunTurn(t, runs, id, 1)
		runtime.mu.RLock()
		node := runtime.agents[id]
		runtime.mu.RUnlock()
		waitAgentIdle(t, node)
		return node
	}
	child := spawn(runtime.rootNode, "child")
	grandchild := spawn(child, "grandchild")
	for _, test := range []struct {
		name string
		id   string
		cost int64
	}{{"root", root.AgentID(), 45}, {"child", child.id, 80}, {"grandchild", grandchild.id, 160}} {
		t.Run(test.name, func(t *testing.T) {
			summary := modelAccountingSummary(t, store, root, test.id, false)
			if summary.Scope != "agent" || summary.ReportedCalls != 1 || summary.EstimatedCostMicros != test.cost || summary.UnknownCostCalls != 0 {
				t.Fatalf("route price or scope mismatch: %+v", summary)
			}
		})
	}
	tree := modelAccountingSummary(t, store, root, "", true)
	if tree.Scope != "subtree" || tree.ReportedCalls != 3 || tree.EstimatedCostMicros != 285 {
		t.Fatalf("tree should include each depth once: %+v", tree)
	}
	childTree := modelAccountingSummary(t, store, root, child.id, true)
	if childTree.ReportedCalls != 2 || childTree.EstimatedCostMicros != 240 {
		t.Fatalf("child subtree includes wrong owners: %+v", childTree)
	}
}

func TestModelAccountingAcceptanceAutomaticAndUnfundedTitles(t *testing.T) {
	var titleRequests atomic.Int32
	store, root, runtime := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		request, ok := modelAccountingRequest(t, w, r)
		if !ok {
			return
		}
		usage, output := modelAccountingUsage(), "root result"
		if request.Model == "title-model" {
			titleRequests.Add(1)
			usage["cost"] = 0
			output = "Accounting Acceptance Results"
		}
		modelAccountingReply(w, request, output, usage)
	}, nil, func(value *agent.Agent) {
		value.CompactClient, value.CompactModel, value.CompactProvider = value.Client, "title-model", "title-provider"
		value.CompactPricing = llm.Pricing{Prompt: "0.1", Completion: "0.2"}
	})
	if result := clientCommand(t, root, "tui", "enable-title", "session.autotitle", protocol.EmptyParams{}); result.Status != "succeeded" {
		t.Fatalf("enable title: %+v", result)
	}
	receipt, err := root.Submit(t.Context(), "Inspect model accounting")
	if err != nil || waitReceipt(t, receipt).Err != nil {
		t.Fatalf("root task: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		meta, _, err := store.Load(root.ID())
		if err != nil {
			t.Fatal(err)
		}
		if meta.Title == "Accounting Acceptance Results" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("automatic title was not published: %q, requests=%d", meta.Title, titleRequests.Load())
		}
		time.Sleep(time.Millisecond)
	}
	summary := modelAccountingSummary(t, store, root, "", true)
	if summary.ReportedCalls != 2 || summary.ReportedCostMicros != 0 || summary.EstimatedCostMicros != 45 || titleRequests.Load() != 1 {
		t.Fatalf("title explicit zero was repriced or unaccounted: %+v, title requests=%d", summary, titleRequests.Load())
	}
	if err := store.SetBudgetLimit(t.Context(), root.ID(), "", session.BudgetTokens, 31); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.rootNode.GenerateTitle(t.Context()); err == nil {
		t.Fatal("unfunded title made a provider request")
	}
	if titleRequests.Load() != 1 {
		t.Fatal("denied title reached the provider")
	}
	assertModelAccountingUnchanged(t, summary, modelAccountingSummary(t, store, root, "", true))
}

func TestModelAccountingAcceptanceFinalAnswerAtTurnCap(t *testing.T) {
	var requests atomic.Int32
	store, root, _ := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		request, ok := modelAccountingRequest(t, w, r)
		if !ok {
			return
		}
		requests.Add(1)
		if len(request.Tools) > 0 {
			modelAccountingToolReply(w, "accounting-cap", "1", "", modelAccountingUsage())
			return
		}
		modelAccountingReply(w, request, "final answer at the cap", modelAccountingUsage())
	}, nil, func(value *agent.Agent) { value.MaxTurns = 1 })
	receipt, err := root.Submit(t.Context(), "perform one computation")
	if err != nil {
		t.Fatal(err)
	}
	completion := waitReceipt(t, receipt)
	if completion.Err != nil || completion.Output != "final answer at the cap" {
		t.Fatalf("capped final answer = %+v", completion)
	}
	summary := modelAccountingSummary(t, store, root, "", true)
	if requests.Load() != 2 || summary.ReportedCalls != 2 || summary.EstimatedCostMicros != 90 {
		t.Fatalf("final call bypassed accounting: requests=%d summary=%+v", requests.Load(), summary)
	}
	_, history, err := store.Load(root.ID())
	if err != nil || history[len(history)-1].Content != completion.Output {
		t.Fatalf("accounted final answer missing from durable transcript: %+v, %v", history, err)
	}
}

func TestModelAccountingAcceptanceDedicatedCompaction(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(fmt.Sprintf("automatic=%t", automatic), func(t *testing.T) {
			var compactRequests atomic.Int32
			history := []llm.Message{{Role: "system", Content: "system"}}
			for turn := range 8 {
				history = append(history,
					llm.Message{Role: "user", Authored: true, Content: fmt.Sprintf("turn %d %s", turn, strings.Repeat("history ", 1000))},
					llm.Message{Role: "assistant", Content: strings.Repeat("response ", 1000)},
				)
			}
			store, root, _ := modelAccountingRuntime(t, func(w http.ResponseWriter, r *http.Request) {
				request, ok := modelAccountingRequest(t, w, r)
				if !ok {
					return
				}
				output := "continued task"
				if request.Model == "compact-model" {
					compactRequests.Add(1)
					output = "Retain the outstanding task and its decisions."
				}
				modelAccountingReply(w, request, output, modelAccountingUsage())
			}, history, func(value *agent.Agent) {
				value.ContextLimit = 4000
				value.CompactClient, value.CompactModel, value.CompactProvider = value.Client, "compact-model", "compact-provider"
				value.CompactPricing = llm.Pricing{Prompt: "0.000007", Completion: "0.000011"}
			})
			if automatic {
				receipt, err := root.Submit(t.Context(), "continue the task")
				if err != nil || waitReceipt(t, receipt).Err != nil {
					t.Fatalf("automatic compaction turn: %v", err)
				}
			} else {
				result := clientCommand(t, root, "tui", "manual-compaction", "history.compact", protocol.EmptyParams{})
				if result.Status != "succeeded" {
					t.Fatalf("manual compaction: %+v", result)
				}
			}
			summary := modelAccountingSummary(t, store, root, "", true)
			wantCalls, wantCost := int64(1), int64(125)
			if automatic {
				wantCalls, wantCost = 2, 170
			}
			if compactRequests.Load() != 1 || summary.ReportedCalls != wantCalls || summary.EstimatedCostMicros != wantCost || summary.UnknownCostCalls != 0 {
				t.Fatalf("dedicated compaction route accounting = %+v, compactions=%d", summary, compactRequests.Load())
			}
			if len(store.Compactions(root.ID())) != 1 {
				t.Fatal("accounted compaction was not persisted")
			}
		})
	}
}
