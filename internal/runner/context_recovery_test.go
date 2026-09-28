package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func longExchange(mailOnly bool) []session.Message {
	raw := compactionMessages(1, 246)
	for i := range raw {
		raw[i].TurnID = "current"
		if mailOnly {
			raw[i].InputID = nil
		}
	}
	raw = append(raw, session.Message{ID: "latest_mail", SessionID: "owner", TurnID: "current", Sequence: 247, Role: session.User, Parts: []session.Part{{Type: "text", Text: "mail is not the opening input"}}})
	raw = append(raw, session.Message{ID: "latest_calls", SessionID: "owner", TurnID: "current", Sequence: 248, Role: session.Assistant, Parts: []session.Part{
		{Type: "tool_call", Call: &session.ToolCall{ID: "first", Name: "execute", Arguments: json.RawMessage(`{}`)}},
		{Type: "tool_call", Call: &session.ToolCall{ID: "second", Name: "execute", Arguments: json.RawMessage(`{}`)}},
	}})
	for i, id := range []string{"first", "second"} {
		raw = append(raw, session.Message{ID: session.MessageID("latest_" + id), SessionID: "owner", TurnID: "current", Sequence: int64(249 + i), Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: id, Output: "exact durable result"}}}})
	}
	return raw
}

func TestLongTurnSplitPinsInputAndRetainsWholeNewestExchange(t *testing.T) {
	for _, mailOnly := range []bool{false, true} {
		t.Run(strconv.FormatBool(mailOnly), func(t *testing.T) {
			ledger := newCompactionLedger(longExchange(mailOnly))
			ledger.live["current"] = true
			var helpers int
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				if request.Purpose == "compaction" {
					helpers++
					if !mailOnly {
						opening := 0
						for _, message := range request.Messages {
							if reflect.DeepEqual(message.Parts, ledger.raw[0].Parts) {
								opening++
							}
						}
						if opening != 1 {
							return model.Response{}, fmt.Errorf("opening input duplicated or lost: %d", opening)
						}
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
				}
				want := 5
				if mailOnly {
					want = 4
				}
				if len(request.Messages) != want {
					return model.Response{}, fmt.Errorf("split context message count %d want %d", len(request.Messages), want)
				}
				tail := request.Messages[len(request.Messages)-3:]
				if tail[0].Parts[0].Call.ID != "first" || tail[0].Parts[1].Call.ID != "second" || tail[1].Parts[0].Result.CallID != "first" || tail[2].Parts[0].Result.CallID != "second" {
					return model.Response{}, errors.New("split orphaned newest tool batch")
				}
				if !mailOnly && !reflect.DeepEqual(request.Messages[1].Parts, ledger.raw[0].Parts) {
					return model.Response{}, errors.New("mail replaced opening input")
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
			})
			r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
			if err != nil || outcome.State != session.Succeeded || helpers != 3 {
				t.Fatalf("outcome=%+v err=%v helpers=%d", outcome, err, helpers)
			}
			selected := ledger.values[*ledger.head.CompactionID]
			wantPins := []session.MessageID{ledger.raw[0].ID}
			if mailOnly {
				wantPins = nil
			}
			if selected.ThroughSequence != 247 || !slices.Equal(selected.PinnedMessageIDs, wantPins) {
				t.Fatalf("wrong exact coverage/pin: %+v", selected)
			}
			// A restarted runner restores pins through the same bounded lookup.
			restored, err := r.selection(t.Context(), "owner")
			if err != nil || len(restored.pins) != len(wantPins) || restored.through() != 247 {
				t.Fatalf("restore=%+v err=%v", restored, err)
			}
			for _, draft := range ledger.drafts {
				if !slices.Equal(draft.PinnedMessageIDs, wantPins) {
					t.Fatalf("intermediate fold lost mandatory pin: %+v", draft)
				}
			}
		})
	}
}

