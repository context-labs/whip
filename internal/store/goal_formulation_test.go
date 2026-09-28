package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func admitFormulationTest(t *testing.T, s *Store, owner session.SessionID, key string, request session.GoalFormulationRequest) Admission {
	t.Helper()
	result, err := s.AdmitGoalFormulation(t.Context(), session.RequestIdentity{ClientID: "formulate", RequestID: key}, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func formulationAttemptTest(t *testing.T, s *Store, claimed Claim, key string) session.ModelAttempt {
	t.Helper()
	request := attemptRequest(claimed.Turn.ID, key)
	request.Request.Purpose = session.GoalFormulationPurpose
	request.Request.Model = claimed.Configuration.Model
	value := reserveTest(t, s, request)
	dispatchTest(t, s, value.ID)
	return value
}

func settleFormulationTest(t *testing.T, s *Store, attempt session.ModelAttemptID) session.GoalFormulationSettlement {
	t.Helper()
	value, err := s.SettleGoalFormulation(t.Context(), attempt, compactionOutcomeTest(), &session.GoalFormulationDraft{Text: "Complete the recorded task."})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestGoalFormulationAdmissionFreezesHistoryAndClaimCapturesMainModel(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	request := session.GoalFormulationRequest{GoalID: "goal", Start: true}
	identity := session.RequestIdentity{ClientID: "formulate", RequestID: "first"}
	if _, err := s.AdmitGoalFormulation(t.Context(), identity, owner.ID, request); !errors.Is(err, session.ErrInvalid) || count(t, s, "inputs") != 0 {
		t.Fatal("empty history admitted", err)
	}
	for i := range 3 {
		compactionHistoryTest(t, s, owner.ID, fmt.Sprintf("history_%d", i))
	}
	submit(t, s, owner.ID, "inflight")
	active := claim(t, s, owner.ID).Turn
	first := admitFormulationTest(t, s, owner.ID, "first", request)
	if _, err := s.AppendMessage(t.Context(), active.ID, outputDraft("after-admission", "later history")); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, active.ID, session.Succeeded)
	owner, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)})
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.AdmitGoalFormulation(t.Context(), identity, owner.ID, request)
	if err != nil || !reflect.DeepEqual(first, retry) {
		t.Fatal("receipt rechecked mutable state", retry, err)
	}
	if _, err := s.AdmitGoalFormulation(t.Context(), identity, owner.ID, session.GoalFormulationRequest{GoalID: "different"}); !errors.Is(err, ErrConflict) {
		t.Fatal("changed admission retry accepted", err)
	}
	model := session.ModelSelection{Provider: "captured", Name: "main"}
	owner, err = s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(true), Model: &model})
	if err != nil {
		t.Fatal(err)
	}
	claimed := claim(t, s, owner.ID)
	input, err := s.GoalFormulationInput(t.Context(), claimed.Turn.ID)
	if err != nil || input.AfterSequence != 5 || input.ThroughSequence != 13 || input.Request.TailMessages != 8 || !reflect.DeepEqual(claimed.Configuration.Model, model) || claimed.Turn.Goal != nil || claimed.Turn.Kind != session.GoalFormulationInputKind {
		t.Fatalf("wrong source/config capture: %+v %+v %v", input, claimed, err)
	}
	if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "later", Name: "other"}}); err != nil {
		t.Fatal(err)
	}
	invalid := attemptRequest(claimed.Turn.ID, "wrong-purpose")
	if _, err := s.ReserveModelAttempt(t.Context(), invalid); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("ordinary model request admitted for maintenance", err)
	}
	invalid.Request.Purpose = session.GoalFormulationPurpose
	if _, err := s.ReserveModelAttempt(t.Context(), invalid); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("uncaptured main model admitted", err)
	}
	attempt := formulationAttemptTest(t, s, claimed, "formulation")
	settled := settleFormulationTest(t, s, attempt.ID)
	if !settled.Accepted || settled.Candidate == nil || settled.Candidate.ThroughSequence != 13 || count(t, s, "messages") != 14 {
		t.Fatal(settled)
	}
	if _, err := s.AppendMessage(t.Context(), claimed.Turn.ID, outputDraft("forbidden", "helper")); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("formulation authored transcript", err)
	}
	finishMailTest(t, s, claimed.Turn.ID, session.Succeeded)
	if output, err := s.TurnOutput(t.Context(), claimed.Turn.ID); err != nil || output != nil {
		t.Fatal("formulation has ordinary output", output, err)
	}
}

