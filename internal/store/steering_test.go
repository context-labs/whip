package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func steerTest(t *testing.T, s *Store, owner session.SessionID, turn session.TurnID, key string) Admission {
	t.Helper()
	value, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "steering", RequestID: key}, Submission{SessionID: owner, Source: session.UserInput, Delivery: session.DeliverySteer, TargetTurnID: &turn, Parts: []session.Part{{Type: "text", Text: key}}})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestInputSteeringKeepsOriginalEvidenceOpeningInputAndFork(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	opening := submit(t, s, owner.ID, "opening")
	turn := claim(t, s, owner.ID).Turn
	request := designSubmission(t, s, owner.ID)
	request.Delivery = session.DeliverySteer
	request.TargetTurnID = &turn.ID
	identity := session.RequestIdentity{ClientID: "human", RequestID: "design_steer"}
	accepted, err := s.Admit(t.Context(), identity, request)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Input.Steering == nil || accepted.Input.Steering.Consumed || accepted.Input.TurnID != nil {
		t.Fatal("acceptance fabricated consumption", accepted)
	}
	activity, err := s.Activity(t.Context(), owner.ID)
	if err != nil || activity.QueuedInputCount != 1 || *activity.ActiveInputID != opening.Input.ID {
		t.Fatal(activity, err)
	}
	messages, err := s.ObserveSteers(t.Context(), turn.ID)
	if err != nil || len(messages) != 1 {
		t.Fatal(messages, err)
	}
	message := messages[0]
	if message.InputID == nil || *message.InputID != accepted.Input.ID || message.OpeningInput || message.TurnID != turn.ID || message.GroupID != session.HistoryGroupID(turn.ID) || !reflect.DeepEqual(message.Parts, request.Parts) || message.DesignContext == nil || !reflect.DeepEqual(message.DesignContext.DesignContext, *request.DesignContext) {
		t.Fatal("steering changed original evidence", message)
	}
	again, err := s.ObserveSteers(t.Context(), turn.ID)
	if err != nil || len(again) != 0 {
		t.Fatal("duplicate consumption", again, err)
	}
	receipt, err := s.Admit(t.Context(), identity, request)
	if err != nil || receipt.Turn == nil || receipt.Turn.ID != turn.ID || !receipt.Input.Steering.Consumed {
		t.Fatal(receipt, err)
	}
	original, err := s.TurnInput(t.Context(), turn.ID)
	if err != nil || original.ID != opening.Input.ID {
		t.Fatal("steering replaced opening input", original, err)
	}
	activity, err = s.Activity(t.Context(), owner.ID)
	if err != nil || activity.QueuedInputCount != 0 || *activity.ActiveInputID != opening.Input.ID {
		t.Fatal(activity, err)
	}
	page, err := s.InputPage(t.Context(), owner.ID, "queued", 0, 10)
	if err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
	page, err = s.InputPage(t.Context(), owner.ID, "all", 0, 10)
	if err != nil || len(page.Items) != 2 || !page.Items[1].Steering.Consumed || *page.Items[1].TurnID != turn.ID {
		t.Fatal(page, err)
	}
	mustFail(t, s, "UPDATE inputs SET steered_turn_id=NULL WHERE id=?", accepted.Input.ID)
	finishMailTest(t, s, turn.ID, session.Succeeded)
	fork := forkTest(t, s, forkRequestTest(t, s, owner.ID, "steering_fork", 2))
	history, err := s.History(t.Context(), fork.Root.ID, 0, 10)
	if err != nil || len(history) != 2 || !history[0].OpeningInput || history[1].OpeningInput || history[1].InputID != nil || history[1].DesignContext == nil || !reflect.DeepEqual(history[1].Parts, request.Parts) {
		t.Fatal("fork changed steering provenance", history, err)
	}
}