func TestProviderContextRejectionReplansOnceAfterSettledEvidence(t *testing.T) {
	for _, again := range []bool{false, true} {
		ledger := newCompactionLedger(compactionMessages(6, 2))
		var purposes []string
		ordinary := 0
		provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
			purposes = append(purposes, request.Purpose)
			if request.Purpose == "compaction" {
				first := ledger.results["replan_model_1_try_1"]
				if first.State != session.AttemptFailed || first.Usage.Input == nil || *first.Usage.Input != 17 || len(ledger.messages) != 0 {
					return model.Response{}, errors.New("helper ran before exact failed accounting settled")
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
			}
			ordinary++
			if ordinary == 1 || again {
				return model.Response{Usage: session.ModelUsage{Input: new(int64(17))}}, &model.CallError{ContextLimit: true, Message: "context rejected"}
			}
			if len(request.Messages) >= len(ledger.raw) {
				return model.Response{}, errors.New("replan did not shrink request")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
		})
		r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := r.Run(t.Context(), session.Turn{ID: "replan", SessionID: "owner"}, session.Configuration{})
		want := session.Succeeded
		if again {
			want = session.Failed
		}
		if err != nil || outcome.State != want || !slices.Equal(purposes, []string{"", "compaction", ""}) || len(ledger.specs) != 3 || ledger.specs[2].LogicalID != "replan_model_2" {
			t.Fatalf("outcome=%+v err=%v calls=%v attempts=%+v", outcome, err, purposes, ledger.specs)
		}
	}
}

func TestProviderContextRejectionCannotBypassCertaintyOrSettlement(t *testing.T) {
	for _, mode := range []string{"uncertain", "cancelled", "untyped", "prepare", "settlement", "no source", "helper rejection"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ledger := newCompactionLedger(compactionMessages(6, 2))
				if mode == "no source" {
					ledger.raw = compactionMessages(1, 1)
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				ordinary, helpers := 0, 0
				provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
					if mode == "prepare" {
						return model.Prepared{}, &model.CallError{ContextLimit: true, Message: "not dispatched"}
					}
					prepared, err := (model.Scripted{}).Prepare(ctx, request)
					prepared.MaxAttempts = 2
					prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
						if request.Purpose == "compaction" {
							helpers++
							return model.Response{}, &model.CallError{ContextLimit: true, Message: "helper too large"}
						}
						ordinary++
						if mode == "cancelled" {
							cancel()
						}
						if mode == "untyped" {
							return model.Response{}, errors.New("context_length_exceeded")
						}
						if mode == "settlement" {
							ledger.ambiguous = true
							ledger.ordinarySettleErr = errors.New("storage unavailable")
						}
						return model.Response{}, &model.CallError{ContextLimit: true, Uncertain: mode == "uncertain", Message: "context rejected"}
					}
					return prepared, err
				})
				r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
				if err != nil {
					t.Fatal(err)
				}
				outcome, err := r.Run(ctx, session.Turn{ID: "failure", SessionID: "owner"}, session.Configuration{})
				if err == nil && outcome.State != session.Failed {
					t.Fatalf("unexpected success: %+v", outcome)
				}
				wantOrdinary := 1
				if mode == "prepare" {
					wantOrdinary = 0
				}
				wantHelpers := 0
				if mode == "helper rejection" {
					wantHelpers = 1
				}
				if ordinary != wantOrdinary || helpers != wantHelpers {
					t.Fatalf("mode=%s ordinary=%d helpers=%d err=%v", mode, ordinary, helpers, err)
				}
			})
		})
	}
}

