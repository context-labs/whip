package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

type compactionLedger struct {
	*outputLedger
	raw               []session.Message
	head              session.ContextHead
	values            map[session.CompactionID]session.Compaction
	drafts            map[session.ModelAttemptID]session.CompactionDraft
	reserveErr        error
	settleErr         error
	ordinarySettleErr error
	loseReply         bool
	unselected        bool
	live              map[session.TurnID]bool
}

func newCompactionLedger(raw []session.Message) *compactionLedger {
	return &compactionLedger{outputLedger: newOutputLedger(), raw: raw, head: session.ContextHead{SessionID: "owner"}, values: map[session.CompactionID]session.Compaction{}, drafts: map[session.ModelAttemptID]session.CompactionDraft{}, live: map[session.TurnID]bool{}}
}

func (s *compactionLedger) ContextPins(_ context.Context, owner session.SessionID, ids []session.MessageID) ([]session.Message, error) {
	var result []session.Message
	for _, message := range s.raw {
		if slices.Contains(ids, message.ID) && message.SessionID == owner && message.OpeningInput && message.Role == session.User {
			result = append(result, message)
		}
	}
	if len(result) != len(ids) {
		return nil, errors.New("missing exact input pin")
	}
	return result, nil
}

//nolint:nilnil // A fully covered terminal or mail-only turn requires no input pin.
func (s *compactionLedger) ContextBoundaryPin(_ context.Context, _ session.SessionID, through int64) (*session.MessageID, error) {
	var group session.HistoryGroupID
	partial := false
	for _, message := range s.raw {
		if message.Sequence == through {
			group = message.GroupID
			partial = message.TurnID != "" && s.live[message.TurnID]
		}
	}
	var pin *session.MessageID
	for _, message := range s.raw {
		if message.GroupID != group {
			continue
		}
		if message.Sequence > through {
			partial = true
		}
		if message.OpeningInput {
			id := message.ID
			pin = &id
		}
	}
	if !partial {
		return nil, nil
	}
	return pin, nil
}

func (s *compactionLedger) ReserveModelAttempt(ctx context.Context, spec session.ModelAttemptSpec) (session.ModelAttempt, error) {
	if s.reserveErr != nil {
		return session.ModelAttempt{}, s.reserveErr
	}
	return s.outputLedger.ReserveModelAttempt(ctx, spec)
}

func (s *compactionLedger) SettleModelAttempt(ctx context.Context, id session.ModelAttemptID, result session.ModelAttemptResult, draft *session.MessageDraft) (session.ModelAttempt, error) {
	if s.ordinarySettleErr != nil {
		return session.ModelAttempt{}, s.ordinarySettleErr
	}
	exists := false
	if draft != nil {
		_, exists = s.messages[draft.ID]
	}
	value, err := s.outputLedger.SettleModelAttempt(ctx, id, result, draft)
	if draft != nil && !exists {
		var turn session.TurnID
		for _, spec := range s.specs {
			if spec.ID == id {
				turn = spec.TurnID
			}
		}
		s.recordMessage(turn, draft.ID, draft.Role, draft.Parts)
	}
	return value, err
}

func (s *compactionLedger) recordMessage(turn session.TurnID, id session.MessageID, role session.Role, parts []session.Part) {
	sequence := int64(1)
	if len(s.raw) != 0 {
		sequence = s.raw[len(s.raw)-1].Sequence + 1
	}
	s.raw = append(s.raw, session.Message{ID: id, SessionID: "owner", GroupID: session.HistoryGroupID(turn), TurnID: turn, Sequence: sequence, Role: role, Parts: parts})
}

func (s *compactionLedger) ContextHead(context.Context, session.SessionID) (session.ContextHead, error) {
	return s.head, nil
}

func (s *compactionLedger) Compaction(_ context.Context, _ session.SessionID, id session.CompactionID) (session.Compaction, error) {
	value, ok := s.values[id]
	if !ok {
		return value, errors.New("missing compaction")
	}
	return value, nil
}

