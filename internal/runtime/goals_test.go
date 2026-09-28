package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

//nolint:nilnil // Ordinary non-goal turns have no captured goal context.
func capturedTestGoal(request model.Request) (*session.GoalContext, error) {
	_, value, ok := strings.Cut(request.Instructions, "Captured goal for this turn (JSON data):\n")
	if !ok {
		return nil, nil
	}
	raw, _, _ := strings.Cut(value, "\n")
	var goal session.GoalContext
	if err := json.Unmarshal([]byte(raw), &goal); err != nil {
		return nil, err
	}
	return &goal, nil
}

func awaitGoal(t *testing.T, r *Runtime, owner session.SessionID, id session.GoalID, state session.GoalState) session.Goal {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := r.Goal(ctx, owner, id)
		if err != nil {
			t.Fatal(err)
		}
		if value.State == state {
			return value
		}
		select {
		case <-ctx.Done():
			t.Fatalf("goal=%+v runtime=%v", value, r.Err())
		case <-ticker.C:
		}
	}
}

func awaitGoalPermission(t *testing.T, r *Runtime, owner session.SessionID) session.Operation {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		values, err := r.Permissions(ctx, owner, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if value.State == session.PermissionPending {
				operation, err := r.Operation(ctx, value.OperationID)
				if err != nil {
					t.Fatal(err)
				}
				if operation.Capability != "goals.complete" || operation.State != session.OperationWaiting || operation.DispatchedAt != nil {
					t.Fatalf("unexpected permission: %+v", operation)
				}
				return operation
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("permission missing runtime=%v", r.Err())
		case <-ticker.C:
		}
	}
}

