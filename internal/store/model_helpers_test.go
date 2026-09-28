package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func helperOperation(t *testing.T, s *Store, owner session.Session, cell session.Cell, id, capability, arguments string, dispatch bool) session.Operation {
	t.Helper()
	op := admitOperation(t, s, session.OperationSpec{
		ID: session.OperationID(id), CellID: cell.ID, RequestID: id,
		Capability: capability, Resource: string(owner.TreeID), Arguments: json.RawMessage(arguments),
	})
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if dispatch {
		if allowed, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !allowed {
			t.Fatalf("dispatch helper operation: %v %v", allowed, err)
		}
	}
	return op
}

func helperRequest(t *testing.T, operation session.Operation, index int) session.ModelAttemptSpec {
	t.Helper()
	logical, err := session.ModelHelperLogicalID(operation.ID, index)
	if err != nil {
		t.Fatal(err)
	}
	request := attemptRequest(operation.TurnID, logical+"_try_1")
	request.LogicalID = logical
	request.Request.Purpose = session.ModelHelperPurpose
	request.OperationID, request.BatchIndex = &operation.ID, &index
	return request
}

func TestModelHelperAdmissionValidatesOperationAndArguments(t *testing.T) {
	for _, test := range []struct {
		name, capability, arguments     string
		index                           int
		undispatched, wrongScope, valid bool
	}{
		{name: "call", arguments: `{"prompt":"question"}`, valid: true},
		{name: "batch", capability: "models.batch", arguments: `{"prompts":["one","two"]}`, index: 1, valid: true},
		{name: "stricter route", arguments: `{"prompt":"question","max_tokens":5000}`, valid: true},
		{name: "exact cap", arguments: `{"prompt":"question","max_tokens":4096}`, valid: true},
		{name: "undispatched", arguments: `{"prompt":"question"}`, undispatched: true},
		{name: "other scope", arguments: `{"prompt":"question"}`, wrongScope: true},
		{name: "other capability", capability: "state.get", arguments: `{"prompt":"question"}`},
		{name: "call index", arguments: `{"prompt":"question"}`, index: 1},
		{name: "batch index", capability: "models.batch", arguments: `{"prompts":["one"]}`, index: 1},
		{name: "empty batch", capability: "models.batch", arguments: `{"prompts":[]}`},
		{name: "large batch", capability: "models.batch", arguments: `{"prompts":[` + strings.Repeat(`"one",`, 32) + `"last"]}`},
		{name: "maximum batch", capability: "models.batch", arguments: `{"prompts":[` + strings.Repeat(`"one",`, 31) + `"last"]}`, index: 31, valid: true},
		{name: "batch member", capability: "models.batch", arguments: `{"prompts":["one",false]}`},
		{name: "empty prompt", arguments: `{"prompt":" "}`},
		{name: "missing prompt", arguments: `{}`},
		{name: "other shape", arguments: `{"prompts":["one"]}`},
		{name: "extra option", arguments: `{"prompt":"one","model":"other"}`},
		{name: "cap exceeded", arguments: `{"prompt":"one","max_tokens":4095}`},
		{name: "zero cap", arguments: `{"prompt":"one","max_tokens":0}`},
		{name: "negative cap", arguments: `{"prompt":"one","max_tokens":-1}`},
		{name: "fractional cap", arguments: `{"prompt":"one","max_tokens":1.5}`},
		{name: "null cap", arguments: `{"prompt":"one","max_tokens":null}`},
		{name: "excessive cap", arguments: `{"prompt":"one","max_tokens":1000001}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			if test.wrongScope {
				owner.TreeID = "another-tree"
			}
			capability := test.capability
			if capability == "" {
				capability = "models.call"
			}
			op := helperOperation(t, s, owner, cell, "op", capability, test.arguments, !test.undispatched)
			request := helperRequest(t, op, test.index)
			_, err := s.ReserveModelAttempt(t.Context(), request)
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
				dispatchTest(t, s, request.ID)
			} else if err == nil || count(t, s, "model_attempts") != 0 || count(t, s, "attempt_budget_ancestors") != 0 {
				t.Fatalf("invalid request changed accounting: %v", err)
			}
		})
	}
}

func TestModelHelperProvenanceIsOwnedImmutableAndRetryable(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := helperOperation(t, s, owner, cell, "op", "models.batch", `{"prompts":["one","two"]}`, true)
	request := helperRequest(t, op, 0)
	_, foreignCell := operationCell(t, s)
	foreign := request
	foreign.TurnID = foreignCell.TurnID
	if _, err := s.ReserveModelAttempt(t.Context(), foreign); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign turn accepted: %v", err)
	}
	for _, mutate := range []func(*session.ModelAttemptSpec){
		func(p *session.ModelAttemptSpec) { p.OperationID = nil },
		func(p *session.ModelAttemptSpec) { p.BatchIndex = nil },
		func(p *session.ModelAttemptSpec) { p.OperationID, p.BatchIndex = nil, nil },
		func(p *session.ModelAttemptSpec) { p.Request.Purpose = "turn" },
		func(p *session.ModelAttemptSpec) { p.BatchIndex = new(-1) },
		func(p *session.ModelAttemptSpec) { p.LogicalID = "ordinary_model_1" },
	} {
		invalid := request
		mutate(&invalid)
		if _, err := s.ReserveModelAttempt(t.Context(), invalid); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid provenance accepted: %+v %v", invalid, err)
		}
	}
	first := reserveTest(t, s, request)
	if first.OperationID == nil || *first.OperationID != op.ID || first.BatchIndex == nil || *first.BatchIndex != 0 {
		t.Fatalf("provenance missing: %+v", first)
	}
	changed := helperRequest(t, op, 1)
	changed.ID = request.ID
	if _, err := s.ReserveModelAttempt(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed provenance retry: %v", err)
	}
	mustFail(t, s, "UPDATE model_attempts SET batch_index=1,state='dispatched',dispatched_at=? WHERE id=?", now(), first.ID)
	mustFail(t, s, "UPDATE model_attempts SET operation_id='another-operation',state='dispatched',dispatched_at=? WHERE id=?", now(), first.ID)
	dispatchTest(t, s, first.ID)
	if _, err := s.SettleModelAttempt(t.Context(), first.ID, session.ModelAttemptResult{State: session.AttemptSucceeded}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleOperation(t.Context(), op.ID, session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`[]`)}); err != nil {
		t.Fatal(err)
	}
	replayed := reserveTest(t, s, request)
	if replayed.State != session.AttemptSucceeded || !reflect.DeepEqual(replayed.OperationID, first.OperationID) {
		t.Fatalf("retry lost committed provenance: %+v", replayed)
	}
	request.Number++
	request.ID += "_again"
	if _, err := s.ReserveModelAttempt(t.Context(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("terminal operation admitted more work: %v", err)
	}
}

func TestModelHelperRejectsPendingAndDeniedOperations(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := admitOperation(t, s, session.OperationSpec{
		ID: "pending-helper", CellID: cell.ID, RequestID: "helper", Capability: "models.call",
		Resource: string(owner.TreeID), Arguments: json.RawMessage(`{"prompt":"question"}`),
	})
	request := helperRequest(t, op, 0)
	if _, err := s.ReserveModelAttempt(t.Context(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("unapproved operation admitted model work: %v", err)
	}
	if _, err := s.ResolvePermission(t.Context(), op.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveModelAttempt(t.Context(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("denied operation admitted model work: %v", err)
	}
	if count(t, s, "model_attempts") != 0 || count(t, s, "attempt_budget_ancestors") != 0 {
		t.Fatal("rejected operations changed accounting")
	}
}

func TestModelHelperSettlementJoinsAttemptsBeforeOperationAndCell(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := helperOperation(t, s, owner, cell, "op", "models.batch", `{"prompts":["one","two"]}`, true)
	reserved := reserveTest(t, s, helperRequest(t, op, 0))
	dispatched := reserveTest(t, s, helperRequest(t, op, 1))
	dispatchTest(t, s, dispatched.ID)
	result := session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`[{"error":"cancelled"},{"output":"kept"}]`)}
	for _, state := range []session.OperationState{session.OperationSucceeded, session.OperationFailed, session.OperationUncertain} {
		pending := result
		pending.State = state
		if _, err := s.SettleOperation(t.Context(), op.ID, pending); !errors.Is(err, ErrBusy) {
			t.Fatalf("operation settled before attempts: %s %v", state, err)
		}
	}
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: "call", Output: "done"}, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("cell settled before helper: %v", err)
	}
	if _, err := s.SettleModelAttempt(t.Context(), reserved.ID, session.ModelAttemptResult{State: session.AttemptCancelled}, nil); err != nil {
		t.Fatal(err)
	}
	outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(6))}
	draft := session.MessageDraft{ID: "helper-answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "kept"}}}
	if _, err := s.SettleModelAttempt(t.Context(), dispatched.ID, outcome, &draft); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("helper appended transcript: %v", err)
	}
	if _, err := s.SettleModelAttempt(t.Context(), dispatched.ID, outcome, nil); err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `CREATE TRIGGER fail_helper_result BEFORE UPDATE ON operations WHEN NEW.finished_at IS NOT NULL BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := s.SettleOperation(t.Context(), op.ID, result); err == nil {
		t.Fatal("operation fault ignored")
	}
	if got := budgetState(t, s, owner.ID, session.BudgetModelCalls); got.Used != 1 || count(t, s, "messages") != 2 {
		t.Fatalf("operation failure lost charge or published helper transcript: %+v", got)
	}
	execTest(t, s, "DROP TRIGGER fail_helper_result")
	for range 2 {
		if _, err := s.SettleOperation(t.Context(), op.ID, result); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: "call", Output: "done"}, nil); err != nil {
		t.Fatal(err)
	}
	if allowed, err := s.DispatchModelAttempt(t.Context(), dispatched.ID); err != nil || allowed {
		t.Fatalf("settled helper was replayed: %v %v", allowed, err)
	}
}

func TestModelHelperRecoverySettlesAttemptsBeforeDependencies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	owner, cell := operationCell(t, s)
	op := helperOperation(t, s, owner, cell, "op", "models.batch", `{"prompts":["one","two"]}`, true)
	reserved := reserveTest(t, s, helperRequest(t, op, 0))
	dispatched := reserveTest(t, s, helperRequest(t, op, 1))
	dispatchTest(t, s, dispatched.ID)
	execTest(t, s, `CREATE TRIGGER fail_helper_recovery BEFORE UPDATE ON operations BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := other.Recover(t.Context()); err == nil {
		t.Fatal("recovery failure ignored")
	}
	before, err := s.ModelAttempt(t.Context(), reserved.ID)
	if err != nil || before.State != session.AttemptReserved {
		t.Fatalf("recovery escaped rollback: %+v %v", before, err)
	}
	execTest(t, s, "DROP TRIGGER fail_helper_recovery")
	if _, err := other.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []session.ModelAttemptID{reserved.ID, dispatched.ID} {
		got, err := s.ModelAttempt(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if id == reserved.ID {
			if got.State != session.AttemptCancelled || got.CostNanoUSD == nil || *got.CostNanoUSD != 0 || got.CostSource != "not_dispatched" {
				t.Fatalf("reserved outcome: %+v", got)
			}
		} else if got.State != session.AttemptUncertain || got.CostNanoUSD != nil || got.Result.Usage.Input != nil {
			t.Fatalf("dispatched outcome: %+v", got)
		}
		if allowed, err := s.DispatchModelAttempt(t.Context(), id); err != nil || allowed {
			t.Fatalf("recovery permitted replay: %v %v", allowed, err)
		}
	}
	operation, err := s.Operation(t.Context(), op.ID)
	if err != nil || operation.State != session.OperationUncertain || operation.Result.Value != nil {
		t.Fatalf("operation recovery invented output: %+v %v", operation, err)
	}
	latest, err := s.LatestCell(t.Context(), owner.ID)
	if err != nil || latest.State != session.CellUncertain || latest.Checkpoint != nil {
		t.Fatalf("cell recovery: %+v %v", latest, err)
	}
	if changed, err := other.Recover(t.Context()); err != nil || changed != 0 {
		t.Fatalf("recovery retry: %d %v", changed, err)
	}
}

func TestModelHelperChildDeletionRetainsProvenanceAndAncestorCharges(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	budgetLimit(t, s, root.ID, session.BudgetModelCalls, 1)
	if _, err := s.CreateGrant(t.Context(), session.Grant{
		ID: "models-grant", SessionID: root.ID, Capability: "models.call", Resource: string(root.TreeID),
	}); err != nil {
		t.Fatal(err)
	}
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	cell := childOperationCell(t, s, child.Session.ID)
	op := admitOperation(t, s, session.OperationSpec{
		ID: "child-helper", CellID: cell.ID, RequestID: "helper", Capability: "models.call",
		Resource: string(root.TreeID), Arguments: json.RawMessage(`{"prompt":"question"}`),
	})
	if allowed, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !allowed {
		t.Fatalf("dispatch child helper: %v %v", allowed, err)
	}
	attempt := reserveTest(t, s, helperRequest(t, op, 0))
	dispatchTest(t, s, attempt.ID)
	outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(6))}
	settled, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleOperation(t.Context(), op.ID, session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`"answer"`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: "call", Output: "done"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), cell.TurnID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	before, err := s.Budgets(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Operation(t.Context(), op.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("child operation survived deletion: %v", err)
	}
	retained, err := s.ModelAttempt(t.Context(), attempt.ID)
	if err != nil || !reflect.DeepEqual(retained, settled) || retained.OperationID == nil || *retained.OperationID != op.ID {
		t.Fatalf("deleted child lost immutable helper evidence: %+v %v", retained, err)
	}
	after, err := s.Budgets(t.Context(), root.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("deleted child replenished accounting: before=%+v after=%+v %v", before, after, err)
	}
	sibling := spawnChildTest(t, s, "sibling", childRequest(root.ID))
	turn := claim(t, s, sibling.Session.ID).Turn
	if _, err := s.ReserveModelAttempt(t.Context(), attemptRequest(turn.ID, "blocked")); !errors.Is(err, ErrLimit) {
		t.Fatalf("sibling reused deleted helper allowance: %v", err)
	}
	if allowed, err := s.DispatchModelAttempt(t.Context(), attempt.ID); err != nil || allowed {
		t.Fatalf("deleted helper dispatched again: %v %v", allowed, err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "model_attempts") != 0 || count(t, s, "attempt_budget_ancestors") != 0 {
		t.Fatal("root deletion leaked retained helper accounting")
	}
}

func TestAttemptDispatchRechecksAncestorExposureAcrossConnections(t *testing.T) {
	for _, test := range []struct {
		name       string
		limit      int64
		outcome    session.ModelAttemptResult
		wantDenied bool
	}{
		{name: "own reservation counted once", limit: 10, outcome: session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(5))}},
		{name: "sibling overage", limit: 10, outcome: session.ModelAttemptResult{State: session.AttemptSucceeded, ReportedCostNanoUSD: new(int64(6))}, wantDenied: true},
		{name: "unknown beyond bound", limit: 100, outcome: session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{CachedInput: new(int64(1))}}, wantDenied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s, other := openTest(t, path), openTest(t, path)
			_, root := create(t, s, nil)
			budgetLimit(t, s, root.ID, session.BudgetModelCostNanoUSD, test.limit)
			left := spawnChildTest(t, s, "left", childRequest(root.ID))
			right := spawnChildTest(t, s, "right", childRequest(root.ID))
			attempts := make([]session.ModelAttempt, 2)
			for i, child := range []session.SessionID{left.Session.ID, right.Session.ID} {
				turn := claim(t, s, child).Turn
				request := attemptRequest(turn.ID, fmt.Sprintf("request_%d", i))
				rate := new(int64(1_000_000))
				request.Request.Prices = session.ModelPrices{Input: rate, Output: rate, CachedInput: rate, Reasoning: rate, CachedOutput: rate}
				request.Request.InputTokenBound = new(int64(0))
				request.Request.MaxOutputTokens = 5
				attempts[i] = reserveTest(t, s, request)
			}
			dispatchTest(t, s, attempts[0].ID)
			draft := session.MessageDraft{ID: "retained-answer", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "already dispatched output"}}}
			if _, err := other.SettleModelAttempt(t.Context(), attempts[0].ID, test.outcome, &draft); err != nil {
				t.Fatal(err)
			}
			allowed, err := s.DispatchModelAttempt(t.Context(), attempts[1].ID)
			if test.wantDenied {
				if allowed || !errors.Is(err, ErrLimit) {
					t.Fatalf("invalidated reservation dispatched: %v %v", allowed, err)
				}
				if _, err := other.SettleModelAttempt(t.Context(), attempts[1].ID, session.ModelAttemptResult{State: session.AttemptCancelled}, nil); err != nil {
					t.Fatal(err)
				}
			} else if err != nil || !allowed {
				t.Fatalf("own reservation double counted: %v %v", allowed, err)
			}
			settled, err := s.ModelAttempt(t.Context(), attempts[0].ID)
			if err != nil || settled.MessageID == nil || *settled.MessageID != draft.ID || !reflect.DeepEqual(settled.Result, &test.outcome) {
				t.Fatalf("dispatch denial erased completed output/evidence: %+v %v", settled, err)
			}
		})
	}
}