func (s *compactionLedger) HistorySnapshot(context.Context, session.SessionID) (session.HistorySnapshot, error) {
	var through int64
	if len(s.raw) != 0 {
		through = s.raw[len(s.raw)-1].Sequence
	}
	return session.HistorySnapshot{SessionID: "owner", ThroughSequence: through, MessageCount: int64(len(s.raw))}, nil
}

func (s *compactionLedger) HistoryRange(_ context.Context, _ session.SessionID, after, through int64, limit int) ([]session.Message, error) {
	var values []session.Message
	for _, message := range s.raw {
		if message.Sequence > after && message.Sequence <= through && len(values) < limit {
			values = append(values, message)
		}
	}
	return values, nil
}

func (s *compactionLedger) ContextTail(_ context.Context, _ session.SessionID, through int64, keep int) (int64, error) {
	var groups []session.HistoryGroupID
	for _, message := range slices.Backward(s.raw) {
		if message.Sequence <= through && !slices.Contains(groups, message.GroupID) {
			groups = append(groups, message.GroupID)
			if len(groups) == keep {
				break
			}
		}
	}
	for _, message := range s.raw {
		if slices.Contains(groups, message.GroupID) {
			return message.Sequence - 1, nil
		}
	}
	return 0, nil
}

func (s *compactionLedger) SettleCompaction(ctx context.Context, id session.ModelAttemptID, outcome session.ModelAttemptResult, draft *session.CompactionDraft) (session.CompactionSettlement, error) {
	if err := ctx.Err(); err != nil {
		return session.CompactionSettlement{}, err
	}
	if s.settleErr != nil {
		return session.CompactionSettlement{}, s.settleErr
	}
	s.writes[id]++
	s.results[id] = outcome
	result := session.CompactionSettlement{Head: s.head}
	if draft != nil {
		if previous, ok := s.drafts[id]; ok && !reflect.DeepEqual(previous, *draft) {
			return result, errors.New("settlement retry changed evidence")
		}
		s.drafts[id] = *draft
		value, exists := s.values[draft.ID]
		if !exists {
			value = session.Compaction{ID: draft.ID, SessionID: "owner", AttemptID: id, BaseID: draft.BaseID, ExpectedRevision: draft.ExpectedRevision, ThroughSequence: draft.ThroughSequence, PinnedMessageIDs: slices.Clone(draft.PinnedMessageIDs), TextBytes: int64(len(draft.Text)), Text: draft.Text}
			s.values[draft.ID] = value
			if !s.unselected {
				s.head.Revision++
				s.head.CompactionID = &value.ID
			}
		}
		result.Compaction, result.Head = &value, s.head
		result.Selected = s.head.CompactionID != nil && *s.head.CompactionID == value.ID
	}
	if s.loseReply && s.writes[id] == 1 {
		return result, errors.New("lost compaction commit acknowledgement")
	}
	return result, nil
}

func compactionMessages(turns, each int) []session.Message {
	var messages []session.Message
	for turn := 1; turn <= turns; turn++ {
		for i := range each {
			sequence := int64(len(messages) + 1)
			role := session.User
			if i%2 != 0 {
				role = session.Assistant
			}
			message := session.Message{ID: session.MessageID(fmt.Sprintf("m%d", sequence)), SessionID: "owner", GroupID: session.HistoryGroupID(fmt.Sprintf("old%d", turn)), TurnID: session.TurnID(fmt.Sprintf("old%d", turn)), Sequence: sequence, Role: role, Parts: []session.Part{{Type: "text", Text: fmt.Sprintf("source %d with important exact context", sequence)}}}
			if i == 0 {
				message.OpeningInput = true
				message.InputID = new(session.InputID(fmt.Sprintf("input%d", turn)))
			}
			messages = append(messages, message)
		}
	}
	return messages
}

type compactionPreview struct{ calls int }