func TestGoalFormulationSettlementAtomicRecoveryAndReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "raw")
	old := createGoalTest(t, s, owner.ID, "old", nil, false)
	request := session.GoalFormulationRequest{GoalID: "formulated", Expected: &old.Goal.GoalRef, Start: true, MaxContinuations: new(int64(0))}
	admission := admitFormulationTest(t, s, owner.ID, "first", request)
	for _, delivery := range []session.MailDelivery{session.MailQueued, session.MailSteer, session.MailNextTurn} {
		sendMailTest(t, s, mailSpec(owner.ID, string(delivery), delivery))
	}
	claimed := claim(t, s, owner.ID)
	if claimed.Turn.Goal != nil || claimed.Input.ID != admission.Input.ID {
		t.Fatal("maintenance captured conversation goal", claimed)
	}
	attempt := formulationAttemptTest(t, s, claimed, "formulation")
	writes, messages := count(t, s, "logical_writes"), count(t, s, "messages")
	for _, fault := range []string{"BEFORE UPDATE ON model_attempts", "BEFORE INSERT ON goals", "BEFORE INSERT ON inputs", "BEFORE INSERT ON logical_writes", "BEFORE INSERT ON goal_formulations"} {
		execTest(t, s, "CREATE TRIGGER formulation_failure "+fault+" BEGIN SELECT RAISE(ABORT,'injected SQL failure'); END")
		if _, err := s.SettleGoalFormulation(t.Context(), attempt.ID, compactionOutcomeTest(), &session.GoalFormulationDraft{Text: "Complete the recorded task."}); err == nil {
			t.Fatal("SQL failure was treated as semantic rejection", fault)
		}
		pending, err := s.ModelAttempt(t.Context(), attempt.ID)
		selected, goalErr := s.CurrentGoal(t.Context(), owner.ID)
		if err != nil || goalErr != nil || pending.State != session.AttemptDispatched || selected.ID != old.ID || selected.State != session.GoalArmed || count(t, s, "goal_formulations") != 0 || count(t, s, "logical_writes") != writes {
			t.Fatalf("SQL error partially settled: %s %+v %+v %v %v", fault, pending, selected, err, goalErr)
		}
		execTest(t, s, "DROP TRIGGER formulation_failure")
	}
	settled := settleFormulationTest(t, s, attempt.ID)
	if !settled.Accepted || settled.Rejection != nil || settled.Attempt.MessageID != nil || settled.Candidate == nil || count(t, s, "logical_writes") != writes+2 || count(t, s, "messages") != messages || count(t, s, "cells") != 0 || count(t, s, "turn_mail_observations") != 0 {
		t.Fatalf("wrong accepted helper settlement: %+v", settled)
	}
	mustFail(t, s, "UPDATE goal_formulations SET text='changed' WHERE attempt_id=?", attempt.ID)
	mustFail(t, s, "UPDATE goals SET origin_formulation_attempt_id=NULL,revision=revision+1 WHERE id=?", request.GoalID)
	if _, err := s.ObserveSteers(t.Context(), claimed.Turn.ID); err != nil || count(t, s, "turn_mail_observations") != 0 {
		t.Fatal("formulation observed mail", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if changed, err := s.Recover(t.Context()); err != nil || changed != 1 {
		t.Fatal(changed, err)
	}
	goal, err := s.CurrentGoal(t.Context(), owner.ID)
	if err != nil || goal.ID != request.GoalID || goal.State != session.GoalArmed || goal.ContinuationsUsed != 0 || goal.OriginFormulationAttemptID == nil || *goal.OriginFormulationAttemptID != attempt.ID {
		t.Fatal("recovery changed accepted goal", goal, err)
	}
	initial, err := s.Admission(t.Context(), goalInitialIdentity(goal.ID))
	if err != nil || initial.Input.State != session.Queued {
		t.Fatal("recovery lost accepted input", initial, err)
	}
	if retried := settleFormulationTest(t, s, attempt.ID); !reflect.DeepEqual(settled, retried) {
		t.Fatal("settlement replay changed accepted evidence", retried)
	}
	replacement := createGoalTest(t, s, owner.ID, "replacement", &goal.GoalRef, false)
	if retried := settleFormulationTest(t, s, attempt.ID); !retried.Accepted {
		t.Fatal("replacement hid prior acceptance", retried)
	}
	current, err := s.CurrentGoal(t.Context(), owner.ID)
	if err != nil || current.ID != replacement.ID {
		t.Fatal("retry reselected formulation", current, err)
	}
	oldInput, err := s.Input(t.Context(), initial.Input.ID)
	if err != nil || oldInput.State != session.InputCancelled || count(t, s, "goal_formulations") != 1 {
		t.Fatal(oldInput, err)
	}
	for _, id := range []session.MailID{"queued", "steer", "next_turn"} {
		mailStateTest(t, s, owner.ID, id, session.MailPending, 1)
	}
}

func TestGoalFormulationSemanticRejectionRetainsBillingWithoutLaterActivation(t *testing.T) {
	for _, reason := range []string{"queue", "writes", "bytes", "stale", "cancelled", "disabled", "identity"} {
		t.Run(reason, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			compactionHistoryTest(t, s, owner.ID, "raw")
			old := createGoalTest(t, s, owner.ID, "old", nil, false)
			request := session.GoalFormulationRequest{GoalID: "new", Expected: &old.Goal.GoalRef, Start: true}
			if reason == "identity" {
				request.GoalID = old.ID
			}
			admission := admitFormulationTest(t, s, owner.ID, "first", request)
			claimed := claim(t, s, owner.ID)
			attempt := formulationAttemptTest(t, s, claimed, "formulation")
			writes := count(t, s, "logical_writes")
			switch reason {
			case "queue":
				resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 0)
			case "writes":
				budgetLimit(t, s, owner.ID, session.BudgetLogicalWrites, int64(writes+1))
			case "bytes":
				budgetLimit(t, s, owner.ID, session.BudgetLogicalWriteBytes, budgetState(t, s, owner.ID, session.BudgetLogicalWriteBytes).Used+1)
			case "stale":
				if _, err := s.CancelGoal(t.Context(), owner.ID, old.ID); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				if _, err := s.CancelInput(t.Context(), admission.Input.ID); err != nil {
					t.Fatal(err)
				}
			case "disabled":
				var err error
				owner, err = s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)})
				if err != nil {
					t.Fatal(err)
				}
			}
			settled := settleFormulationTest(t, s, attempt.ID)
			if settled.Accepted || settled.Candidate == nil || settled.Rejection == nil || settled.Attempt.CostNanoUSD == nil || *settled.Attempt.CostNanoUSD != 1200 || count(t, s, "goals") != 1 || count(t, s, "logical_writes") != writes {
				t.Fatalf("semantic rejection lost evidence or partially activated: %+v", settled)
			}
			goal, err := s.CurrentGoal(t.Context(), owner.ID)
			if err != nil || goal.ID != old.ID || reason != "stale" && goal.State != session.GoalArmed {
				t.Fatal("rejected replacement changed current goal", goal, err)
			}
			resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 10)
			budgetLimit(t, s, owner.ID, session.BudgetLogicalWrites, 100)
			budgetLimit(t, s, owner.ID, session.BudgetLogicalWriteBytes, 1<<20)
			if reason == "disabled" {
				if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(true)}); err != nil {
					t.Fatal(err)
				}
			}
			if retried := settleFormulationTest(t, s, attempt.ID); !reflect.DeepEqual(settled, retried) || retried.Accepted || count(t, s, "goals") != 1 {
				t.Fatal("settlement retry retried activation", retried)
			}
			changed := session.GoalFormulationDraft{Text: "different candidate"}
			if _, err := s.SettleGoalFormulation(t.Context(), attempt.ID, compactionOutcomeTest(), &changed); !errors.Is(err, ErrConflict) {
				t.Fatal("candidate changed on retry", err)
			}
			if reason != "cancelled" {
				request := attemptRequest(claimed.Turn.ID, "repair")
				request.Request.Purpose, request.Request.Model = session.GoalFormulationPurpose, claimed.Configuration.Model
				if _, err := s.ReserveModelAttempt(t.Context(), request); !errors.Is(err, ErrConflict) {
					t.Fatal("settled candidate allowed another formulation attempt", err)
				}
			}
			state := session.Failed
			if reason == "cancelled" {
				state = session.Cancelled
			}
			finishMailTest(t, s, claimed.Turn.ID, state)
			if current, err := s.CurrentGoal(t.Context(), owner.ID); err != nil || !reflect.DeepEqual(current, goal) {
				t.Fatal("formulation outcome altered old goal", current, err)
			}
		})
	}
}

