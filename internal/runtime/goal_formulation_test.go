package runtime

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestGoalFormulationPublicAdmissionAndReadForRootAndChild(t *testing.T) {
	var calls atomic.Int64
	r := openTest(t, t.TempDir(), providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
		if request.Purpose == session.GoalFormulationPurpose {
			calls.Add(1)
			return model.Response{Parts: []session.Part{{Type: "text", Text: "Complete the recorded objective."}}}, nil
		}
		return model.Response{Parts: []session.Part{{Type: "text", Text: "seed history"}}}, nil
	}))
	root := createTest(t, r)
	submitTest(t, r, root.ID, "root-seed")
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child-seed"}, store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "child objective"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	terminal := func(a store.Admission) bool { return a.Turn != nil && a.Turn.State.Terminal() }
	waitTestWithin(t, r, "root-seed", terminal, 30*time.Second)
	waitTestWithin(t, r, "child-seed", terminal, 30*time.Second)
	for _, owner := range []session.Session{root, *child.Session} {
		key := string(owner.ID)
		before, err := r.History(t.Context(), owner.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		identity := session.RequestIdentity{ClientID: "test", RequestID: key}
		request := session.GoalFormulationRequest{GoalID: session.GoalID("goal_" + key), MaxContinuations: new(int64(9007199254740993)), Start: false}
		first, err := r.FormulateGoal(t.Context(), identity, owner.ID, request)
		if err != nil || first.Input.Kind != session.GoalFormulationInputKind || len(first.Input.Parts) != 0 {
			t.Fatalf("admission=%+v %v", first, err)
		}
		result := waitTestWithin(t, r, key, terminal, 30*time.Second)
		if result.Turn.Kind != session.GoalFormulationInputKind || result.Turn.State != session.Succeeded {
			t.Fatal(result)
		}
		retried, err := r.FormulateGoal(t.Context(), identity, owner.ID, request)
		if err != nil || retried.Input.ID != first.Input.ID {
			t.Fatalf("lost acknowledgement changed admission: %+v %v", retried, err)
		}
		changed := request
		changed.Start = true
		if _, err := r.FormulateGoal(t.Context(), identity, owner.ID, changed); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("changed admission reused identity: %v", err)
		}
		attempts, err := r.ModelAttempts(t.Context(), result.Turn.ID, "", 100)
		if err != nil || len(attempts) != 1 {
			t.Fatalf("attempts=%+v %v", attempts, err)
		}
		candidate, err := r.GoalFormulation(t.Context(), owner.ID, attempts[0].ID)
		if err != nil || candidate.Rejection != nil || candidate.Request.Start || *candidate.Request.MaxContinuations != 9007199254740993 {
			t.Fatalf("candidate=%+v %v", candidate, err)
		}
		foreignOwner := root.ID
		if owner.ID == root.ID {
			foreignOwner = child.Session.ID
		}
		if _, err := r.GoalFormulation(t.Context(), foreignOwner, attempts[0].ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("foreign candidate leaked: %v", err)
		}
		if _, err := r.CancelGoal(t.Context(), owner.ID, request.GoalID); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			value, err := r.GoalFormulation(t.Context(), owner.ID, attempts[0].ID)
			if err != nil || !reflect.DeepEqual(value, candidate) {
				t.Fatalf("candidate changed with later goal state: %+v %v", value, err)
			}
		}
		failed, err := r.FormulateGoal(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key + "_rejected"}, owner.ID, session.GoalFormulationRequest{GoalID: session.GoalID("rejected_" + key)})
		if err != nil || failed.Input == nil {
			t.Fatal(err)
		}
		rejected := waitTestWithin(t, r, key+"_rejected", terminal, 30*time.Second)
		if rejected.Turn.State != session.Failed {
			t.Fatal("stale CAS was accepted", rejected)
		}
		attempts, err = r.ModelAttempts(t.Context(), rejected.Turn.ID, "", 100)
		if err != nil || len(attempts) != 1 {
			t.Fatal(attempts, err)
		}
		rejection, err := r.GoalFormulation(t.Context(), owner.ID, attempts[0].ID)
		if err != nil || rejection.Rejection == nil || rejection.Text == "" {
			t.Fatal("semantic rejection lost candidate", rejection, err)
		}
		after, err := r.History(t.Context(), owner.ID, 0, 100)
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatal("formulation or inspection authored history", err)
		}
		if owner.ParentID != nil {
			if err := r.DeleteSubtree(t.Context(), owner.ID); err != nil {
				t.Fatal(err)
			}
			retained, err := r.GoalFormulation(t.Context(), owner.ID, candidate.AttemptID)
			if err != nil || !reflect.DeepEqual(retained, candidate) {
				t.Fatalf("deleted child lost immutable candidate acceptance: %+v %v", retained, err)
			}
			replayed, err := r.FormulateGoal(t.Context(), identity, owner.ID, request)
			if err != nil || replayed.Receipt.DeletedAt == nil || replayed.Input != nil {
				t.Fatalf("deleted owner retry requeued work: %+v %v", replayed, err)
			}
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("inspection or retry ran provider: %d", calls.Load())
	}
}