func (p *compactionPreview) BeginPreview(session.Turn, session.ModelAttemptID, session.MessageID) (func(model.Chunk), func()) {
	p.calls++
	return func(model.Chunk) {}, func() {}
}

type forbiddenExecutor struct{}

func (forbiddenExecutor) Instructions(context.Context, session.Turn, session.Instructions) (string, error) {
	return "", errors.New("manual compaction loaded executor instructions")
}

func (forbiddenExecutor) Execute(context.Context, session.Turn, session.MessageID, session.ToolCall) ([]session.Part, error) {
	return nil, errors.New("manual compaction executed a tool")
}

func TestManualCompactionSettlesOnlyHelperAndRestoresSelectedContext(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(6, 2))
	ledger.loseReply = true
	before := slices.Clone(ledger.raw)
	preview := &compactionPreview{}
	calls := 0
	provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		calls++
		if request.Purpose != "compaction" || len(request.Tools) != 0 || len(request.Messages) != 4 || request.Instructions != compactionInstructions {
			return model.Response{}, errors.New("helper leaked ordinary execution policy or chose wrong source")
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "preserved facts"}}, Usage: session.ModelUsage{Output: new(int64(3))}}, nil
	})
	mail := mailFunc(func(context.Context, session.TurnID) ([]session.Message, error) {
		return nil, errors.New("manual compaction observed mail")
	})
	r, err := New(provider, ledger, ledger, nil, forbiddenExecutor{}, preview, mail, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "compact", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{Instructions: session.Instructions{Text: "ordinary instructions"}, OutputSchema: json.RawMessage(`{"const":"not a summary"}`)})
	if err != nil || outcome.State != session.Succeeded || calls != 1 || preview.calls != 0 || len(ledger.messages) != 0 || !reflect.DeepEqual(before, ledger.raw) {
		t.Fatalf("helper outcome=%+v err=%v calls=%d preview=%d transcript=%v", outcome, err, calls, preview.calls, ledger.messages)
	}
	spec := ledger.specs[0]
	if spec.Request.Purpose != "compaction" || ledger.writes[spec.ID] != 2 || ledger.results[spec.ID].Usage.Output == nil || *ledger.results[spec.ID].Usage.Output != 3 {
		t.Fatalf("helper accounting bypassed or replayed: %+v %+v", spec, ledger.results[spec.ID])
	}
	selected := ledger.values[*ledger.head.CompactionID]
	if selected.ThroughSequence != 4 {
		t.Fatal("wrong raw prefix", selected)
	}
	// A fresh runner uses the persisted selection and captured tail, rather than
	// hidden in-memory state or a second authored transcript message.
	r, err = New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if len(request.Messages) != 9 || request.Messages[0].Role != session.User || !strings.Contains(request.Messages[0].Parts[0].Text, "untrusted_context_summary") || !strings.Contains(request.Messages[0].Parts[0].Text, "preserved facts") || !reflect.DeepEqual(request.Messages[1].Parts, ledger.raw[4].Parts) {
			return model.Response{}, errors.New("restart did not project selected summary and exact raw tail")
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "next reply"}}}, nil
	}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err = r.Run(t.Context(), session.Turn{ID: "next", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || len(ledger.specs) != 2 || ledger.specs[1].Request.Purpose != "turn" {
		t.Fatalf("ordinary context=%+v err=%v attempts=%+v", outcome, err, ledger.specs)
	}
}

func TestCompactionNoFold(t *testing.T) {
	for _, tc := range []struct {
		name        string
		turns, each int
		kind        session.InputKind
		want        session.TurnState
	}{
		{"empty", 0, 0, session.CompactInput, session.Succeeded},
		{"four retained", 4, 2, session.CompactInput, session.Succeeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ledger := newCompactionLedger(compactionMessages(tc.turns, tc.each))
			r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
				t.Error("unexpected model dispatch")
				return model.Response{}, nil
			}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
			if err != nil {
				t.Fatal(err)
			}
			outcome, err := r.Run(t.Context(), session.Turn{ID: "turn", SessionID: "owner", Kind: tc.kind}, session.Configuration{})
			if err != nil || outcome.State != tc.want || len(ledger.specs) != 0 {
				t.Fatalf("outcome=%+v err=%v attempts=%d", outcome, err, len(ledger.specs))
			}
		})
	}
}

