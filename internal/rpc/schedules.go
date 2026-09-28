package rpc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchSchedule(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "schedules.create":
		return decode(raw, func(p protocol.CreateScheduleParams) (any, error) {
			parts := make([]session.Part, len(p.Parts))
			for i, part := range p.Parts {
				parts[i] = part.Domain()
			}
			value, err := r.CreateSchedule(ctx, session.SessionID(p.SessionID), session.ScheduleID(p.ScheduleID), session.ScheduleSpec{Expression: p.Expression, Parts: parts})
			return protocol.ScheduleAdmissionFromDomain(value), err
		})
	case "schedules.get":
		return decode(raw, func(p protocol.ScheduleParams) (any, error) {
			value, err := r.Schedule(ctx, session.SessionID(p.SessionID), session.ScheduleID(p.ScheduleID))
			if err != nil {
				return nil, err
			}
			parts := make([]protocol.Part, len(value.Parts))
			for i, part := range value.Parts {
				parts[i] = protocol.PartFromDomain(part)
			}
			return protocol.ScheduleResult{Schedule: protocol.ScheduleFromDomain(value.ScheduleMetadata), Parts: parts}, nil
		})
	case "schedules.cancel":
		return decode(raw, func(p protocol.ScheduleParams) (any, error) {
			value, err := r.CancelSchedule(ctx, session.SessionID(p.SessionID), session.ScheduleID(p.ScheduleID))
			return protocol.ScheduleAdmissionFromDomain(value), err
		})
	case "schedules.list":
		return decode(raw, func(p protocol.ListSchedulesParams) (any, error) {
			request := session.ScheduleList{After: session.ScheduleID(p.After), Upcoming: p.Upcoming, Limit: p.Limit}
			if p.Cursor != nil {
				due, err := time.Parse(time.RFC3339Nano, p.Cursor.Due)
				if err != nil || due.UTC().Format("2006-01-02T15:04:05.000000000Z") != p.Cursor.Due {
					return nil, session.ErrInvalid
				}
				request.Cursor = &session.ScheduleCursor{Due: due, ID: session.ScheduleID(p.Cursor.ID)}
			}
			value, err := r.Schedules(ctx, session.SessionID(p.SessionID), request)
			return protocol.SchedulesFromDomain(value), err
		})
	default:
		return nil, ErrMethod
	}
}
