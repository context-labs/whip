package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestGoalFormulationContractDefaultsAndExactCounters(t *testing.T) {
	base := FormulateGoalParams{Identity: RequestIdentity{ClientID: "client", RequestID: "request"}, SessionID: "owner", Request: GoalFormulationRequest{GoalID: "goal", MaxContinuations: new(Counter(9007199254740993))}}
	for _, tail := range []int{0, 2, 100, 1, -1, 101} {
		base.Request.TailMessages = tail
		raw, err := json.Marshal(base)
		if err != nil {
			t.Fatal(err)
		}
		err = Validate("FormulateGoalParams", raw)
		valid := tail == 0 || tail >= 2 && tail <= 100
		if (err == nil) != valid {
			t.Fatalf("tail=%d valid=%v err=%v", tail, valid, err)
		}
		if valid {
			var decoded FormulateGoalParams
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			domain := decoded.Request.Domain()
			if domain.Start || *domain.MaxContinuations != 9007199254740993 || domain.Expected != nil {
				t.Fatal(domain)
			}
		}
	}
}

func TestGoalFormulationProjectionKeepsHistoricalAcceptance(t *testing.T) {
	value := session.GoalFormulation{InputID: "input", SessionID: "owner", Request: session.GoalFormulationRequest{GoalID: "goal", TailMessages: 8}, TurnID: "turn", AttemptID: "attempt", Text: "Objective", CreatedAt: time.Now()}
	for _, state := range []session.GoalState{session.GoalArmed, session.GoalCancelled, session.GoalSuperseded} {
		goal := GoalFromDomain(session.Goal{ID: "goal", State: state, OriginFormulationAttemptID: &value.AttemptID})
		candidate := GoalFormulationFromDomain(value)
		if !candidate.Accepted || candidate.Rejection != nil || goal.OriginFormulationAttemptID == nil || *goal.OriginFormulationAttemptID != candidate.AttemptID {
			t.Fatalf("historical origin changed with live state: %+v %+v", candidate, goal)
		}
	}
	// A retained candidate needs no live goal to project historical acceptance.
	if !GoalFormulationFromDomain(value).Accepted {
		t.Fatal("candidate lookup depended on deleted goal")
	}
	value.Rejection = new("capacity exhausted")
	if rejected := GoalFormulationFromDomain(value); rejected.Accepted || rejected.Rejection == nil {
		t.Fatal(rejected)
	}
}