func TestAutomaticCompactionRebuildsWithinMessageBound(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(53, 2))
	var purposes []string
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		purposes = append(purposes, request.Purpose)
		if len(request.Messages) > maxContextMessages {
			return model.Response{}, errors.New("oversized request dispatched")
		}
		if request.Purpose == "compaction" {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
		}
		if len(request.Messages) != 9 {
			return model.Response{}, errors.New("automatic compaction dropped retained turns")
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "answer"}}}, nil
	}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "auto", SessionID: "owner"}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || !slices.Equal(purposes, []string{"compaction", ""}) || len(ledger.messages) != 1 {
		t.Fatalf("outcome=%+v err=%v purposes=%v", outcome, err, purposes)
	}
}

func TestCompactionPreservesToolBatchesAcrossIncrementalFolds(t *testing.T) {
	raw := compactionMessages(1, 98)
	call := session.Message{ID: "calls", SessionID: "owner", GroupID: "old1", TurnID: "old1", Sequence: 99, Role: session.Assistant, Parts: []session.Part{
		{Type: "tool_call", Call: &session.ToolCall{ID: "a", Name: "execute", Arguments: json.RawMessage(`{"code":"a"}`)}},
		{Type: "tool_call", Call: &session.ToolCall{ID: "b", Name: "execute", Arguments: json.RawMessage(`{"code":"b"}`)}},
	}}
	raw = append(raw, call)
	for i, id := range []string{"a", "b"} {
		raw = append(raw, session.Message{ID: session.MessageID("result_" + id), SessionID: "owner", GroupID: "old1", TurnID: "old1", Sequence: int64(100 + i), Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: id, Output: "exact result"}}}})
	}
	for _, message := range compactionMessages(4, 1) {
		message.Sequence += 101
		message.ID = session.MessageID(fmt.Sprintf("tail%d", message.Sequence))
		message.TurnID = session.TurnID(fmt.Sprintf("tail%d", message.Sequence))
		message.GroupID = session.HistoryGroupID(message.TurnID)
		raw = append(raw, message)
	}
	ledger := newCompactionLedger(raw)
	var boundaries []int
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		boundaries = append(boundaries, len(request.Messages))
		pending := map[string]bool{}
		for _, message := range request.Messages {
			for _, part := range message.Parts {
				if part.Call != nil {
					pending[part.Call.ID] = true
				}
				if part.Result != nil {
					if !pending[part.Result.CallID] {
						return model.Response{}, errors.New("unpaired result")
					}
					delete(pending, part.Result.CallID)
				}
			}
		}
		if len(pending) != 0 {
			return model.Response{}, errors.New("assistant batch cut before all results")
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
	}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "batches", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || !slices.Equal(boundaries, []int{98, 5}) {
		t.Fatalf("outcome=%+v err=%v batches=%v", outcome, err, boundaries)
	}
	if len(ledger.values) != 2 || ledger.values[*ledger.head.CompactionID].ThroughSequence != 101 {
		t.Fatalf("wrong incremental coverage: %+v", ledger.values)
	}
}

