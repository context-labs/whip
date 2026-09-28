package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func TestContextPressureUsesKnownInputAndResetsOnProjectionChanges(t *testing.T) {
	prepared := model.Prepared{Snapshot: session.ModelRequestSnapshot{Model: session.ModelSelection{Name: "model"}, Route: "route", Adapter: "adapter"}, ContextWindowTokens: new(int64(100000))}
	var pressure contextPressure
	pressure.sync(prepared, 0)
	if got := pressure.occupancy(100); got != 100 {
		t.Fatal("unknown input must use estimate", got)
	}
	pressure.observe(new(int64(60000)), 100)
	if got := pressure.occupancy(125); got != 60025 {
		t.Fatal("measured input must dominate estimate, plus estimated growth", got)
	}
	pressure.observe(nil, 125)
	if got := pressure.occupancy(150); got != 60050 {
		t.Fatal("missing usage lost last validated observation", got)
	}
	pressure.observe(new(int64(0)), 100)
	if got := pressure.occupancy(125); got != 25 {
		t.Fatal("known zero was treated as unknown", got)
	}
	pressure.observe(new(int64(math.MaxInt64-1)), 100)
	if got := pressure.occupancy(125); got != math.MaxInt64 {
		t.Fatal("growth overflowed", got)
	}
	for _, change := range []string{"route", "model", "adapter", "window", "unknown", "fold"} {
		t.Run(change, func(t *testing.T) {
			value := prepared
			folds := 0
			switch change {
			case "route":
				value.Snapshot.Route = "other"
			case "model":
				value.Snapshot.Model.Effort = "other"
			case "adapter":
				value.Snapshot.Adapter = "other"
			case "window":
				value.ContextWindowTokens = new(int64(200000))
			case "unknown":
				value.ContextWindowTokens = nil
			case "fold":
				folds = 1
			}
			state := contextPressure{}
			state.sync(prepared, 0)
			state.observe(new(int64(60000)), 100)
			state.stalled = true
			state.sync(value, folds)
			if !state.stalled {
				t.Fatal("turn-wide proactive stall was cleared")
			}
			if got := state.occupancy(125); got != 125 {
				t.Fatal("stale measurement survived projection change", got)
			}
		})
	}
	if got := pressureThreshold(math.MaxInt64, 100); got != math.MaxInt64 {
		t.Fatal("threshold overflowed", got)
	}
	if pressureThreshold(101, 50) != 51 || pressureThreshold(100, 0) != 50 {
		t.Fatal("threshold rounding/default changed")
	}
}

func TestProactivePostFinalUsesSettledOrdinaryUsageAndCapturedHelper(t *testing.T) {
	for _, mode := range []string{"measured", "unknown window", "unknown usage", "zero usage", "invalid usage", "higher threshold"} {
		t.Run(mode, func(t *testing.T) {
			ledger := newCompactionLedger(compactionMessages(6, 2))
			preview := &compactionPreview{}
			ordinary, helpers, preparedCalls := 0, 0, 0
			helperModel := session.ModelSelection{Provider: "helper", Name: "summary", Effort: "low"}
			configuration := session.Configuration{Model: session.ModelSelection{Provider: "ordinary", Name: "chat"}, Compaction: session.CompactionPolicy{Model: &helperModel}}
			if mode == "higher threshold" {
				configuration.Compaction.ThresholdPercent = 75
			}
			provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
				preparedCalls++
				prepared, err := model.Scripted{}.Prepare(ctx, request)
				if err != nil {
					return prepared, err
				}
				prepared.ContextWindowTokens = new(int64(100000))
				if mode == "unknown window" {
					prepared.ContextWindowTokens = nil
				}
				prepared.Snapshot.InputTokenBound = new(int64(math.MaxInt64))
				prepared.Snapshot.Prices.Input = new(int64(17))
				prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
					if request.Purpose == "compaction" {
						helpers++
						if request.Selection != helperModel || request.Instructions != compactionInstructions || len(request.Tools) != 0 || len(ledger.messages) != 1 || ledger.results["pressure_model_1_try_1"].State != session.AttemptSucceeded {
							return model.Response{}, errors.New("helper did not follow captured route and durable final response")
						}
						return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}, Usage: session.ModelUsage{Input: new(int64(90000))}}, nil
					}
					ordinary++
					input := new(int64(60000))
					switch mode {
					case "unknown usage":
						input = nil
					case "zero usage":
						input = new(int64(0))
					case "invalid usage":
						input = new(int64(-1))
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "final answer"}}, Usage: session.ModelUsage{Input: input}}, nil
				}
				return prepared, nil
			})
			r, err := New(provider, ledger, ledger, nil, nil, preview, nil, ledger, nil)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := r.Run(t.Context(), session.Turn{ID: "pressure", SessionID: "owner"}, configuration)
			wantHelpers := 0
			if mode == "measured" {
				wantHelpers = 1
			}
			if err != nil || outcome.State != session.Succeeded || ordinary != 1 || helpers != wantHelpers || preview.calls != 1 || preparedCalls != 1+wantHelpers || len(ledger.messages) != 1 {
				t.Fatalf("outcome=%+v err=%v ordinary=%d helpers=%d previews=%d prepares=%d", outcome, err, ordinary, helpers, preview.calls, preparedCalls)
			}
			if wantHelpers != 0 {
				spec := ledger.specs[1]
				if spec.Request.Model != helperModel || spec.Request.Purpose != "compaction" || *spec.Request.Prices.Input != 17 || *ledger.results[spec.ID].Usage.Input != 90000 {
					t.Fatal("helper lost captured accounting snapshot", spec)
				}
			}
		})
	}
}

