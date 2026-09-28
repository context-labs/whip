package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) CreateSchedule(ctx context.Context, owner session.SessionID, id session.ScheduleID, spec session.ScheduleSpec) (session.ScheduleAdmission, error) {
	if err := r.Err(); err != nil {
		return session.ScheduleAdmission{}, err
	}
	result, err := r.store.CreateSchedule(ctx, owner, id, spec)
	if err == nil {
		r.Wake()
	}
	return result, err
}

func (r *Runtime) CancelSchedule(ctx context.Context, owner session.SessionID, id session.ScheduleID) (session.ScheduleAdmission, error) {
	result, err := r.store.CancelSchedule(ctx, owner, id)
	if err == nil {
		r.Wake()
	}
	return result, err
}

func (r *Runtime) Schedule(ctx context.Context, owner session.SessionID, id session.ScheduleID) (session.Schedule, error) {
	return r.store.Schedule(ctx, owner, id)
}

func (r *Runtime) Schedules(ctx context.Context, owner session.SessionID, request session.ScheduleList) (session.SchedulePage, error) {
	return r.store.Schedules(ctx, owner, request)
}

// Admission is independent of runnable workers. Advance the advisory cursor even
// when an earlier owner is blocked, then wrap after each bounded pass.
type scheduleScan struct {
	After   *session.ScheduleCursor
	Through *int64
}

func (r *Runtime) admitSchedules(ctx context.Context, scan scheduleScan) (scheduleScan, error) {
	if scan.Through == nil {
		through, err := r.store.ScheduleSweep(ctx)
		if err != nil {
			return scan, err
		}
		scan.Through = &through
	}
	const pageSize = 8
	values, err := r.store.DueSchedules(ctx, scan.After, *scan.Through, pageSize)
	if err != nil {
		return scan, err
	}
	next := scan
	for _, value := range values {
		next.After = &session.ScheduleCursor{Due: *value.NextDue, ID: value.ID}
		if _, err := r.store.FireSchedule(ctx, value.ID, *value.NextDue); err != nil {
			if errors.Is(err, store.ErrBusy) || errors.Is(err, store.ErrLimit) || errors.Is(err, store.ErrStopped) || errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrConflict) || errors.Is(err, store.ErrNoWork) || errors.Is(err, store.ErrScheduleBlocked) {
				continue
			}
			return next, err
		}
	}
	if len(values) < pageSize {
		next = scheduleScan{}
	}
	return next, nil
}

func (r *Runtime) prepareSchedule(current session.Session, call tool.Invocation) (tool.Prepared, error) {
	var request any
	switch call.Name {
	case "create":
		request = &session.ScheduleSpec{}
	case "list":
		request = &session.ScheduleList{Limit: 20}
	case "cancel":
		request = &session.ScheduleIDRequest{}
	default:
		return tool.Prepared{}, fmt.Errorf("%w: unsupported schedule operation", session.ErrInvalid)
	}
	if err := decodeArguments(call.Arguments, request); err != nil {
		return tool.Prepared{}, err
	}
	if spec, ok := request.(*session.ScheduleSpec); ok {
		if err := spec.Validate(); err != nil {
			return tool.Prepared{}, err
		}
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{Capability: "schedules." + call.Name, Resource: string(current.TreeID), Arguments: arguments, Apply: func(ctx context.Context, id session.OperationID) (any, error) {
		result, err := r.store.ApplyScheduleOperation(ctx, id)
		if err == nil {
			r.Wake()
		}
		return result, err
	}}, nil
}
