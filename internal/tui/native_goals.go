package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/google/uuid"

	"github.com/context-labs/whip/internal/protocol"
)

func (m *nativeModel) currentGoal(ctx context.Context, owner protocol.ID) (*protocol.Goal, error) {
	var value protocol.CurrentGoalResult
	if err := m.connection.Call(ctx, "goals.current", protocol.SessionParams{SessionID: owner}, &value); err != nil {
		return nil, err
	}
	if value.Goal != nil && value.Goal.SessionID != owner {
		return nil, errors.New("goal ownership mismatch")
	}
	return value.Goal, nil
}

func nativeGoalText(goal *protocol.Goal) string {
	if goal == nil {
		return "No goal for this session. /goal <text> starts one; /goal-from-context [2..100] formulates one."
	}
	text := fmt.Sprintf("Goal %s · revision %d · %s\nAdditional continuations: %d of %d\n%s", goal.ID, goal.Revision, goal.State, goal.ContinuationsUsed, goal.Spec.MaxContinuations, goal.Spec.Text)
	if goal.StopReason != nil {
		text += "\nStop reason: " + *goal.StopReason
	}
	return text
}

func (m *nativeModel) goalCommand(name, args string) tea.Cmd {
	owner := m.owner.ID
	if name == "/goal" && (args == "" || args == "status") {
		return m.control("Current goal", false, func(ctx context.Context) nativeControlResult {
			goal, err := m.currentGoal(ctx, owner)
			return nativeControlResult{notice: nativeGoalText(goal), err: err}
		})
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	if name == "/goal-from-context" || args == "resume" {
		window := 0
		if name == "/goal-from-context" && args != "" {
			var err error
			window, err = strconv.Atoi(args)
			if err != nil || window < 2 || window > 100 {
				m.status = "usage: /goal-from-context [2..100]"
				return nil
			}
		}
		identity := protocol.RequestIdentity{ClientID: "tui", RequestID: protocol.ID(uuid.NewString())}
		return m.control("Prepare goal input", false, func(ctx context.Context) nativeControlResult {
			goal, err := m.currentGoal(ctx, owner)
			if err != nil {
				return nativeControlResult{err: err}
			}
			var current *protocol.GoalRef
			if goal != nil {
				current = new(goal.GoalRef)
			}
			method := "goals.formulate"
			var params any = protocol.FormulateGoalParams{SessionID: owner, Identity: identity, Request: protocol.GoalFormulationRequest{GoalID: protocol.ID(uuid.NewString()), ExpectedCurrent: current, Start: true, TailMessages: window}}
			if name == "/goal" {
				if current == nil {
					return nativeControlResult{err: errors.New("no goal to resume")}
				}
				method, params = "goals.resume", protocol.ResumeGoalParams{SessionID: owner, Identity: identity, Goal: *current}
			}
			command, err := m.connection.PrepareInput(method, params)
			return nativeControlResult{input: command, err: err}
		})
	}
	if args == "clear" {
		var params *protocol.GoalParams
		return m.control("Cancel goal", true, func(ctx context.Context) nativeControlResult {
			if params == nil {
				goal, err := m.currentGoal(ctx, owner)
				if err != nil || goal == nil {
					return nativeControlResult{label: "No current goal", err: err}
				}
				params = &protocol.GoalParams{SessionID: owner, GoalID: goal.ID}
			}
			var changed protocol.GoalChange
			err := m.connection.Call(ctx, "goals.cancel", *params, &changed)
			if err == nil && (changed.Goal.SessionID != owner || changed.Goal.ID != params.GoalID) {
				err = errors.New("cancelled goal ownership mismatch")
			}
			return nativeControlResult{notice: nativeGoalText(&changed.Goal), err: err}
		})
	}
	params := protocol.CreateGoalParams{SessionID: owner, GoalID: protocol.ID(uuid.NewString()), Spec: protocol.GoalRequest{Text: args}, Start: true}
	prepared := false
	return m.control("Start goal", true, func(ctx context.Context) nativeControlResult {
		if !prepared {
			goal, err := m.currentGoal(ctx, owner)
			if err != nil {
				return nativeControlResult{err: err}
			}
			if goal != nil {
				params.ExpectedCurrent = new(goal.GoalRef)
			}
			prepared = true // Freeze the original goal CAS before the first effect.
		}
		var value protocol.GoalAdmission
		err := m.connection.Call(ctx, "goals.create", params, &value)
		if err == nil && (value.ID != params.GoalID || value.Goal != nil && (value.Goal.SessionID != owner || value.Goal.ID != params.GoalID)) {
			err = errors.New("created goal ownership mismatch")
		}
		text := nativeGoalText(value.Goal)
		if value.DeletedAt != nil {
			text = "The original goal was deleted; it was not recreated."
		} else if !value.Current && value.Goal != nil {
			text += "\nThis original goal has been superseded; it was not made current again."
		}
		return nativeControlResult{notice: text, err: err}
	})
}

func (m *nativeModel) nativeAdmissionAvailable() bool {
	if m.uncertain != nil || m.rejected != nil || m.redraft != nil || m.retryControl != nil || m.standingDraft != nil || m.sending || m.controlling {
		m.status = "Resolve the original pending input, staged redraft, control, or standing draft before another action."
		return false
	}
	return true
}

func (m *nativeModel) scheduleCommand(args string) tea.Cmd {
	owner := m.owner.ID
	fields := strings.Fields(args)
	if len(fields) == 0 || fields[0] == "list" && len(fields) <= 2 {
		params := protocol.ListSchedulesParams{SessionID: owner, Limit: 100}
		if len(fields) == 2 {
			params.After = protocol.ID(fields[1])
		}
		return m.control("Session schedules", false, func(ctx context.Context) nativeControlResult {
			var page protocol.SchedulesResult
			if err := m.connection.Call(ctx, "schedules.list", params, &page); err != nil {
				return nativeControlResult{err: err}
			}
			lines := []string{"Schedules for " + string(owner)}
			if len(page.Items) > params.Limit {
				return nativeControlResult{err: errors.New("schedule page exceeds requested limit")}
			}
			for _, item := range page.Items {
				if item.SessionID != owner {
					return nativeControlResult{err: errors.New("schedule ownership mismatch")}
				}
				state := "no upcoming occurrence"
				if item.NextDue != nil {
					state = "next " + *item.NextDue
				}
				if item.CancelledAt != nil {
					state = "cancelled"
				}
				if item.Failure != nil {
					state += " · " + *item.Failure
				}
				lines = append(lines, fmt.Sprintf("%s · %s · %s\n%s", item.ID, item.Expression, state, item.Preview))
			}
			if page.NextAfter != nil {
				lines = append(lines, "Next page: /schedule list "+string(*page.NextAfter))
			}
			return nativeControlResult{notice: strings.Join(lines, "\n")}
		})
	}
	if !m.nativeAdmissionAvailable() {
		return nil
	}
	method := "schedules.create"
	id := protocol.ID(uuid.NewString())
	var params any
	if len(fields) == 2 && fields[0] == "cancel" {
		method, id = "schedules.cancel", protocol.ID(fields[1])
		params = protocol.ScheduleParams{SessionID: owner, ScheduleID: id}
	} else if len(fields) >= 3 && (fields[0] == "@at" || fields[0] == "@every") {
		text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(args, fields[0])), fields[1]))
		params = protocol.CreateScheduleParams{SessionID: owner, ScheduleID: id, Expression: fields[0] + " " + fields[1], Parts: []protocol.Part{{Type: "text", Text: text}}}
	} else {
		m.status = "usage: /schedule @every <duration>|@at <RFC3339 time> <prompt> | list [after-id] | cancel <id>"
		return nil
	}
	return m.control("Update schedule", true, func(ctx context.Context) nativeControlResult {
		var value protocol.ScheduleAdmission
		err := m.connection.Call(ctx, method, params, &value)
		if err == nil && (value.ID != id || value.Schedule != nil && (value.Schedule.ID != id || value.Schedule.SessionID != owner)) {
			err = errors.New("schedule admission ownership mismatch")
		}
		text := "Schedule " + string(id)
		if value.DeletedAt != nil {
			text += " was deleted; it was not recreated."
		} else if value.Schedule != nil && value.Schedule.CancelledAt != nil {
			text += " cancelled."
		} else {
			text += " accepted. /schedule list reads current timing and outcome."
		}
		return nativeControlResult{notice: text, err: err}
	})
}