func TestProactiveNoSourceRechecksAfterNewExchangeThenStalls(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(1, 1))
	ledger.raw[0].TurnID = "current"
	ledger.raw[0].Parts[0].Text = strings.Repeat("retained opening ", 100)
	ledger.live["current"] = true
	executor := &compactionExecutor{ledger: ledger}
	ordinary, helpers := 0, 0
	provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
		prepared, err := model.Scripted{}.Prepare(ctx, request)
		if err != nil {
			return prepared, err
		}
		prepared.ContextWindowTokens = new(int64(100))
		prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
			if request.Purpose == "compaction" {
				helpers++
				if ordinary != 2 || executor.calls != 2 {
					return model.Response{}, errors.New("no-source check dispatched early or stalled planner repeated")
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
			}
			ordinary++
			if ordinary == 5 {
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			}
			return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: fmt.Sprintf("cell%d", ordinary), Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, nil
		}
		return prepared, nil
	})
	r, err := New(provider, ledger, ledger, nil, executor, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || ordinary != 5 || helpers != 1 || executor.calls != 4 {
		t.Fatalf("outcome=%+v err=%v ordinary=%d helpers=%d effects=%d", outcome, err, ordinary, helpers, executor.calls)
	}
	selected := ledger.values[*ledger.head.CompactionID]
	if selected.ThroughSequence != 3 || !slices.Equal(selected.PinnedMessageIDs, []session.MessageID{ledger.raw[0].ID}) {
		t.Fatal("proactive fold lost exact opening or latest exchange", selected)
	}
}

func TestProactiveFirstFinalWithoutReplaceableSourceSucceeds(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(1, 1))
	ledger.raw[0].TurnID = "current"
	ledger.live["current"] = true
	calls := 0
	provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
		prepared, err := model.Scripted{}.Prepare(ctx, request)
		prepared.ContextWindowTokens = new(int64(100000))
		prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
			calls++
			return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}, Usage: session.ModelUsage{Input: new(int64(60000))}}, nil
		}
		return prepared, err
	})
	r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || calls != 1 || len(ledger.values) != 0 {
		t.Fatalf("outcome=%+v err=%v calls=%d summaries=%d", outcome, err, calls, len(ledger.values))
	}
}

func TestProactiveCorrectionPreservesDurableEffectsAndSingleCorrection(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(6, 2))
	executor := &compactionExecutor{ledger: ledger}
	ordinary, helpers := 0, 0
	provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
		prepared, err := model.Scripted{}.Prepare(ctx, request)
		if err != nil {
			return prepared, err
		}
		prepared.ContextWindowTokens = new(int64(100000))
		prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
			if request.Purpose == "compaction" {
				helpers++
				return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}, Usage: session.ModelUsage{Input: new(int64(90000))}}, nil
			}
			ordinary++
			switch ordinary {
			case 1:
				return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "cell", Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, nil
			case 2:
				return model.Response{Parts: []session.Part{{Type: "text", Text: "42"}}, Usage: session.ModelUsage{Input: new(int64(60000))}}, nil
			default:
				last := request.Messages[len(request.Messages)-1]
				if helpers != 1 || executor.calls != 1 || last.Role != session.System || !strings.Contains(last.Parts[0].Text, "Correct it once") {
					return model.Response{}, errors.New("proactive fold lost correction or replayed an effect")
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: `"done"`}}}, nil
			}
		}
		return prepared, nil
	})
	r, err := New(provider, ledger, ledger, nil, executor, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{OutputSchema: json.RawMessage(`{"type":"string"}`)})
	if err != nil || outcome.State != session.Succeeded || ordinary != 3 || helpers != 1 || executor.calls != 1 || len(ledger.messages) != 3 {
		t.Fatalf("outcome=%+v err=%v ordinary=%d helpers=%d effects=%d", outcome, err, ordinary, helpers, executor.calls)
	}
}

