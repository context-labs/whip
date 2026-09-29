package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func nativeEnableGoals(t *testing.T, m *nativeModel) {
	t.Helper()
	m.owner = nativeMenuRPC[protocol.Session](t, m.connection, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: m.owner.ID, ExpectedRevision: m.owner.ConfigRevision, Patch: protocol.ConfigPatch{GoalsEnabled: new(true)}})
}

func TestNativeGoalCommandsKeepOriginalOwnerAndCreationOnRetry(t *testing.T) {
	m, _ := nativeUIFixture(t)
	nativeEnableGoals(t, m)
	result := nativeUIControl(t, m, "/goal original objective")
	if result.err != nil || !strings.Contains(m.notice, "original objective") || !strings.Contains(m.notice, "Additional continuations:") {
		t.Fatal(result.err, m.notice)
	}
	original := nativeMenuRPC[protocol.CurrentGoalResult](t, m.connection, "goals.current", protocol.SessionParams{SessionID: m.owner.ID}).Goal
	if original == nil || original.Spec.Text != "original objective" || result.retry == nil {
		t.Fatal(original)
	}
	// Repeating an old control must not make it current after replacement.
	cancelled := nativeUIControl(t, m, "/goal clear")
	if cancelled.err != nil {
		t.Fatal(cancelled.err)
	}
	nativeGoalWaitIdle(t, m)
	second := nativeUIControl(t, m, "/goal replacement objective")
	if second.err != nil {
		t.Fatal(second.err)
	}
	newGoal := nativeMenuRPC[protocol.CurrentGoalResult](t, m.connection, "goals.current", protocol.SessionParams{SessionID: m.owner.ID}).Goal
	for _, exact := range []nativeControlResult{result, cancelled} {
		repeated := exact.retry().(nativeControlResult)
		if repeated.err != nil {
			t.Fatal(repeated.err)
		}
	}
	current := nativeMenuRPC[protocol.CurrentGoalResult](t, m.connection, "goals.current", protocol.SessionParams{SessionID: m.owner.ID}).Goal
	if current == nil || current.ID != newGoal.ID || current.Spec.Text != "replacement objective" {
		t.Fatal("retry retargeted latest goal", current)
	}
	if result := nativeUIControl(t, m, "/goal"); result.err != nil || !strings.Contains(m.notice, "replacement objective") {
		t.Fatal(result.err, m.notice)
	}
	if result := nativeUIControl(t, m, "/goal clear"); result.err != nil {
		t.Fatal(result.err)
	}
}

func TestNativeGoalFormulationUsesJournalAndOriginalInput(t *testing.T) {
	m, _ := nativeUIFixture(t)
	nativeEnableGoals(t, m)
	input := nativeUISubmit(t, m, "build the requested exporter")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := input.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	nativeUIRead(t, m)
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	command := m.command("/goal-from-context 2")
	if command == nil {
		t.Fatal(m.status)
	}
	prepared := command().(nativeControlResult)
	if prepared.err != nil || prepared.input == nil {
		t.Fatal(prepared.err)
	}
	_, send := m.Update(prepared)
	if send == nil {
		t.Fatal(m.status)
	}
	raw, err := m.recovery.read(m.owner.ID)
	if err != nil {
		t.Fatal("helper input dispatched without durable original record", err)
	}
	var record client.InputRecord
	if err := json.Unmarshal(raw, &record); err != nil || record.Method != "goals.formulate" {
		t.Fatal(record, err)
	}
	var params protocol.FormulateGoalParams
	if err := json.Unmarshal(record.Params, &params); err != nil || params.SessionID != m.owner.ID || params.Request.TailMessages != 2 || !params.Request.Start {
		t.Fatal(params, err)
	}
	admitted := send().(nativeSubmission)
	m.Update(admitted)
	if admitted.err != nil || admitted.admission.Input == nil || admitted.admission.Input.Kind != "goal_formulation" {
		t.Fatal(admitted.err, admitted.admission)
	}
	if _, err := prepared.input.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	goal := nativeMenuRPC[protocol.CurrentGoalResult](t, m.connection, "goals.current", protocol.SessionParams{SessionID: m.owner.ID}).Goal
	if goal == nil || goal.ID != params.Request.GoalID || goal.OriginFormulationAttemptID == nil {
		t.Fatal("helper did not create canonical goal provenance", goal)
	}
	if result := nativeUIControl(t, m, "/goal clear"); result.err != nil {
		t.Fatal(result.err)
	}
}