func TestGoalFormulationRecoveryNeverInventsCandidateOrReplaysModel(t *testing.T) {
	for _, dispatched := range []bool{false, true} {
		t.Run(strconv.FormatBool(dispatched), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			_, owner := create(t, s, nil)
			compactionHistoryTest(t, s, owner.ID, "raw")
			admitFormulationTest(t, s, owner.ID, "first", session.GoalFormulationRequest{GoalID: "new", Start: true})
			claimed := claim(t, s, owner.ID)
			request := attemptRequest(claimed.Turn.ID, "formulation")
			request.Request.Purpose, request.Request.Model = session.GoalFormulationPurpose, claimed.Configuration.Model
			attempt := reserveTest(t, s, request)
			if dispatched {
				dispatchTest(t, s, attempt.ID)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, path)
			if _, err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			got, err := s.ModelAttempt(t.Context(), attempt.ID)
			if err != nil {
				t.Fatal(err)
			}
			if dispatched {
				if got.State != session.AttemptUncertain || got.CostNanoUSD != nil {
					t.Fatal("invented dispatched evidence", got)
				}
			} else if got.State != session.AttemptCancelled || got.CostNanoUSD == nil || *got.CostNanoUSD != 0 {
				t.Fatal("reserved attempt not known-zero", got)
			}
			if _, err := s.SettleGoalFormulation(t.Context(), got.ID, *got.Result, &session.GoalFormulationDraft{Text: "too late"}); err != nil || count(t, s, "goal_formulations") != 0 || count(t, s, "goals") != 0 {
				t.Fatal("late recovery result attached candidate", err)
			}
			if allowed, err := s.DispatchModelAttempt(t.Context(), got.ID); err != nil || allowed {
				t.Fatal("recovered request dispatched twice", allowed, err)
			}
		})
	}
}