func TestProactivePostFinalFailureRetainsAnswerAndNeverRedispatches(t *testing.T) {
	for _, mode := range []string{"route", "budget", "provider", "cancel", "settlement", "lost acknowledgement"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ledger := newCompactionLedger(compactionMessages(6, 2))
				ordinary, helpers := 0, 0
				provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
					if request.Purpose == "compaction" && mode == "route" {
						return model.Prepared{}, errors.New("explicit helper route is unavailable")
					}
					prepared, err := model.Scripted{}.Prepare(ctx, request)
					prepared.ContextWindowTokens = new(int64(100000))
					prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
						if request.Purpose == "compaction" {
							helpers++
							switch mode {
							case "provider":
								return model.Response{Usage: session.ModelUsage{Input: new(int64(10))}}, &model.CallError{Message: "helper rejected"}
							case "cancel":
								cancel()
								return model.Response{Usage: session.ModelUsage{Input: new(int64(10))}}, context.Canceled
							}
							return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
						}
						ordinary++
						switch mode {
						case "budget":
							ledger.reserveErr = errors.New("helper budget exhausted")
						case "settlement":
							ledger.settleErr = errors.New("helper SQL unavailable")
						case "lost acknowledgement":
							ledger.loseReply = true
						}
						return model.Response{Parts: []session.Part{{Type: "text", Text: "durable final answer"}}, Usage: session.ModelUsage{Input: new(int64(60000))}}, nil
					}
					return prepared, err
				})
				r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
				if err != nil {
					t.Fatal(err)
				}
				outcome, err := r.Run(ctx, session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{Compaction: session.CompactionPolicy{Model: &session.ModelSelection{Provider: "helper", Name: "summary"}}})
				want := session.Failed
				if mode == "lost acknowledgement" {
					want = session.Succeeded
				}
				wantHelpers := 1
				if mode == "route" || mode == "budget" {
					wantHelpers = 0
				}
				if err != nil || outcome.State != want || ordinary != 1 || helpers != wantHelpers || len(ledger.messages) != 1 || ledger.messages["current_model_1_answer"].Parts[0].Text != "durable final answer" {
					t.Fatalf("outcome=%+v err=%v ordinary=%d helpers=%d messages=%+v", outcome, err, ordinary, helpers, ledger.messages)
				}
				if mode == "provider" || mode == "cancel" {
					result := ledger.results["current_compact_1_try_1"]
					if result.Usage.Input == nil || *result.Usage.Input != 10 {
						t.Fatal("failed helper lost usage", result)
					}
					if mode == "cancel" && result.State != session.AttemptUncertain {
						t.Fatal("cancelled helper lost uncertainty", result)
					}
				}
			})
		})
	}
}

func TestCapturedHelperSelectionAppliesToEveryCompactionTrigger(t *testing.T) {
	for _, trigger := range []string{"manual", "local", "reactive", "proactive"} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%t", trigger, explicit), func(t *testing.T) {
				raw := compactionMessages(6, 2)
				if trigger == "local" {
					raw = compactionMessages(6, 20)
				}
				ledger := newCompactionLedger(raw)
				conversation := session.ModelSelection{Provider: "chat", Name: "conversation", Effort: "high"}
				helper := session.ModelSelection{Provider: "summary", Name: "helper", Effort: "low"}
				configuration := session.Configuration{Model: conversation}
				wantSelection := conversation
				if explicit {
					configuration.Compaction.Model = &helper
					wantSelection = helper
				}
				ordinary, helpers := 0, 0
				provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
					prepared, err := model.Scripted{}.Prepare(ctx, request)
					if err != nil {
						return prepared, err
					}
					if trigger == "proactive" {
						prepared.ContextWindowTokens = new(int64(100))
					}
					prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
						if request.Purpose == "compaction" {
							helpers++
							if request.Selection != wantSelection {
								return model.Response{}, errors.New("helper used wrong captured selection")
							}
							return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
						}
						ordinary++
						if request.Selection != conversation {
							return model.Response{}, errors.New("helper selection leaked into conversation")
						}
						if trigger == "reactive" && ordinary == 1 {
							return model.Response{}, &model.CallError{ContextLimit: true, Message: "context rejected"}
						}
						return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
					}
					return prepared, nil
				})
				r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
				if err != nil {
					t.Fatal(err)
				}
				turn := session.Turn{ID: "current", SessionID: "owner"}
				wantOrdinary := 1
				if trigger == "manual" {
					turn.Kind, wantOrdinary = session.CompactInput, 0
				}
				if trigger == "reactive" {
					wantOrdinary = 2
				}
				outcome, err := r.Run(t.Context(), turn, configuration)
				if err != nil || outcome.State != session.Succeeded || helpers != 1 || ordinary != wantOrdinary {
					t.Fatalf("outcome=%+v err=%v helpers=%d ordinary=%d", outcome, err, helpers, ordinary)
				}
			})
		}
	}
}

