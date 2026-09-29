package store

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestGoalFormulationDefaultCapturedAtAdmissionAndNotRetry(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	compactionHistoryTest(t, s, owner.ID, "seed")
	identity := session.RequestIdentity{ClientID: "test", RequestID: "formulate"}
	request := session.GoalFormulationRequest{GoalID: "goal"}
	first, err := s.AdmitGoalFormulationWithDefault(t.Context(), identity, owner.ID, request, 7)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.AdmitGoalFormulationWithDefault(t.Context(), identity, owner.ID, request, -1)
	if err != nil || retry.Input.ID != first.Input.ID {
		t.Fatal("formulation retry rechecked injected default", retry, err)
	}
	claimed := claim(t, s, owner.ID)
	captured, err := s.GoalFormulationInput(t.Context(), claimed.Turn.ID)
	if err != nil || captured.Request.MaxContinuations == nil || *captured.Request.MaxContinuations != 7 {
		t.Fatal("formulation did not freeze allowance", captured, err)
	}
	changed := request
	changed.MaxContinuations = new(int64(7))
	if _, err := s.MatchGoalFormulation(t.Context(), identity, owner.ID, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("resolved allowance replaced original identity", err)
	}
	attempt := formulationAttemptTest(t, s, claimed, "formulation")
	result := settleFormulationTest(t, s, attempt.ID)
	if !result.Accepted {
		t.Fatal(result)
	}
	goal, err := s.Goal(t.Context(), owner.ID, request.GoalID)
	if err != nil || goal.Spec.MaxContinuations != 7 {
		t.Fatal("settlement lost admitted allowance", goal, err)
	}
}
