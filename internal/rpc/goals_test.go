package rpc_test

import (
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestGoalsRPCStableCreationCurrentAndExactCounters(t *testing.T) {
	_, client := fixture(t)
	tree := create(t, client)
	if value := call[protocol.CurrentGoalResult](t, client, "goals.current", protocol.SessionParams{SessionID: tree.Root.ID}); value.Goal != nil {
		t.Fatal(value)
	}
	params := protocol.CreateGoalParams{SessionID: tree.Root.ID, GoalID: "first", Spec: protocol.GoalRequest{Text: "objective", MaxContinuations: new(protocol.Counter(9007199254740993))}}
	first := call[protocol.GoalAdmission](t, client, "goals.create", params)
	if first.Goal == nil || first.Initial != nil || !first.Current || first.Goal.Spec.MaxContinuations != 9007199254740993 {
		t.Fatal(first)
	}
	replacement := call[protocol.GoalAdmission](t, client, "goals.create", protocol.CreateGoalParams{SessionID: tree.Root.ID, GoalID: "replacement", ExpectedCurrent: &first.Goal.GoalRef, Spec: protocol.GoalRequest{Text: "replacement", MaxContinuations: new(protocol.Counter(0))}})
	replay := call[protocol.GoalAdmission](t, client, "goals.create", params)
	if replay.Current || replay.Goal.State != "superseded" || replay.Initial != nil {
		t.Fatal(replay)
	}
	current := call[protocol.CurrentGoalResult](t, client, "goals.current", protocol.SessionParams{SessionID: tree.Root.ID})
	if current.Goal.ID != replacement.ID {
		t.Fatal(current)
	}
	resumed := call[protocol.Admission](t, client, "goals.resume", protocol.ResumeGoalParams{SessionID: tree.Root.ID, Goal: replacement.Goal.GoalRef, Identity: protocol.RequestIdentity{ClientID: "goal_rpc", RequestID: "resume"}})
	if resumed.Input == nil || resumed.Input.Source != "goal" || resumed.Input.Goal == nil || resumed.Input.Goal.ID != replacement.ID {
		t.Fatal(resumed)
	}
	cancelled := call[protocol.GoalChange](t, client, "goals.cancel", protocol.GoalParams{SessionID: tree.Root.ID, GoalID: replacement.ID})
	if cancelled.Goal.State != "cancelled" {
		t.Fatal(cancelled)
	}
	retry := call[protocol.Admission](t, client, "goals.resume", protocol.ResumeGoalParams{SessionID: tree.Root.ID, Goal: replacement.Goal.GoalRef, Identity: protocol.RequestIdentity{ClientID: "goal_rpc", RequestID: "resume"}})
	if retry.Input.ID != resumed.Input.ID {
		t.Fatal(retry)
	}
	got := call[protocol.Goal](t, client, "goals.get", protocol.GoalParams{SessionID: tree.Root.ID, GoalID: replacement.ID})
	if got.State != "cancelled" {
		t.Fatal(got)
	}
	var invalid protocol.GoalAdmission
	if err := client.Call(t.Context(), "goals.create", params, &invalid); err != nil {
		t.Fatal(err)
	}
	params.Spec.Text = "changed"
	if err := client.Call(t.Context(), "goals.create", params, &invalid); err == nil {
		t.Fatal("changed goal payload admitted")
	}
}