func TestOrdinaryRouteChangeInvalidatesReportedOccupancy(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(6, 2))
	executor := &compactionExecutor{ledger: ledger}
	ordinary := 0
	provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
		prepared, err := model.Scripted{}.Prepare(ctx, request)
		prepared.ContextWindowTokens = new(int64(100000))
		prepared.Snapshot.Route = fmt.Sprintf("route%d", ordinary)
		prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
			if request.Purpose == "compaction" {
				return model.Response{}, errors.New("previous route usage triggered a helper")
			}
			ordinary++
			if ordinary == 1 {
				return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "cell", Name: "execute", Arguments: json.RawMessage(`{}`)}}}, Usage: session.ModelUsage{Input: new(int64(60000))}}, nil
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
		}
		return prepared, err
	})
	r, err := New(provider, ledger, ledger, nil, executor, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || ordinary != 2 || len(ledger.values) != 0 {
		t.Fatalf("outcome=%+v err=%v ordinary=%d summaries=%d", outcome, err, ordinary, len(ledger.values))
	}
}

func TestProactiveUnknownWindowDoesNotHydrateFinalProjection(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(6, 2))
	calls := 0
	provider := providerFunc(func(context.Context, model.Request) (model.Response, error) {
		calls++
		return model.Response{Parts: []session.Part{{Type: "content", ReferenceID: "final_image"}}, Usage: session.ModelUsage{Input: new(int64(60000))}}, nil
	})
	r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || calls != 1 || len(ledger.messages) != 1 {
		t.Fatalf("disabled proactive mode read final content: outcome=%+v err=%v calls=%d", outcome, err, calls)
	}
}

func TestProactiveTinySourceCannotBeatRequiredSummaryEnvelope(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(5, 1))
	for i := range ledger.raw {
		ledger.raw[i].Parts[0].Text = "x"
	}
	calls := 0
	provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
		prepared, err := model.Scripted{}.Prepare(ctx, request)
		prepared.ContextWindowTokens = new(int64(2))
		prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
			calls++
			return model.Response{Parts: []session.Part{{Type: "text", Text: "x"}}}, nil
		}
		return prepared, err
	})
	r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || calls != 1 || len(ledger.values) != 0 {
		t.Fatalf("impossible shrink dispatched a helper: outcome=%+v err=%v calls=%d", outcome, err, calls)
	}
}

func TestProactivePreservesValidatedInputAcrossOrdinaryRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ledger := newCompactionLedger(compactionMessages(6, 2))
		ordinary, helpers := 0, 0
		provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
			prepared, err := model.Scripted{}.Prepare(ctx, request)
			prepared.ContextWindowTokens = new(int64(100000))
			prepared.MaxAttempts = 2
			prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
				if request.Purpose == "compaction" {
					helpers++
					return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
				}
				ordinary++
				if ordinary == 1 {
					return model.Response{Usage: session.ModelUsage{Input: new(int64(60000))}}, &model.CallError{Retryable: true, Message: "confirmed temporary rejection"}
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			}
			return prepared, err
		})
		r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
		if err != nil || outcome.State != session.Succeeded || ordinary != 2 || helpers != 1 || len(ledger.specs) != 3 {
			t.Fatalf("outcome=%+v err=%v ordinary=%d helpers=%d attempts=%d", outcome, err, ordinary, helpers, len(ledger.specs))
		}
		if ledger.results["current_model_1_try_2"].Usage.Input != nil || ledger.results["current_model_1_try_1"].State != session.AttemptFailed {
			t.Fatal("planning observation changed durable per-attempt evidence")
		}
	})
}