func TestCompactionFailureNeverRedispatchesOrPublishesTranscript(t *testing.T) {
	for _, mode := range []string{"budget", "cancel", "settlement", "unselected", "oversized", "tool", "ineffective"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ledger := newCompactionLedger(compactionMessages(5, 2))
				if mode == "budget" {
					ledger.reserveErr = errors.New("budget exhausted")
				}
				if mode == "settlement" {
					ledger.settleErr = errors.New("storage offline")
				}
				ledger.unselected = mode == "unselected"
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				calls := 0
				r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
					calls++
					switch mode {
					case "cancel":
						cancel()
						return model.Response{}, ctx.Err()
					case "oversized":
						return model.Response{Parts: []session.Part{{Type: "text", Text: strings.Repeat("x", session.MaxCompactionBytes+1)}}}, nil
					case "ineffective":
						return model.Response{Parts: []session.Part{{Type: "text", Text: strings.Repeat("x", 1000)}}}, nil
					case "tool":
						return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, nil
					default:
						return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
					}
				}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
				if err != nil {
					t.Fatal(err)
				}
				outcome, err := r.Run(ctx, session.Turn{ID: "failure", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{})
				wantCalls := 1
				if mode == "budget" {
					wantCalls = 0
				}
				if err != nil || outcome.State != session.Failed || calls != wantCalls || len(ledger.messages) != 0 {
					t.Fatalf("outcome=%+v err=%v calls=%d transcript=%v", outcome, err, calls, ledger.messages)
				}
				if mode == "cancel" && ledger.results[ledger.specs[0].ID].State != session.AttemptUncertain {
					t.Fatal("cancelled dispatch lost uncertain accounting")
				}
			})
		})
	}
}

func TestCompactionRestoresExactPinsAndDropsCompletedOnes(t *testing.T) {
	for _, missing := range []bool{false, true} {
		ledger := newCompactionLedger(compactionMessages(6, 2))
		pin := ledger.raw[0].ID
		if missing {
			pin = "missing"
		}
		base := session.Compaction{ID: "base", SessionID: "owner", ThroughSequence: 2, PinnedMessageIDs: []session.MessageID{pin}, Text: "previous exact summary"}
		ledger.values[base.ID] = base
		ledger.head.CompactionID, ledger.head.Revision = &base.ID, 1
		calls := 0
		r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
			calls++
			if len(request.Messages) != 4 || request.Messages[0].Role != session.User || !strings.Contains(request.Messages[0].Parts[0].Text, "previous exact summary") || !reflect.DeepEqual(request.Messages[1].Parts, ledger.raw[0].Parts) {
				return model.Response{}, errors.New("summary or exact pin missing from helper input")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
		}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := r.Run(t.Context(), session.Turn{ID: "pins", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{})
		if missing {
			if err == nil || calls != 0 {
				t.Fatalf("missing pin was silently skipped: outcome=%+v err=%v calls=%d", outcome, err, calls)
			}
			continue
		}
		if err != nil || outcome.State != session.Succeeded || calls != 1 || len(ledger.values[*ledger.head.CompactionID].PinnedMessageIDs) != 0 {
			t.Fatalf("pins changed: outcome=%+v err=%v head=%+v", outcome, err, ledger.head)
		}
	}
}

type preparedProvider func(context.Context, model.Request) (model.Prepared, error)

func (p preparedProvider) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	return p(ctx, request)
}

func TestCompactionRetriesOnlyExplicitProviderFailureWithFreshAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ledger := newCompactionLedger(compactionMessages(5, 2))
		calls := 0
		provider := preparedProvider(func(ctx context.Context, request model.Request) (model.Prepared, error) {
			prepared, err := (model.Scripted{}).Prepare(ctx, request)
			prepared.MaxAttempts = 2
			prepared.Execute = func(context.Context, func(model.Chunk)) (model.Response, error) {
				calls++
				if calls == 1 {
					return model.Response{Usage: session.ModelUsage{Output: new(int64(2))}}, &model.CallError{Retryable: true, Message: "provider rejected"}
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
			}
			return prepared, err
		})
		r, err := New(provider, ledger, ledger, nil, nil, nil, nil, ledger, nil)
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := r.Run(t.Context(), session.Turn{ID: "retry", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{})
		if err != nil || outcome.State != session.Succeeded || calls != 2 || len(ledger.specs) != 2 || ledger.specs[0].ID == ledger.specs[1].ID || ledger.specs[0].LogicalID != ledger.specs[1].LogicalID || len(ledger.values) != 1 {
			t.Fatalf("outcome=%+v err=%v calls=%d attempts=%+v", outcome, err, calls, ledger.specs)
		}
		if ledger.results[ledger.specs[0].ID].State != session.AttemptFailed || ledger.results[ledger.specs[0].ID].Usage.Output == nil {
			t.Fatal("retry erased first attempt usage")
		}
	})
}

