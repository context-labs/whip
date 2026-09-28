package rpc_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestGoalFormulationRPCUsesOrdinaryReceiptAndImmutableEvidence(t *testing.T) {
	r, client := fixture(t)
	tree := create(t, client)
	identity := protocol.RequestIdentity{ClientID: "formulation", RequestID: "seed"}
	call[protocol.Admission](t, client, "sessions.submit", protocol.SubmitParams{Identity: identity, SessionID: tree.Root.ID, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "Create the report."}}})
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	wait := func(key string) protocol.Admission {
		t.Helper()
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			value := call[protocol.Admission](t, client, "receipts.get", protocol.RequestIdentity{ClientID: "formulation", RequestID: protocol.ID(key)})
			if value.Turn != nil && value.Turn.FinishedAt != nil {
				return value
			}
			select {
			case <-ctx.Done():
				t.Fatal("formulation did not finish", r.Err())
			case <-ticker.C:
			}
		}
	}
	wait("seed")
	params := protocol.FormulateGoalParams{Identity: protocol.RequestIdentity{ClientID: "formulation", RequestID: "formulate"}, SessionID: tree.Root.ID, Request: protocol.GoalFormulationRequest{GoalID: "formulated", MaxContinuations: new(protocol.Counter(0)), Start: false}}
	first := call[protocol.Admission](t, client, "goals.formulate", params)
	if first.Input.Kind != "goal_formulation" || len(first.Input.Parts) != 0 {
		t.Fatal(first)
	}
	completed := wait("formulate")
	if completed.Turn.State != "succeeded" || completed.Turn.Kind != "goal_formulation" {
		t.Fatal(completed)
	}
	attempts := call[protocol.ModelAttemptsResult](t, client, "turns.attempts", protocol.ModelAttemptsParams{TurnID: completed.Turn.ID, Limit: 100})
	lookup := protocol.GoalFormulationParams{SessionID: tree.Root.ID, AttemptID: attempts.Items[0].ID}
	candidate := call[protocol.GoalFormulation](t, client, "goals.formulation", lookup)
	if !candidate.Accepted || candidate.Rejection != nil || candidate.Request.Start || candidate.Request.TailMessages != 8 || *candidate.Request.MaxContinuations != 0 {
		t.Fatal(candidate)
	}
	goal := call[protocol.Goal](t, client, "goals.get", protocol.GoalParams{SessionID: tree.Root.ID, GoalID: "formulated"})
	if goal.OriginFormulationAttemptID == nil || *goal.OriginFormulationAttemptID != candidate.AttemptID {
		t.Fatal("goal lost formulation origin", goal)
	}
	call[protocol.GoalChange](t, client, "goals.cancel", protocol.GoalParams{SessionID: tree.Root.ID, GoalID: "formulated"})
	again := call[protocol.GoalFormulation](t, client, "goals.formulation", lookup)
	if !reflect.DeepEqual(candidate, again) {
		t.Fatal("candidate projected mutable goal state")
	}
	retry := call[protocol.Admission](t, client, "goals.formulate", params)
	if retry.Input.ID != first.Input.ID {
		t.Fatal("exact retry queued another helper")
	}
	params.Request.GoalID = "different"
	var invalid protocol.Admission
	if err := client.Call(t.Context(), "goals.formulate", params, &invalid); err == nil {
		t.Fatal("changed request identity accepted")
	}
	lookup.SessionID = "foreign"
	var foreign protocol.GoalFormulation
	if err := client.Call(t.Context(), "goals.formulation", lookup, &foreign); err == nil {
		t.Fatal("foreign candidate exposed")
	}
	history, err := r.History(t.Context(), session.SessionID(tree.Root.ID), 0, 100)
	if err != nil || len(history) != 2 {
		t.Fatal("formulation added ordinary history", history, err)
	}
}