func TestInputSteeringPromotionExactRetryDeletionAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "opening")
	turn := claim(t, s, owner.ID).Turn
	input := submit(t, s, owner.ID, "queued")
	request := session.SteerInputRequest{ID: "promotion", SessionID: owner.ID, InputID: input.Input.ID, TurnID: turn.ID}
	first, err := s.SteerInput(t.Context(), request)
	if err != nil || first.Input.Steering.Consumed {
		t.Fatal(first, err)
	}
	changed := request
	changed.TurnID = "other"
	if _, err := s.SteerInput(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatal("changed retry accepted", err)
	}
	changed = request
	changed.ID = "second"
	if _, err := s.SteerInput(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatal("input retargeted", err)
	}
	if _, err := s.InputSteering(t.Context(), "foreign", request.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign receipt exposed", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	same, err := s.SteerInput(t.Context(), request)
	if err != nil || !same.Steering.CreatedAt.Equal(first.Steering.CreatedAt) || same.Input.State != session.Queued {
		t.Fatal("retry retested ended target", same, err)
	}
	next := claim(t, s, owner.ID)
	if next.Input.ID != input.Input.ID || next.Turn.ID == turn.ID || next.Input.Steering.Consumed {
		t.Fatal("untaken steering did not open its own turn", next)
	}
	messages, err := s.ObserveSteers(t.Context(), next.Turn.ID)
	if err != nil || len(messages) != 0 {
		t.Fatal("untaken input retargeted", messages, err)
	}
	finishMailTest(t, s, next.Turn.ID, session.Succeeded)
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	same, err = s.SteerInput(t.Context(), request)
	if err != nil || !same.Deleted || same.Input != nil {
		t.Fatal("deletion lost receipt", same, err)
	}
	mustFail(t, s, "DELETE FROM input_steering WHERE id=?", request.ID)
}

func TestInputSteeringAtomicBoundariesAndCancellation(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	turn, message := cellCall(t, s, owner.ID)
	input := steerTest(t, s, owner.ID, turn.ID, "steer")
	if _, err := s.ObserveSteers(t.Context(), turn.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("steered before tool call completion", err)
	}
	cell, _, err := s.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: turn.ID, CallMessageID: message.ID, CallID: "call"})
	if err != nil {
		t.Fatal(err)
	}
	settleMailCell(t, s, cell)
	attempt := reserveTest(t, s, attemptRequest(turn.ID, "model"))
	if _, err := s.ObserveSteers(t.Context(), turn.ID); !errors.Is(err, ErrBusy) {
		t.Fatal("steered during reserved model attempt", err)
	}
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, session.ModelAttemptResult{State: session.AttemptCancelled}, nil); err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `CREATE TRIGGER fail_steer_message BEFORE INSERT ON messages WHEN NEW.opening_input=0 AND NEW.input_id IS NOT NULL BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.ObserveSteers(t.Context(), turn.ID); err == nil {
		t.Fatal("injected message failure ignored")
	}
	current, err := s.Input(t.Context(), input.Input.ID)
	if err != nil || current.State != session.Queued || current.Steering.Consumed {
		t.Fatal("partial consumption survived rollback", current, err)
	}
	execTest(t, s, "DROP TRIGGER fail_steer_message")
	cancelled := steerTest(t, s, owner.ID, turn.ID, "cancelled")
	if _, err := s.CancelInput(t.Context(), cancelled.Input.ID); err != nil {
		t.Fatal(err)
	}
	messages, err := s.ObserveSteers(t.Context(), turn.ID)
	if err != nil || len(messages) != 1 || *messages[0].InputID != input.Input.ID {
		t.Fatal(messages, err)
	}
	if _, err := s.CancelInput(t.Context(), input.Input.ID); err != nil {
		t.Fatal(err)
	}
	currentTurn, err := s.Turn(t.Context(), turn.ID)
	if err != nil || currentTurn.State != session.Cancelling {
		t.Fatal("consumed input did not address linked turn", currentTurn, err)
	}
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(t.Context(), owner.ID); !errors.Is(err, ErrNoWork) {
		t.Fatal("consumed or cancelled steer was replayed", err)
	}
}

func TestInputSteeringAdmissionRejectsStaleTargetAndRollsBack(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "opening")
	turn := claim(t, s, owner.ID).Turn
	request := Submission{SessionID: owner.ID, Source: session.UserInput, Delivery: session.DeliverySteer, TargetTurnID: new(session.TurnID("missing")), Parts: []session.Part{{Type: "text", Text: "steer"}}}
	identity := session.RequestIdentity{ClientID: "human", RequestID: "steer"}
	if _, err := s.Admit(t.Context(), identity, request); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	request.TargetTurnID = &turn.ID
	execTest(t, s, `CREATE TRIGGER fail_steer BEFORE INSERT ON input_steering BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.Admit(t.Context(), identity, request); err == nil {
		t.Fatal("injected routing failure ignored")
	}
	if count(t, s, "inputs") != 1 || count(t, s, "receipts") != 1 {
		t.Fatal("partial admission survived rollback")
	}
	execTest(t, s, "DROP TRIGGER fail_steer")
	finishMailTest(t, s, turn.ID, session.Succeeded)
	if _, err := s.Admit(t.Context(), identity, request); !errors.Is(err, ErrConflict) {
		t.Fatal("stale explicit target queued", err)
	}
	request.TargetTurnID = nil
	accepted, err := s.Admit(t.Context(), identity, request)
	if err != nil || accepted.Input.Steering != nil {
		t.Fatal("idle steer did not queue", accepted, err)
	}
}

