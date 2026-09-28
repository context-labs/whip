package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func executionGoal(t *testing.T, s *Store, owner session.SessionID, id session.GoalID, limit int64, start bool) GoalAdmission {
	t.Helper()
	goal, err := s.CreateGoal(t.Context(), owner, id, nil, session.GoalRequest{Text: "complete the exact objective", MaxContinuations: &limit}, start)
	if err != nil {
		t.Fatal(err)
	}
	return goal
}

func TestGoalTurnCaptureOrdinarySourcesAndCompactExclusion(t *testing.T) {
	for _, source := range []string{"user", "mail", "schedule", "compact"} {
		t.Run(source, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			goal := executionGoal(t, s, owner.ID, "goal", 0, false)
			switch source {
			case "user":
				submit(t, s, owner.ID, "human")
			case "mail":
				if _, err := s.SendMail(t.Context(), session.MailSpec{ID: "mail", SenderID: owner.ID, RecipientID: owner.ID, Delivery: session.MailQueued, Subject: "subject", Body: "body"}); err != nil {
					t.Fatal(err)
				}
			case "schedule":
				scheduled := makeSchedule(t, s, owner.ID, "scheduled", "@at 2020-01-01T00:00:00Z")
				if _, err := s.FireSchedule(t.Context(), scheduled.ID, *scheduled.NextDue); err != nil {
					t.Fatal(err)
				}
			case "compact":
				if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "compact"}, Submission{SessionID: owner.ID, Source: session.UserInput, Kind: session.CompactInput}); err != nil {
					t.Fatal(err)
				}
			}
			turn := claim(t, s, owner.ID).Turn
			captured, err := s.TurnGoal(t.Context(), turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if source == "compact" {
				if turn.Goal != nil || captured != nil {
					t.Fatal("compact inherited goal")
				}
				return
			}
			if turn.Goal == nil || *turn.Goal != goal.Goal.GoalRef || captured == nil || captured.Spec != goal.Goal.Spec {
				t.Fatalf("lost capture %+v %+v", turn, captured)
			}
			// Cancellation and eligibility edits cannot rewrite a running turn's context.
			if _, err := s.CancelGoal(t.Context(), owner.ID, goal.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)}); err != nil {
				t.Fatal(err)
			}
			again, err := s.TurnGoal(t.Context(), turn.ID)
			if err != nil || !reflect.DeepEqual(again, captured) {
				t.Fatal(again, err)
			}
			current, _ := s.Turn(t.Context(), turn.ID)
			if current.State != session.Running {
				t.Fatal("goal cancellation cancelled ordinary turn")
			}
			if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
				t.Fatal(err)
			}
			if queued := resourceState(t, s, owner.ID, session.ResourceQueuedInputs).Used; queued != 0 {
				t.Fatal("cancelled goal continued", queued)
			}
		})
	}
}