func TestCompactionRejectsOneOversizedToolBatchBeforeDispatch(t *testing.T) {
	ledger := newCompactionLedger(nil)
	call := session.Message{ID: "calls", SessionID: "owner", GroupID: "old", TurnID: "old", Sequence: 1, Role: session.Assistant}
	for i := range 8 {
		call.Parts = append(call.Parts, session.Part{Type: "tool_call", Call: &session.ToolCall{ID: fmt.Sprintf("call%d", i), Name: "execute", Arguments: json.RawMessage(`{}`)}})
	}
	ledger.raw = append(ledger.raw, call)
	for i := range 8 {
		ledger.raw = append(ledger.raw, session.Message{ID: session.MessageID(fmt.Sprintf("result%d", i)), SessionID: "owner", GroupID: "old", TurnID: "old", Sequence: int64(i + 2), Role: session.Tool, Parts: []session.Part{{Type: "tool_result", Result: &session.ToolResult{CallID: fmt.Sprintf("call%d", i), Output: strings.Repeat("x", 600<<10)}}}})
	}
	for _, message := range compactionMessages(4, 1) {
		message.Sequence += 9
		message.ID = session.MessageID(fmt.Sprintf("tail%d", message.Sequence))
		ledger.raw = append(ledger.raw, message)
	}
	r, err := New(providerFunc(func(context.Context, model.Request) (model.Response, error) {
		t.Error("oversized tool batch dispatched")
		return model.Response{}, nil
	}), ledger, ledger, nil, nil, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "large", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{})
	if err != nil || outcome.State != session.Failed || outcome.Failure == nil || !strings.Contains(*outcome.Failure, "complete source tool batch") || len(ledger.specs) != 0 {
		t.Fatalf("outcome=%+v err=%v attempts=%+v", outcome, err, ledger.specs)
	}
}

type compactionExecutor struct {
	ledger *compactionLedger
	calls  int
}

func (*compactionExecutor) Instructions(_ context.Context, _ session.Turn, policy session.Instructions) (string, error) {
	return policy.Text + "\nexecute code", nil
}

func (e *compactionExecutor) Execute(_ context.Context, turn session.Turn, _ session.MessageID, call session.ToolCall) ([]session.Part, error) {
	e.calls++
	result := session.ToolResult{CallID: call.ID, Output: "settled result"}
	e.ledger.recordMessage(turn.ID, session.MessageID("result_"+call.ID), session.Tool, []session.Part{{Type: "tool_result", Result: &result}})
	return []session.Part{{Type: "tool_result", Result: &result}}, nil
}