func TestBothEnginesGoalCompletionAuthorityAndContinuation(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			accepted := make(chan model.Request, 1)
			release := make(chan struct{})
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				goal, err := capturedTestGoal(request)
				if err != nil {
					return model.Response{}, err
				}
				if goal == nil || (goal.Spec.Text == "continue then complete" && goal.Revision == 1) {
					return model.Response{Parts: []session.Part{{Type: "text", Text: "progress"}}}, nil
				}
				last := request.Messages[len(request.Messages)-1]
				if last.Role == session.Tool {
					select {
					case accepted <- request:
					case <-ctx.Done():
						return model.Response{}, ctx.Err()
					}
					select {
					case <-release:
					case <-ctx.Done():
						return model.Response{}, ctx.Err()
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "finished"}}}, nil
				}
				code := fmt.Sprintf("print(goals.complete(goal_id=%q,expected_revision=%q,evidence=\"checked result\"))", goal.ID, strconv.FormatInt(goal.Revision, 10))
				if engine == session.QuickJS {
					code = fmt.Sprintf("print(await goals.complete({goal_id:%q,expected_revision:%q,evidence:'checked result'}));", goal.ID, strconv.FormatInt(goal.Revision, 10))
				}
				return childControlCode(1, code), nil
			})
			r := openEngineTest(t, t.TempDir(), provider)
			root := createEngineSession(t, r, engine)
			admitted, err := r.CreateGoal(t.Context(), root.ID, "root_goal", nil, session.GoalRequest{Text: "continue then complete", MaxContinuations: new(int64(2))}, true)
			if err != nil {
				t.Fatal(err)
			}
			op := awaitGoalPermission(t, r, root.ID)
			if _, err := r.ResolvePermission(t.Context(), op.ID, true); err != nil {
				t.Fatal(err)
			}
			var request model.Request
			select {
			case request = <-accepted:
			case <-time.After(30 * time.Second):
				t.Fatal("completion did not reach final model step")
			}
			current, err := r.CurrentGoal(t.Context(), root.ID)
			if err != nil || current == nil || current.State != session.GoalArmed || current.ContinuationsUsed != 1 {
				t.Fatalf("intent completed goal early: %+v %v", current, err)
			}
			operation, err := r.Operation(t.Context(), op.ID)
			if err != nil || operation.State != session.OperationSucceeded || !strings.Contains(string(operation.Result.Value), `"accepted":true`) {
				t.Fatalf("intent=%+v err=%v", operation, err)
			}
			release <- struct{}{}
			completed := awaitGoal(t, r, root.ID, "root_goal", session.GoalCompleted)
			if completed.CompletionTurnID == nil || *completed.CompletionTurnID != request.TurnID || completed.CompletionOperationID == nil || *completed.CompletionOperationID != op.ID {
				t.Fatalf("completion audit=%+v", completed)
			}
			replay, err := r.CreateGoal(t.Context(), root.ID, "root_goal", nil, session.GoalRequest{Text: "continue then complete", MaxContinuations: new(int64(2))}, true)
			if err != nil || replay.Initial.Input.ID != admitted.Initial.Input.ID || replay.Goal.Revision != completed.Revision {
				t.Fatalf("create replay=%+v err=%v", replay, err)
			}

			// A fresh operation cannot reuse the earlier one-use approval. Its
			// accepted intent still loses to the captured final-output contract.
			root = configureOutput(t, r, root, `{"type":"integer"}`)
			invalid, err := r.CreateGoal(t.Context(), root.ID, "invalid_output_goal", &completed.GoalRef, session.GoalRequest{Text: "complete with invalid final output", MaxContinuations: new(int64(0))}, true)
			if err != nil {
				t.Fatal(err)
			}
			nextPermission := awaitGoalPermission(t, r, root.ID)
			if nextPermission.GrantID != nil || nextPermission.ID == op.ID {
				t.Fatalf("reused one-use authority: %+v", nextPermission)
			}
			if _, err := r.ResolvePermission(t.Context(), nextPermission.ID, true); err != nil {
				t.Fatal(err)
			}
			select {
			case <-accepted:
			case <-time.After(30 * time.Second):
				t.Fatal("accepted intent did not reach invalid final")
			}
			release <- struct{}{}
			invalidGoal := awaitGoal(t, r, root.ID, invalid.ID, session.GoalPaused)
			invalidTurn := awaitGoalReceipt(t, r, invalid.Initial.Receipt.RequestIdentity)
			if invalidGoal.CompletionOperationID != nil || invalidTurn.Turn.State != session.Failed || invalidTurn.Turn.Failure == nil || !strings.HasPrefix(*invalidTurn.Turn.Failure, "output_invalid") {
				t.Fatalf("invalid output applied completion: %+v %+v", invalidGoal, invalidTurn.Turn)
			}
			invalidOps, err := r.Operations(t.Context(), invalidTurn.Turn.ID, "", 100)
			if err != nil || len(invalidOps) != 1 || invalidOps[0].State != session.OperationSucceeded {
				t.Fatalf("lost accepted intent evidence: %+v %v", invalidOps, err)
			}
			root = configureOutput(t, r, root, "")

			grant, err := r.CreateGrant(t.Context(), session.Grant{ID: "goal_parent_grant", SessionID: root.ID, Capability: "goals.complete", Resource: string(root.TreeID)})
			if err != nil {
				t.Fatal(err)
			}
			child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "initialize"}}, GrantIDs: []session.GrantID{grant.ID}})
			if err != nil {
				t.Fatal(err)
			}
			waitTestWithin(t, r, "child", terminal, 30*time.Second)
			childGoal, err := r.CreateGoal(t.Context(), child.Session.ID, "child_goal", nil, session.GoalRequest{Text: "complete child", MaxContinuations: new(int64(0))}, true)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-accepted:
			case <-time.After(30 * time.Second):
				t.Fatal("delegated child completion stalled")
			}
			release <- struct{}{}
			childDone := awaitGoal(t, r, child.Session.ID, childGoal.ID, session.GoalCompleted)
			if childDone.ContinuationsUsed != 0 {
				t.Fatalf("initial run charged a continuation: %+v", childDone)
			}
			operations, err := r.Operations(t.Context(), *childDone.CompletionTurnID, "", 100)
			if err != nil || len(operations) != 1 || operations[0].GrantID == nil {
				t.Fatalf("child operations=%+v %v", operations, err)
			}
			grants, err := r.Grants(t.Context(), child.Session.ID, "", 100)
			if err != nil || len(grants) != 1 || grants[0].IssuerID == nil || *grants[0].IssuerID != grant.ID || *operations[0].GrantID != grants[0].ID {
				t.Fatalf("child authority=%+v %v", grants, err)
			}
			if _, err := r.RevokeGrant(t.Context(), grant.ID); err != nil {
				t.Fatal(err)
			}
			denied, err := r.CreateGoal(t.Context(), child.Session.ID, "revoked_goal", &childDone.GoalRef, session.GoalRequest{Text: "revoked completion", MaxContinuations: new(int64(0))}, true)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-accepted:
			case <-time.After(30 * time.Second):
				t.Fatal("denial not returned to model")
			}
			release <- struct{}{}
			paused := awaitGoal(t, r, child.Session.ID, denied.ID, session.GoalPaused)
			deniedTurn := awaitGoalReceipt(t, r, denied.Initial.Receipt.RequestIdentity)
			deniedOps, err := r.Operations(t.Context(), deniedTurn.Turn.ID, "", 100)
			if err != nil || len(deniedOps) != 1 || deniedOps[0].State != session.OperationDenied || deniedOps[0].DispatchedAt != nil {
				t.Fatalf("revoked delegation escalated: %+v %v", deniedOps, err)
			}
			if paused.CompletionOperationID != nil || paused.StopReason == nil || *paused.StopReason != "round_limit" {
				t.Fatalf("denied intent completed goal: %+v", paused)
			}
		})
	}
}