func TestInputSteeringConsumptionIsBounded(t *testing.T) {
	for _, large := range []bool{false, true} {
		t.Run(strconv.FormatBool(large), func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			submit(t, s, owner.ID, "opening")
			turn := claim(t, s, owner.ID).Turn
			total := 25
			size := 1
			want := 20
			if large {
				total = 4
				size = 700000
				want = 2
			}
			for i := range total {
				_, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "human", RequestID: strconv.Itoa(i)}, Submission{SessionID: owner.ID, Source: session.UserInput, Delivery: session.DeliverySteer, Parts: []session.Part{{Type: "text", Text: strings.Repeat("x", size)}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			messages, err := s.ObserveSteers(t.Context(), turn.ID)
			if err != nil || len(messages) != want {
				t.Fatal(len(messages), err)
			}
			activity, err := s.Activity(t.Context(), owner.ID)
			if err != nil || activity.QueuedInputCount != int64(total-want) {
				t.Fatal(activity, err)
			}
		})
	}
}

func TestInputSteeringSchemaPredecessorRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s := openTest(t, path)
	execTest(t, s, "PRAGMA user_version=48")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), path); !errors.Is(err, ErrSchema) {
		t.Fatal("schema48 opened", err)
	}
}

func TestInputSteeringGuestChildDefaultAndExplicitQueueUseOriginalAuthority(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	child := controlChild(t, s, owner.ID, "child")
	target := claim(t, s, child.Session.ID).Turn
	for _, delivery := range []session.InputDelivery{"", session.DeliveryQueued} {
		key := "submit_" + string(delivery)
		before := count(t, s, "logical_writes")
		request := session.ChildSubmit{SessionID: child.Session.ID, Delivery: delivery, Parts: []session.Part{{Type: "text", Text: key}}}
		op := childControl(t, s, owner, cell, key, "agents.submit", request)
		result := applyControl(t, s, op)
		same := applyControl(t, s, op)
		if string(result.Value) != string(same.Value) || count(t, s, "logical_writes") != before+1 {
			t.Fatal("submission repeated admission charge", string(result.Value))
		}
		admitted, err := s.Admission(t.Context(), session.RequestIdentity{ClientID: "operation", RequestID: key})
		if err != nil || admitted.Input.Source != session.AgentInput {
			t.Fatal(admitted, err)
		}
		messages, err := s.ObserveSteers(t.Context(), target.ID)
		want := 0
		if delivery == "" {
			want = 1
		}
		if err != nil || len(messages) != want {
			t.Fatal("guest delivery mismatch", messages, err)
		}
		current, err := s.Input(t.Context(), admitted.Input.ID)
		if err != nil {
			t.Fatal(err)
		}
		if delivery == "" && (current.TurnID == nil || *current.TurnID != target.ID || !current.Steering.Consumed) {
			t.Fatal(current)
		}
		if delivery == session.DeliveryQueued && (current.TurnID != nil || current.Steering != nil) {
			t.Fatal(current)
		}
	}
	request := session.ChildSubmit{SessionID: child.Session.ID, Parts: []session.Part{{Type: "text", Text: "revoked"}}}
	op := childControl(t, s, owner, cell, "revoked", "agents.submit", request)
	if _, err := s.RevokeGrant(t.Context(), "grant_revoked"); err != nil {
		t.Fatal(err)
	}
	before := count(t, s, "inputs")
	if _, err := s.ApplyChildControl(t.Context(), op.ID); err == nil || count(t, s, "inputs") != before {
		t.Fatal("revoked child authority steered", err)
	}
}

func TestInputSteeringConcurrentConsumptionAcrossConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	first, second := openTest(t, path), openTest(t, path)
	_, owner := create(t, first, nil)
	submit(t, first, owner.ID, "opening")
	target := claim(t, first, owner.ID).Turn
	steerTest(t, first, owner.ID, target.ID, "only")
	var workers sync.WaitGroup
	var consumed atomic.Int32
	for i := range 8 {
		workers.Go(func() {
			db := first
			if i%2 != 0 {
				db = second
			}
			messages, err := db.ObserveSteers(t.Context(), target.ID)
			if err != nil {
				t.Error(err)
				return
			}
			consumed.Add(int32(len(messages)))
		})
	}
	workers.Wait()
	if consumed.Load() != 1 || count(t, first, "messages") != 2 {
		t.Fatal("concurrent observers duplicated input", consumed.Load())
	}
}