func TestCompactionRebuildsAfterDurableToolBatchOrOutputCorrection(t *testing.T) {
	for _, correcting := range []bool{false, true} {
		ledger := newCompactionLedger(compactionMessages(100, 1))
		ledger.raw[len(ledger.raw)-1].TurnID = "current"
		ledger.raw[len(ledger.raw)-1].GroupID = "current"
		executor := &compactionExecutor{ledger: ledger}
		var purposes []string
		ordinary := 0
		provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
			purposes = append(purposes, request.Purpose)
			if len(request.Messages) > maxContextMessages {
				return model.Response{}, errors.New("late request exceeded cap")
			}
			if request.Purpose == "compaction" {
				return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
			}
			ordinary++
			if ordinary == 1 {
				if correcting {
					return model.Response{Parts: []session.Part{{Type: "text", Text: "42"}}}, nil
				}
				return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "cell", Name: "execute", Arguments: json.RawMessage(`{}`)}}}}, nil
			}
			last := request.Messages[len(request.Messages)-1]
			if correcting {
				if last.Role != session.System || !strings.Contains(last.Parts[0].Text, "Correct it once") {
					return model.Response{}, errors.New("rebuild lost correction instruction")
				}
			} else if last.Role != session.Tool || last.Parts[0].Result.CallID != "cell" || request.Messages[len(request.Messages)-2].Parts[0].Call.ID != "cell" {
				return model.Response{}, errors.New("rebuild lost a paired durable effect")
			}
			return model.Response{Parts: []session.Part{{Type: "text", Text: `"done"`}}}, nil
		})
		r, err := New(provider, ledger, ledger, nil, executor, nil, nil, ledger, nil)
		if err != nil {
			t.Fatal(err)
		}
		config := session.Configuration{}
		if correcting {
			config.OutputSchema = json.RawMessage(`{"type":"string"}`)
		}
		outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, config)
		if err != nil || outcome.State != session.Succeeded || !slices.Equal(purposes, []string{"", "compaction", ""}) {
			t.Fatalf("correcting=%v outcome=%+v err=%v purposes=%v", correcting, outcome, err, purposes)
		}
		wantEffects := 1
		if correcting {
			wantEffects = 0
		}
		if executor.calls != wantEffects {
			t.Fatal("compaction replayed execution", executor.calls)
		}
	}
}

func TestCompactionDropsOldPinsWhenMeasuringProjection(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(6, 2))
	ledger.raw[0].Parts[0].Text = strings.Repeat("important pin ", 1000)
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
	outcome, err := r.Run(t.Context(), session.Turn{ID: "growing", SessionID: "owner", Kind: session.CompactInput}, session.Configuration{})
	if err != nil || outcome.State != session.Succeeded || ledger.head.Revision != 2 || len(ledger.values) != 2 || len(ledger.values[*ledger.head.CompactionID].PinnedMessageIDs) != 0 {
		t.Fatalf("completed pin did not release its projection budget: outcome=%+v err=%v head=%+v", outcome, err, ledger.head)
	}
}

func TestCompactionFoldIdentityPersistsAcrossModelBoundaries(t *testing.T) {
	ledger := newCompactionLedger(compactionMessages(5, 20))
	for i := 80; i < len(ledger.raw); i++ {
		ledger.raw[i].TurnID = "current"
		ledger.raw[i].GroupID = "current"
	}
	executor := &compactionExecutor{ledger: ledger}
	ordinary := 0
	r, err := New(providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose == "compaction" {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "summary"}}}, nil
		}
		ordinary++
		if ordinary == 4 {
			return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
		}
		count := 1
		if ordinary == 2 {
			count = 16
		}
		var parts []session.Part
		for i := range count {
			parts = append(parts, session.Part{Type: "tool_call", Call: &session.ToolCall{ID: fmt.Sprintf("round%d_call%d", ordinary, i), Name: "execute", Arguments: json.RawMessage(`{}`)}})
		}
		return model.Response{Parts: parts}, nil
	}), ledger, ledger, nil, executor, nil, nil, ledger, nil)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := r.Run(t.Context(), session.Turn{ID: "current", SessionID: "owner"}, session.Configuration{})
	var helpers []string
	for _, spec := range ledger.specs {
		if spec.Request.Purpose == "compaction" {
			helpers = append(helpers, spec.LogicalID)
		}
	}
	if err != nil || outcome.State != session.Succeeded || ordinary != 4 || executor.calls != 18 || !slices.Equal(helpers, []string{"current_compact_1", "current_compact_2"}) {
		t.Fatalf("outcome=%+v err=%v ordinary=%d effects=%d helpers=%v", outcome, err, ordinary, executor.calls, helpers)
	}
}