func TestGoalQueuedDisabledOrStaleCleanupCommits(t *testing.T) {
	for _, mode := range []string{"disabled", "stale"} {
		t.Run(mode, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			goal := executionGoal(t, s, owner.ID, "goal", 2, true)
			human := submit(t, s, owner.ID, "human")
			if mode == "disabled" {
				if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.db.ExecContext(t.Context(), "UPDATE goals SET revision=revision+1 WHERE id=?", goal.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Claim(t.Context(), owner.ID); !errors.Is(err, ErrNoWork) {
				t.Fatal(err)
			}
			input, _ := s.Input(t.Context(), goal.Initial.Input.ID)
			if input.State != session.InputCancelled || count(t, s, "turns") != 0 || count(t, s, "turn_permits") != 0 {
				t.Fatal("cleanup rolled back", input)
			}
			next := claim(t, s, owner.ID)
			if next.Input.ID != human.Input.ID {
				t.Fatal("head was not retired")
			}
			if mode == "disabled" {
				current, _ := s.Goal(t.Context(), owner.ID, goal.ID)
				if current.State != session.GoalPaused || *current.StopReason != "disabled" || next.Turn.Goal != nil {
					t.Fatal(current)
				}
			} else if next.Turn.Goal == nil || next.Turn.Goal.Revision != 2 {
				t.Fatal("ordinary work did not capture current revision")
			}
		})
	}
}

func TestGoalContinuationExactCountReopenAndFinishReplay(t *testing.T) {
	for _, limit := range []int64{0, 2} {
		t.Run(strconv.FormatInt(limit, 10), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			_, owner := create(t, s, nil)
			goal := executionGoal(t, s, owner.ID, "goal", limit, true)
			for i := int64(0); i <= limit; i++ {
				turn := claim(t, s, owner.ID).Turn
				if turn.Goal == nil {
					t.Fatal("goal input not captured")
				}
				if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
					t.Fatal(err)
				}
				current, _ := s.Goal(t.Context(), owner.ID, goal.ID)
				want := min(i+1, limit)
				if current.ContinuationsUsed != want {
					t.Fatalf("usage %d want %d", current.ContinuationsUsed, want)
				}
				beforeInputs, beforeWrites := count(t, s, "inputs"), count(t, s, "logical_writes")
				if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
					t.Fatal(err)
				}
				replay, _ := s.Goal(t.Context(), owner.ID, goal.ID)
				if !reflect.DeepEqual(replay, current) || count(t, s, "inputs") != beforeInputs || count(t, s, "logical_writes") != beforeWrites {
					t.Fatal("Finish replay continued twice")
				}
				s = openTest(t, path)
			}
			final, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			if final.State != session.GoalPaused || *final.StopReason != "round_limit" || count(t, s, "inputs") != int(limit+1) {
				t.Fatal(final)
			}
			if _, err := s.Claim(t.Context(), owner.ID); !errors.Is(err, ErrNoWork) {
				t.Fatal(err)
			}
			if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "exhausted"}, owner.ID, final.GoalRef); !errors.Is(err, ErrLimit) {
				t.Fatal(err)
			}
		})
	}
}

func TestGoalFinishSQLFailureRollsBackAndSemanticPressurePauses(t *testing.T) {
	for _, fault := range []string{"BEFORE UPDATE ON turns", "BEFORE UPDATE ON goals", "BEFORE INSERT ON inputs", "BEFORE INSERT ON receipts", "BEFORE INSERT ON logical_writes"} {
		t.Run(fault, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			goal := executionGoal(t, s, owner.ID, "goal", 2, true)
			turn := claim(t, s, owner.ID).Turn
			execTest(t, s, "CREATE TRIGGER fail "+fault+" BEGIN SELECT RAISE(ABORT,'fault'); END")
			if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err == nil {
				t.Fatal("fault ignored")
			}
			current, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			active, _ := s.Turn(t.Context(), turn.ID)
			if !reflect.DeepEqual(current, *goal.Goal) || active.State != session.Running || count(t, s, "inputs") != 1 || count(t, s, "receipts") != 1 || count(t, s, "logical_writes") != 2 || count(t, s, "turn_permits") != 1 {
				t.Fatal("partial Finish")
			}
			execTest(t, s, "DROP TRIGGER fail")
			if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
				t.Fatal(err)
			}
			current, _ = s.Goal(t.Context(), owner.ID, goal.ID)
			if current.ContinuationsUsed != 1 || count(t, s, "inputs") != 2 {
				t.Fatal("retry missing continuation")
			}
		})
	}
	for _, limit := range []string{"queue", "writes", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			goal := executionGoal(t, s, owner.ID, "goal", 2, true)
			turn := claim(t, s, owner.ID).Turn
			switch limit {
			case "queue":
				resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 0)
			case "writes":
				budgetLimit(t, s, owner.ID, session.BudgetLogicalWrites, 2)
			case "bytes":
				budgetLimit(t, s, owner.ID, session.BudgetLogicalWriteBytes, budgetState(t, s, owner.ID, session.BudgetLogicalWriteBytes).Used)
			}
			result, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil)
			if err != nil || result.State != session.Succeeded {
				t.Fatal("pressure undid successful turn", result, err)
			}
			current, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			if current.State != session.GoalPaused || *current.StopReason != "admission_limit" || current.ContinuationsUsed != 0 || count(t, s, "inputs") != 1 || count(t, s, "receipts") != 1 || count(t, s, "logical_writes") != 2 || count(t, s, "turn_permits") != 0 {
				t.Fatal("partial proposed continuation", current)
			}
		})
	}
}