func TestContextReplanKeepsOutputCorrectionAndCompletedEffects(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(6, 2))
	executor := &compactionExecutor{ledger: ledger}
	ordinary := 0
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose == "compaction" {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
		}
		ordinary++
		switch ordinary {
		case 1:
			return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "only_once", Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, nil
		case 2:
			return model.Response{Parts: []session.Part{{Type: "text", Text: "42"}}}, nil
		case 3:
			return model.Response{}, &model.CallError{ContextLimit: true, Message: "context rejected"}
		case 4:
			last := request.Messages[len(request.Messages)-1]
			if last.Role != session.System || !strings.Contains(last.Parts[0].Text, "Correct it once") {
				return model.Response{}, errors.New("replan erased correction state")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "43"}}}, nil
		default:
			return model.Response{}, errors.New("unexpected third correction")
		}
	}), ledger, ledger, nil, executor, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "correct", SessionID: "owner"}, session.Configuration{OutputSchema: json.RawMessage(`{"type":"string"}`)})
	if err != nil || outcome.State != session.Failed || outcome.Failure == nil || !strings.HasPrefix(*outcome.Failure, "output_invalid:") || ordinary != 4 || executor.calls != 1 {
		t.Fatalf("outcome=%+v err=%v ordinary=%d effects=%d", outcome, err, ordinary, executor.calls)
	}
}

func TestLongTurnFoldsKeepOneTurnBudget(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(1, 1700))
	ledger.live["old1"] = true
	helperCalls := 0
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose != "compaction" {
			t.Error("ordinary request dispatched after exhausting folds")
		}
		helperCalls++
		return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
	}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "bounded", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Failed || outcome.Failure == nil || !strings.Contains(*outcome.Failure, "16-fold") || helperCalls != 16 || len(ledger.specs) != 16 {
		t.Fatalf("outcome=%+v err=%v helpers=%d attempts=%d", outcome, err, helperCalls, len(ledger.specs))
	}
	for i, spec := range ledger.specs {
		if spec.LogicalID != fmt.Sprintf("bounded_compact_%d", i+1) || ledger.results[spec.ID].State != session.AttemptSucceeded {
			t.Fatalf("fold identity/accounting changed: %+v", spec)
		}
	}
}

func TestLongTurnPinCannotSubsidizeSummaryGrowth(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(1, 10))
	ledger.live["old1"] = true
	ledger.raw[0].Parts[0].Text = strings.Repeat("important opening input ", 1000)
	base := session.Compaction{ID: "base", SessionID: "owner", ThroughSequence: 2, PinnedMessageIDs: []session.MessageID{ledger.raw[0].ID}, Text: "small base"}
	ledger.values[base.ID] = base
	ledger.head.CompactionID = &base.ID
	ledger.head.Revision = 1
	r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
		return model.Response{Parts: []session.Part{{Type: "text", Text: strings.Repeat("x", 1000)}}}, nil
	}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := r.selection(t.Context(), "owner")
	if err != nil {
		t.Fatal(err)
	}
	folds := 0
	_, err = r.foldToBoundary(t.Context(), session.Turn{ID: "growth", SessionID: "owner"}, session.Configuration{}, selected, 4, &folds)
	if err == nil || !strings.Contains(err.Error(), "ineffective") || ledger.head.Revision != 1 || len(ledger.values) != 1 || ledger.results[ledger.specs[0].ID].State != session.AttemptFailed {
		t.Fatalf("retained pin subsidized growth: err=%v head=%+v", err, ledger.head)
	}
}

func TestManualCompactionRepairsOversizedRecentTurnWithoutOrdinaryWork(t *testing.T) {
	ledger := newCompactionLedger(longExchange(false))
	helperCalls := 0
	provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		helperCalls++
		if request.Purpose != "compaction" || request.Instructions != compactionInstructions || len(request.Tools) != 0 {
			return model.Response{}, errors.New("manual repair used ordinary model policy")
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
	})
	r, err := New(provider, ledger, ledger, nil, forbiddenExecutor{}, nil, mailFunc(func(context.Context, session.TurnID) ([]session.Message, error) {
		return nil, errors.New("manual repair consumed mail")
	}), ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "manual", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{OutputSchema: json.RawMessage(`{"const":"never"}`)})
	if err != nil || outcome.State != session.Succeeded || helperCalls != 3 || len(ledger.messages) != 0 || ledger.values[*ledger.head.CompactionID].ThroughSequence != 247 {
		t.Fatalf("outcome=%+v err=%v calls=%d", outcome, err, helperCalls)
	}
}