func TestGoalCancelTargetsOnlyGoalOwnedTurn(t *testing.T) {
	for _, human := range []bool{false, true} {
		t.Run(fmt.Sprintf("human=%v", human), func(t *testing.T) {
			provider := &observationProvider{started: make(chan observationCall)}
			r := openTest(t, t.TempDir(), provider)
			owner := createTest(t, r)
			created, err := r.CreateGoal(t.Context(), owner.ID, "cancel_goal", nil, session.GoalRequest{Text: "objective", MaxContinuations: new(int64(0))}, !human)
			if err != nil {
				t.Fatal(err)
			}
			var identity session.RequestIdentity
			if human {
				identity = submitTest(t, r, owner.ID, "human").Receipt.RequestIdentity
			} else {
				identity = created.Initial.Receipt.RequestIdentity
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			running := nextObservationCall(t, provider)
			change, err := r.CancelGoal(t.Context(), owner.ID, created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if human {
				if change.CancelTurnID != nil || running.ctx.Err() != nil {
					t.Fatalf("cancelled captured human turn: %+v %v", change, running.ctx.Err())
				}
				running.complete <- observationResult{response: model.Response{Parts: []session.Part{{Type: "text", Text: "human answer"}}}}
			} else {
				if change.CancelTurnID == nil {
					t.Fatal("missing exact goal turn")
				}
				select {
				case <-running.ctx.Done():
				case <-time.After(5 * time.Second):
					t.Fatal("goal worker not cancelled")
				}
			}
			finish := awaitGoalReceipt(t, r, identity)
			want := session.Cancelled
			if human {
				want = session.Succeeded
			}
			if finish.Turn.State != want || finish.Turn.Goal == nil {
				t.Fatalf("outcome=%+v", finish.Turn)
			}
			replay, err := r.CancelGoal(t.Context(), owner.ID, created.ID)
			if err != nil || replay.CancelTurnID != nil || replay.Goal.State != session.GoalCancelled {
				t.Fatalf("cancel replay=%+v %v", replay, err)
			}
		})
	}
}

func awaitGoalReceipt(t *testing.T, r *Runtime, identity session.RequestIdentity) store.Admission {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := r.Admission(ctx, identity)
		if err != nil {
			t.Fatal(err)
		}
		if terminal(value) || value.Input.State == session.InputCancelled {
			return value
		}
		select {
		case <-ctx.Done():
			t.Fatalf("receipt=%+v runtime=%v", value, r.Err())
		case <-ticker.C:
		}
	}
}

func TestGoalFailureOutputAndDisabledQueuePause(t *testing.T) {
	for _, mode := range []string{"provider_failure", "output_invalid", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			provider := providerFunc(func(context.Context, model.Request) (model.Response, error) {
				calls++
				if mode == "provider_failure" {
					return model.Response{}, errors.New("provider failed")
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "not JSON"}}}, nil
			})
			r := openTest(t, t.TempDir(), provider)
			owner := createTest(t, r)
			if mode == "output_invalid" {
				owner = configureOutput(t, r, owner, `{"type":"integer"}`)
			}
			value, err := r.CreateGoal(t.Context(), owner.ID, "failure_goal", nil, session.GoalRequest{Text: "objective"}, true)
			if err != nil {
				t.Fatal(err)
			}
			if value.Goal.Spec.MaxContinuations != 100 {
				t.Fatal("default allowance changed")
			}
			if mode == "disabled" {
				if _, err := r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			final := awaitGoalReceipt(t, r, value.Initial.Receipt.RequestIdentity)
			paused := awaitGoal(t, r, owner.ID, value.ID, session.GoalPaused)
			if paused.ContinuationsUsed != 0 || paused.StopReason == nil {
				t.Fatalf("failure replayed: %+v", paused)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			if mode == "disabled" {
				if calls != 0 || final.Turn != nil || final.Input.State != session.InputCancelled || *paused.StopReason != "disabled" {
					t.Fatalf("disabled input ran: %+v %+v calls=%d", final, paused, calls)
				}
			} else if final.Turn.State != session.Failed {
				t.Fatalf("failure hidden: %+v", final.Turn)
			}
			if mode == "output_invalid" && calls != 2 {
				t.Fatalf("correction count=%d", calls)
			}
		})
	}
}