func TestGoalFailureRecoveryAndResumeNeverReplay(t *testing.T) {
	for _, state := range []session.TurnState{session.Failed, session.Cancelled, session.Interrupted} {
		t.Run(string(state), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			_, owner := create(t, s, nil)
			goal := executionGoal(t, s, owner.ID, "goal", 2, true)
			initial := claim(t, s, owner.ID).Turn
			if _, err := s.Finish(t.Context(), initial.ID, session.Succeeded, nil, nil); err != nil {
				t.Fatal(err)
			}
			currentTurn := claim(t, s, owner.ID).Turn
			if state == session.Interrupted {
				s = openTest(t, path)
				if n, err := s.Recover(t.Context()); err != nil || n != 1 {
					t.Fatal(n, err)
				}
			} else {
				if _, err := s.Finish(t.Context(), currentTurn.ID, state, nil, nil); err != nil {
					t.Fatal(err)
				}
			}
			current, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			if current.State != session.GoalPaused || current.ContinuationsUsed != 1 || *current.StopReason != "turn_"+string(state) || count(t, s, "inputs") != 2 {
				t.Fatal(current)
			}
			if n, err := s.Recover(t.Context()); err != nil || n != 0 {
				t.Fatal(n, err)
			}
			if _, err := s.Claim(t.Context(), owner.ID); !errors.Is(err, ErrNoWork) {
				t.Fatal("failed work requeued", err)
			}
			resume, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "c", RequestID: "resume"}, owner.ID, current.GoalRef)
			if err != nil {
				t.Fatal(err)
			}
			resumed := claim(t, s, owner.ID)
			if resumed.Input.ID != resume.Input.ID {
				t.Fatal("resume did not use ordinary input")
			}
			updated, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			if updated.ContinuationsUsed != 1 {
				t.Fatal("resume reset allowance")
			}
		})
	}
}