func TestGoalFormulationChildDeletionRetainsCandidateAndOrigin(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	initial := claim(t, s, child.Session.ID).Turn
	if _, err := s.Finish(t.Context(), initial.ID, session.Failed, new("prior failure"), []session.MessageDraft{outputDraft("initial-answer", "raw context")}); err != nil {
		t.Fatal(err)
	}
	before := pendingCompletionTest(t, s, root.ID, child.Session.ID, initial.ID)
	request := session.GoalFormulationRequest{GoalID: "formulated", Start: false}
	admission := admitFormulationTest(t, s, child.Session.ID, "child", request)
	claimed := claim(t, s, child.Session.ID)
	attempt := formulationAttemptTest(t, s, claimed, "child-attempt")
	settled := settleFormulationTest(t, s, attempt.ID)
	if !settled.Accepted || resourceState(t, s, child.Session.ID, session.ResourceQueuedInputs).Used != 0 {
		t.Fatal("start=false admitted input", settled)
	}
	finishMailTest(t, s, claimed.Turn.ID, session.Succeeded)
	if after := pendingCompletionTest(t, s, root.ID, child.Session.ID, initial.ID); !reflect.DeepEqual(before, after) {
		t.Fatal("maintenance replaced child report", after)
	}
	budgets, err := s.Budgets(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	if after, err := s.Budgets(t.Context(), root.ID); err != nil || !reflect.DeepEqual(budgets, after) {
		t.Fatal("deletion erased ancestor billing", after, err)
	}
	candidate, err := s.GoalFormulation(t.Context(), child.Session.ID, attempt.ID)
	if err != nil || !reflect.DeepEqual(&candidate, settled.Candidate) {
		t.Fatal("deletion erased candidate evidence", candidate, err)
	}
	if retry := settleFormulationTest(t, s, attempt.ID); !reflect.DeepEqual(settled, retry) || !retry.Accepted {
		t.Fatal("tombstone lost original acceptance", retry)
	}
	var origin session.ModelAttemptID
	var deleted int64
	if err := s.db.QueryRowContext(t.Context(), "SELECT origin_formulation_attempt_id,deleted_at FROM goals WHERE id=?", request.GoalID).Scan(&origin, &deleted); err != nil || origin != attempt.ID || deleted == 0 {
		t.Fatal("goal tombstone lost origin", origin, deleted, err)
	}
	retried, err := s.AdmitGoalFormulation(t.Context(), admission.Receipt.RequestIdentity, child.Session.ID, request)
	if err != nil || retried.Receipt.DeletedAt == nil || retried.Input != nil {
		t.Fatal("admission retry ignored tombstone", retried, err)
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil || count(t, s, "goal_formulations") != 0 || count(t, s, "model_attempts") != 0 {
		t.Fatal("root deletion leaked accounting", err)
	}
}

func TestGoalFormulationCannotPublishAssistantOrPrivateContinuation(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "raw")
	if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Output: &session.OutputPolicy{Schema: json.RawMessage(`{"type":"integer"}`)}}); err != nil {
		t.Fatal(err)
	}
	admitFormulationTest(t, s, owner.ID, "first", session.GoalFormulationRequest{GoalID: "new"})
	claimed := claim(t, s, owner.ID)
	attempt := formulationAttemptTest(t, s, claimed, "formulation")
	draft := outputDraft("invalid-output", "helper text")
	draft.Continuation = &session.ModelContinuation{Scope: strings.Repeat("a", 64), Data: `[{"type":"reasoning","encrypted_content":"private"}]`}
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, compactionOutcomeTest(), &draft); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("helper published assistant output", err)
	}
	if _, _, err := s.BeginCell(t.Context(), session.CellSpec{ID: "invalid-cell", TurnID: claimed.Turn.ID, CallMessageID: "raw_answer_0", CallID: "call"}); err == nil {
		t.Fatal("maintenance executed a cell")
	}
	if _, err := s.SettleGoalFormulation(t.Context(), attempt.ID, compactionOutcomeTest(), &session.GoalFormulationDraft{Text: " "}); err != nil {
		t.Fatal(err)
	}
	if late := settleFormulationTest(t, s, attempt.ID); late.Candidate != nil || late.Accepted || late.Rejection == nil {
		t.Fatal("late valid candidate attached to settled invalid output", late)
	}
	finishMailTest(t, s, claimed.Turn.ID, session.Succeeded)
	if output, err := s.TurnOutput(t.Context(), claimed.Turn.ID); err != nil || output != nil {
		t.Fatal(output, err)
	}
}