func TestNativeScheduleCommandsBoundListAndNeverRetargetRetries(t *testing.T) {
	m, _ := nativeUIFixture(t)
	created := nativeUIControl(t, m, "/schedule @at 2500-01-02T03:04:05Z preserve\n  this prompt")
	if created.err != nil {
		t.Fatal(created.err)
	}
	page := nativeMenuRPC[protocol.SchedulesResult](t, m.connection, "schedules.list", protocol.ListSchedulesParams{SessionID: m.owner.ID, Limit: 100})
	if len(page.Items) != 1 {
		t.Fatal(page)
	}
	id := page.Items[0].ID
	stored := nativeMenuRPC[protocol.ScheduleResult](t, m.connection, "schedules.get", protocol.ScheduleParams{SessionID: m.owner.ID, ScheduleID: id})
	if len(stored.Parts) != 1 || stored.Parts[0].Text != "preserve\n  this prompt" {
		t.Fatal("changed original scheduled prompt", stored.Parts)
	}
	if result := nativeUIControl(t, m, "/schedule list"); result.err != nil || !strings.Contains(m.notice, string(id)) {
		t.Fatal(result.err, m.notice)
	}
	if result := nativeUIControl(t, m, "/schedule cancel "+string(id)); result.err != nil {
		t.Fatal(result.err)
	}
	if result := created.retry().(nativeControlResult); result.err != nil || !strings.Contains(result.notice, "cancelled") {
		t.Fatal("creation retry rearmed schedule", result)
	}
	page = nativeMenuRPC[protocol.SchedulesResult](t, m.connection, "schedules.list", protocol.ListSchedulesParams{SessionID: m.owner.ID, Limit: 100})
	if len(page.Items) != 1 || page.Items[0].CancelledAt == nil {
		t.Fatal(page)
	}
	for _, text := range []string{"/goal-from-context 1", "/goal-from-context 101", "/schedule @at", "/schedule cancel", "/schedule every 1m hi"} {
		if command := m.command(text); command != nil {
			t.Fatal("invalid syntax admitted", text)
		}
	}
}

func TestNativeGoalResumeRejectsChangedTargetWithoutRetarget(t *testing.T) {
	m, _ := nativeUIFixture(t)
	nativeEnableGoals(t, m)
	first := nativeMenuRPC[protocol.GoalAdmission](t, m.connection, "goals.create", protocol.CreateGoalParams{SessionID: m.owner.ID, GoalID: "first", Spec: protocol.GoalRequest{Text: "first", MaxContinuations: new(protocol.Counter(0))}})
	prepare := m.command("/goal resume")
	if prepare == nil {
		t.Fatal(m.status)
	}
	original := prepare().(nativeControlResult)
	if original.err != nil || original.input == nil {
		t.Fatal(original.err)
	}
	second := nativeMenuRPC[protocol.GoalAdmission](t, m.connection, "goals.create", protocol.CreateGoalParams{SessionID: m.owner.ID, GoalID: "second", ExpectedCurrent: new(first.Goal.GoalRef), Spec: protocol.GoalRequest{Text: "second", MaxContinuations: new(protocol.Counter(0))}})
	_, send := m.Update(original)
	if send == nil {
		t.Fatal(m.status)
	}
	rejected := send().(nativeSubmission)
	m.Update(rejected)
	if rejected.err == nil || rejected.uncertain || m.rejected != nil || m.uncertain != nil {
		t.Fatal("stale goal resume retargeted or became uncertain", rejected, m.status)
	}
	activity, err := m.handle.Activity(t.Context())
	if err != nil || activity.ActiveTurn != nil || activity.QueuedInputCount != 0 {
		t.Fatal(activity, err)
	}
	prepare = m.command("/goal resume")
	current := prepare().(nativeControlResult)
	_, send = m.Update(current)
	accepted := send().(nativeSubmission)
	m.Update(accepted)
	if accepted.err != nil || accepted.admission.Input == nil || accepted.admission.Input.Goal == nil || accepted.admission.Input.Goal.ID != second.Goal.ID {
		t.Fatal(accepted)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := current.input.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func nativeGoalWaitIdle(t *testing.T, m *nativeModel) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for {
		activity, err := m.handle.Activity(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if activity.ActiveTurn == nil && activity.QueuedInputCount == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}