func completionCell(t *testing.T, s *Store, turn session.Turn, key string) session.Cell {
	t.Helper()
	message, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: session.MessageID("message_" + key), Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: key, Name: "execute", Arguments: json.RawMessage(`{"code":"complete"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := s.BeginCell(t.Context(), session.CellSpec{ID: session.CellID(key), TurnID: turn.ID, CallMessageID: message.ID, CallID: key})
	if err != nil || !dispatch {
		t.Fatal(err)
	}
	return cell
}

func completionOperation(t *testing.T, s *Store, owner session.Session, turn session.Turn, cell session.Cell, id string) session.Operation {
	t.Helper()
	raw, err := json.Marshal(session.GoalCompletion{GoalID: turn.Goal.ID, ExpectedRevision: turn.Goal.Revision, Evidence: "the objective is verified"})
	if err != nil {
		t.Fatal(err)
	}
	return admitOperation(t, s, session.OperationSpec{ID: session.OperationID(id), CellID: cell.ID, RequestID: id, Capability: "goals.complete", Resource: string(owner.TreeID), Arguments: raw})
}

func settleCompletionCell(t *testing.T, s *Store, cell session.Cell) {
	t.Helper()
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: cell.CallID, Output: "intent recorded"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestGoalCompletionIntentAtomicAuthorityAndFinalOutcome(t *testing.T) {
	for _, outcome := range []string{"success", "failure", "cancel", "disabled", "revoked-before", "revoked-after", "fault"} {
		t.Run(outcome, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			goal := executionGoal(t, s, owner.ID, "goal", 2, true)
			turn := claim(t, s, owner.ID).Turn
			cell := completionCell(t, s, turn, "cell")
			grant := controlGrant(t, s, owner, "goal-grant", "goals.complete", string(owner.TreeID))
			op := completionOperation(t, s, owner, turn, cell, "complete")
			if outcome == "revoked-before" {
				if _, err := s.RevokeGrant(t.Context(), grant.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.ApplyGoalCompletion(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
					t.Fatal(err)
				}
				return
			}
			if outcome == "fault" {
				execTest(t, s, "CREATE TRIGGER fail BEFORE UPDATE ON operations WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'fault'); END")
				if _, err := s.ApplyGoalCompletion(t.Context(), op.ID); err == nil {
					t.Fatal("fault ignored")
				}
				saved, _ := s.Operation(t.Context(), op.ID)
				if saved.State != session.OperationReady || saved.DispatchedAt != nil {
					t.Fatal("partial completion dispatch")
				}
				execTest(t, s, "DROP TRIGGER fail")
			}
			first, err := s.ApplyGoalCompletion(t.Context(), op.ID)
			if err != nil {
				t.Fatal(err)
			}
			unchanged, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			if !reflect.DeepEqual(unchanged, *goal.Goal) {
				t.Fatal("completion changed goal before Finish")
			}
			if outcome == "revoked-after" {
				if _, err := s.RevokeGrant(t.Context(), grant.ID); err != nil {
					t.Fatal(err)
				}
			}
			settleCompletionCell(t, s, cell)
			state := session.Succeeded
			var failure *string
			if outcome == "failure" {
				state = session.Failed
				failure = new("output_invalid: invalid final")
			}
			if outcome == "cancel" {
				if _, err := s.CancelGoal(t.Context(), owner.ID, goal.ID); err != nil {
					t.Fatal(err)
				}
				state = session.Cancelled
			}
			if outcome == "disabled" {
				if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Finish(t.Context(), turn.ID, state, failure, nil); err != nil {
				t.Fatal(err)
			}
			current, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			if outcome == "failure" || outcome == "disabled" {
				if current.State != session.GoalPaused || current.CompletionOperationID != nil {
					t.Fatal(current)
				}
			} else if outcome == "cancel" {
				if current.State != session.GoalCancelled {
					t.Fatal(current)
				}
			} else if current.State != session.GoalCompleted || current.CompletionTurnID == nil || *current.CompletionTurnID != turn.ID || current.CompletionOperationID == nil || *current.CompletionOperationID != op.ID {
				t.Fatal(current)
			}
			replay, err := s.ApplyGoalCompletion(t.Context(), op.ID)
			if err != nil || !reflect.DeepEqual(first, replay) {
				t.Fatal("lost-ack intent retry failed", err)
			}
			if count(t, s, "inputs") != 1 {
				t.Fatal("completed/failed intent continued")
			}
			if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
				t.Fatal("audit foreign keys blocked deletion", err)
			}
		})
	}
}

func TestGoalCompletionOneUseDelegationAndExactBinding(t *testing.T) {
	for _, mode := range []string{"one-use", "child", "child-revoked", "child-denied", "wrong-goal", "wrong-revision", "wrong-resource"} {
		t.Run(mode, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			if mode == "child" || mode == "child-revoked" || mode == "child-denied" {
				child := controlChild(t, s, owner.ID, "child")
				if _, err := s.CancelInput(t.Context(), child.Admission.Input.ID); err != nil {
					t.Fatal(err)
				}
				owner = *child.Session
			}
			executionGoal(t, s, owner.ID, "goal", 2, true)
			turn := claim(t, s, owner.ID).Turn
			cell := completionCell(t, s, turn, "cell")
			var grant session.Grant
			if mode != "one-use" && mode != "child-denied" {
				grant = controlGrant(t, s, owner, "grant", "goals.complete", string(owner.TreeID))
			}
			args := session.GoalCompletion{GoalID: turn.Goal.ID, ExpectedRevision: turn.Goal.Revision, Evidence: "verified"}
			resource := string(owner.TreeID)
			if mode == "wrong-goal" {
				args.GoalID = "other"
			}
			if mode == "wrong-revision" {
				args.ExpectedRevision++
			}
			if mode == "wrong-resource" {
				resource = "other-tree"
				controlGrant(t, s, owner, "other-grant", "goals.complete", resource)
			}
			raw, _ := json.Marshal(args)
			op := admitOperation(t, s, session.OperationSpec{ID: "complete", CellID: cell.ID, RequestID: "complete", Capability: "goals.complete", Resource: resource, Arguments: raw})
			if mode == "one-use" {
				if op.State != session.OperationWaiting {
					t.Fatal(op)
				}
				if _, err := s.ApplyGoalCompletion(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
					t.Fatal("unapproved completed", err)
				}
				if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "child-revoked" {
				if _, err := s.RevokeGrant(t.Context(), *grant.IssuerID); err != nil {
					t.Fatal(err)
				}
			}
			_, err := s.ApplyGoalCompletion(t.Context(), op.ID)
			valid := mode == "one-use" || mode == "child"
			if !valid {
				if !errors.Is(err, ErrConflict) {
					t.Fatal(mode, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "one-use" {
				other := completionOperation(t, s, owner, turn, cell, "other")
				if other.State != session.OperationWaiting {
					t.Fatal("one-use permission reused")
				}
				if _, err := s.ResolvePermission(t.Context(), other.ID, false); err != nil {
					t.Fatal(err)
				}
			}
			settleCompletionCell(t, s, cell)
			if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
				t.Fatal(err)
			}
			current, _ := s.CurrentGoal(t.Context(), owner.ID)
			if current.State != session.GoalCompleted {
				t.Fatal(current)
			}
		})
	}
}

func TestGoalStopDisabledAndOutstandingInputSuppressContinuation(t *testing.T) {
	for _, mode := range []string{"stopped", "disabled", "outstanding"} {
		t.Run(mode, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			// Human input wins queue order; the goal may already have an unclaimed input.
			submit(t, s, owner.ID, "human")
			goal := executionGoal(t, s, owner.ID, "goal", 2, mode == "outstanding")
			turn := claim(t, s, owner.ID).Turn
			if mode == "stopped" {
				if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "disabled" {
				if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)}); err != nil {
					t.Fatal(err)
				}
			}
			state := session.Succeeded
			if mode == "stopped" {
				state = session.Cancelled
			}
			if _, err := s.Finish(t.Context(), turn.ID, state, nil, nil); err != nil {
				t.Fatal(err)
			}
			current, _ := s.Goal(t.Context(), owner.ID, goal.ID)
			if current.ContinuationsUsed != 0 {
				t.Fatal("unexpected continuation")
			}
			if mode == "outstanding" {
				if current.GoalRef != goal.Goal.GoalRef || count(t, s, "inputs") != 2 {
					t.Fatal("existing input duplicated or rebound")
				}
				next := claim(t, s, owner.ID)
				if next.Input.ID != goal.Initial.Input.ID || *next.Turn.Goal != goal.Goal.GoalRef {
					t.Fatal(next)
				}
			} else if current.State != session.GoalPaused {
				t.Fatal(current)
			}
		})
	}
}

func TestGoalUncertainExecutionCannotContinueEvenWithSuccessfulFinish(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	goal := executionGoal(t, s, owner.ID, "goal", 2, true)
	turn := claim(t, s, owner.ID).Turn
	cell := completionCell(t, s, turn, "uncertain-cell")
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellUncertain, session.ToolResult{CallID: cell.CallID, Output: "transport lost", IsError: true}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	current, _ := s.Goal(t.Context(), owner.ID, goal.ID)
	if current.State != session.GoalPaused || *current.StopReason != "uncertain_execution" || current.ContinuationsUsed != 0 || count(t, s, "inputs") != 1 {
		t.Fatal(current)
	}
}

func TestGoalOrdinaryInitialTurnConsumesZeroAllowanceStart(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	goal := executionGoal(t, s, owner.ID, "goal", 0, false)
	submit(t, s, owner.ID, "human")
	turn := claim(t, s, owner.ID).Turn
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	paused, _ := s.Goal(t.Context(), owner.ID, goal.ID)
	if _, err := s.ResumeGoal(t.Context(), session.RequestIdentity{ClientID: "client", RequestID: "resume"}, owner.ID, paused.GoalRef); !errors.Is(err, ErrLimit) {
		t.Fatal("accepted captured turn was treated as never started", err)
	}
}

func TestGoalStoredOutputMismatchCannotApplyCompletion(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	owner, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Output: &session.OutputPolicy{Schema: json.RawMessage(`{"type":"integer"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	executionGoal(t, s, owner.ID, "goal", 2, true)
	turn := claim(t, s, owner.ID).Turn
	cell := completionCell(t, s, turn, "complete-cell")
	controlGrant(t, s, owner, "grant", "goals.complete", string(owner.TreeID))
	op := completionOperation(t, s, owner, turn, cell, "complete")
	if _, err := s.ApplyGoalCompletion(t.Context(), op.ID); err != nil {
		t.Fatal(err)
	}
	settleCompletionCell(t, s, cell)
	if _, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: "invalid-final", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: `"not an integer"`}}}); err != nil {
		t.Fatal(err)
	}
	// Defensive store boundary: incorrect success reporting still cannot activate
	// an intent or charge a continuation under an invalid captured output contract.
	result, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, nil)
	if err != nil || result.State != session.Succeeded {
		t.Fatal(result, err)
	}
	goal, _ := s.CurrentGoal(t.Context(), owner.ID)
	if goal.State != session.GoalPaused || *goal.StopReason != "output_invalid" || goal.CompletionOperationID != nil || count(t, s, "inputs") != 1 {
		t.Fatal(goal)
	}
}