func TestGoalFormulationRecordedProviderRetryAndPostAcceptanceCancellation(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "raw")
	admission := admitFormulationTest(t, s, owner.ID, "first", session.GoalFormulationRequest{GoalID: "new", Start: true})
	claimed := claim(t, s, owner.ID)
	attempt := formulationAttemptTest(t, s, claimed, "first-attempt")
	failed := session.ModelAttemptResult{State: session.AttemptFailed, ReportedCostNanoUSD: new(int64(5)), Failure: new("confirmed retryable rejection")}
	if result, err := s.SettleGoalFormulation(t.Context(), attempt.ID, failed, nil); err != nil || result.Candidate != nil || result.Accepted {
		t.Fatal(result, err)
	}
	request := attemptRequest(claimed.Turn.ID, "second-attempt")
	request.LogicalID, request.Number = attempt.LogicalID, 2
	request.Request.Purpose, request.Request.Model = session.GoalFormulationPurpose, claimed.Configuration.Model
	retry := reserveTest(t, s, request)
	dispatchTest(t, s, retry.ID)
	if result := settleFormulationTest(t, s, retry.ID); !result.Accepted || count(t, s, "model_attempts") != 2 || budgetState(t, s, owner.ID, session.BudgetModelCostNanoUSD).Used != 1205 {
		t.Fatal("provider retry lost accounting", result)
	}
	if _, err := s.CancelInput(t.Context(), admission.Input.ID); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, claimed.Turn.ID, session.Cancelled)
	goal, err := s.CurrentGoal(t.Context(), owner.ID)
	if err != nil || goal.ID != "new" || goal.State != session.GoalArmed || goal.ContinuationsUsed != 0 {
		t.Fatal("helper cancellation paused accepted goal", goal, err)
	}
	initial, err := s.Admission(t.Context(), goalInitialIdentity(goal.ID))
	if err != nil || initial.Input.State != session.Queued {
		t.Fatal("helper cancellation cancelled accepted input", initial, err)
	}
}

