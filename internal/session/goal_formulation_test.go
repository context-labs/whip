package session

import "testing"

func TestGoalFormulationRequestBoundsAndCapture(t *testing.T) {
	original := GoalFormulationRequest{GoalID: "new-goal", Expected: &GoalRef{ID: "old-goal", Revision: 1}, MaxContinuations: new(int64(0)), Start: true}
	resolved, err := original.Resolve()
	if err != nil || resolved.TailMessages != 8 || *resolved.MaxContinuations != 0 || !resolved.Start {
		t.Fatal(resolved, err)
	}
	original.Expected.Revision = 2
	*original.MaxContinuations = 5
	if resolved.Expected.Revision != 1 || *resolved.MaxContinuations != 0 {
		t.Fatal("captured request aliases caller values")
	}
	for _, n := range []int{2, 8, 100} {
		original.TailMessages = n
		if _, err := original.Resolve(); err != nil {
			t.Fatal(n, err)
		}
	}
	for _, mutate := range []func(*GoalFormulationRequest){
		func(r *GoalFormulationRequest) { r.TailMessages = 1 },
		func(r *GoalFormulationRequest) { r.TailMessages = 101 },
		func(r *GoalFormulationRequest) { r.TailMessages = -1 },
		func(r *GoalFormulationRequest) { r.MaxContinuations = new(int64(-1)) },
		func(r *GoalFormulationRequest) { r.GoalID = "" },
		func(r *GoalFormulationRequest) { r.Expected = &GoalRef{ID: "old", Revision: 0} },
	} {
		invalid := resolved
		mutate(&invalid)
		if _, err := invalid.Resolve(); err == nil {
			t.Fatal("invalid request accepted", invalid)
		}
	}
}
