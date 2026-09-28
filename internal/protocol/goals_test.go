package protocol

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestGoalEligibilityFalsePatchAndInputProvenance(t *testing.T) {
	raw, err := json.Marshal(ConfigPatch{GoalsEnabled: new(false)})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"goals_enabled":false}` {
		t.Fatalf("false lost: %s", raw)
	}
	var patch ConfigPatch
	if err := json.Unmarshal(raw, &patch); err != nil {
		t.Fatal(err)
	}
	domain, err := patch.Domain()
	if err != nil || domain.GoalsEnabled == nil || *domain.GoalsEnabled {
		t.Fatal(domain, err)
	}
	value := InputFromDomain(session.Input{ID: "input", SessionID: "owner", Source: session.GoalInput, Kind: session.PromptInput, State: session.Queued, Goal: &session.GoalRef{ID: "goal", Revision: 9007199254740993}, Parts: []session.Part{{Type: "text", Text: "Work on the goal."}}, CreatedAt: time.Now()})
	raw, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("Input", raw); err != nil {
		t.Fatal(err)
	}
	if value.Goal == nil || value.Goal.Revision != 9007199254740993 || value.Source != "goal" {
		t.Fatal(value)
	}
}