func TestGoalFormulationAdmissionCapacityAndSourceBounds(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "raw")
	resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 0)
	inputs, receipts := count(t, s, "inputs"), count(t, s, "receipts")
	request := session.GoalFormulationRequest{GoalID: "new", TailMessages: 2}
	if _, err := s.AdmitGoalFormulation(t.Context(), session.RequestIdentity{ClientID: "formulate", RequestID: "limited"}, owner.ID, request); !errors.Is(err, ErrLimit) || count(t, s, "inputs") != inputs || count(t, s, "receipts") != receipts || count(t, s, "goal_formulation_inputs") != 0 {
		t.Fatal("limited admission left partial payload", err)
	}
	resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 1)
	admitFormulationTest(t, s, owner.ID, "minimum", request)
	claimed := claim(t, s, owner.ID)
	window, err := s.GoalFormulationInput(t.Context(), claimed.Turn.ID)
	if err != nil || window.AfterSequence != 2 || window.ThroughSequence != 4 {
		t.Fatal("explicit tail not exact", window, err)
	}
	finishMailTest(t, s, claimed.Turn.ID, session.Failed)
	// A bounded message count must also reject oversized encoded source data.
	submit(t, s, owner.ID, "large")
	turn := claim(t, s, owner.ID).Turn
	for i := range 5 {
		if _, err := s.AppendMessage(t.Context(), turn.ID, outputDraft(session.MessageID(fmt.Sprintf("large_%d", i)), strings.Repeat("x", (1<<20)-512))); err != nil {
			t.Fatal(err)
		}
	}
	finishMailTest(t, s, turn.ID, session.Succeeded)
	if _, err := s.AdmitGoalFormulation(t.Context(), session.RequestIdentity{ClientID: "formulate", RequestID: "oversized"}, owner.ID, session.GoalFormulationRequest{GoalID: "oversized"}); !errors.Is(err, ErrLimit) {
		t.Fatal("oversized captured window accepted", err)
	}
}

func TestGoalFormulationReplacementCommitsOrRestoresOldQueuedInput(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(strconv.FormatBool(limited), func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			compactionHistoryTest(t, s, owner.ID, "raw")
			// Queue maintenance before the old goal's initial input so it owns the
			// next turn while the replacement must retire that unclaimed input.
			admitFormulationTest(t, s, owner.ID, "first", session.GoalFormulationRequest{GoalID: "new", Expected: &session.GoalRef{ID: "old", Revision: 1}, Start: true})
			old := createGoalTest(t, s, owner.ID, "old", nil, true)
			claimed := claim(t, s, owner.ID)
			resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 1)
			if limited {
				budgetLimit(t, s, owner.ID, session.BudgetLogicalWrites, 3)
			}
			attempt := formulationAttemptTest(t, s, claimed, "formulation")
			settled := settleFormulationTest(t, s, attempt.ID)
			oldInput, err := s.Input(t.Context(), old.Initial.Input.ID)
			if err != nil || settled.Accepted == limited || resourceState(t, s, owner.ID, session.ResourceQueuedInputs).Used != 1 {
				t.Fatal(settled, oldInput, err)
			}
			if limited {
				if oldInput.State != session.Queued || count(t, s, "goals") != 1 || count(t, s, "logical_writes") != 2 {
					t.Fatal("semantic rollback lost the previous input", oldInput)
				}
			} else if oldInput.State != session.InputCancelled || count(t, s, "goals") != 2 {
				t.Fatal("replacement did not retire old queue entry", oldInput)
			}
		})
	}
}
