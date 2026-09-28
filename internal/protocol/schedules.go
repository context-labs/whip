package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type ScheduleOccurrence struct {
	ScheduleID   ID     `json:"schedule_id"`
	ScheduledFor string `json:"scheduled_for" format:"date-time"`
}
type ScheduleInput struct {
	ScheduleOccurrence
	InputID  ID              `json:"input_id"`
	Identity RequestIdentity `json:"identity"`
}
type ScheduleMetadata struct {
	ID               ID             `json:"id"`
	SessionID        ID             `json:"session_id"`
	Expression       string         `json:"expression"`
	FirstDue         string         `json:"first_due" format:"date-time"`
	NextDue          *string        `json:"next_due"`
	CancelledAt      *string        `json:"cancelled_at"`
	Failure          *string        `json:"failure"`
	CreatedAt        string         `json:"created_at" format:"date-time"`
	PartsBytes       Counter        `json:"parts_bytes"`
	Preview          string         `json:"preview"`
	PreviewTruncated bool           `json:"preview_truncated"`
	Latest           *ScheduleInput `json:"latest"`
}
type ScheduleCursor struct {
	Due string `json:"due" pattern:"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}[.][0-9]{9}Z$"`
	ID  ID     `json:"id"`
}
type CreateScheduleParams struct {
	SessionID  ID     `json:"session_id"`
	ScheduleID ID     `json:"schedule_id"`
	Expression string `json:"expression"`
	Parts      []Part `json:"parts"`
}
type ScheduleParams struct {
	SessionID  ID `json:"session_id"`
	ScheduleID ID `json:"schedule_id"`
}
type ListSchedulesParams struct {
	SessionID ID              `json:"session_id"`
	After     ID              `json:"after,omitempty"`
	Upcoming  bool            `json:"upcoming,omitempty"`
	Cursor    *ScheduleCursor `json:"cursor,omitempty"`
	Limit     int             `json:"limit" min:"1" max:"100"`
}
type SchedulesResult struct {
	Items      []ScheduleMetadata `json:"items"`
	NextAfter  *ID                `json:"next_after"`
	NextCursor *ScheduleCursor    `json:"next_cursor"`
}
type ScheduleAdmission struct {
	ID        ID                `json:"id"`
	Schedule  *ScheduleMetadata `json:"schedule"`
	DeletedAt *string           `json:"deleted_at"`
}
type ScheduleResult struct {
	Schedule ScheduleMetadata `json:"schedule"`
	Parts    []Part           `json:"parts"`
}

func scheduleTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func ScheduleFromDomain(value session.ScheduleMetadata) ScheduleMetadata {
	result := ScheduleMetadata{ID: ID(value.ID), SessionID: ID(value.SessionID), Expression: value.Expression, FirstDue: scheduleTime(value.FirstDue), CancelledAt: timeString(value.CancelledAt), Failure: value.Failure, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), PartsBytes: Counter(value.PartsBytes), Preview: value.Preview, PreviewTruncated: value.PreviewTruncated}
	if value.NextDue != nil {
		result.NextDue = new(scheduleTime(*value.NextDue))
	}
	if value.Latest != nil {
		result.Latest = &ScheduleInput{ScheduleID: ID(value.Latest.ScheduleID), ScheduledFor: scheduleTime(value.Latest.ScheduledFor), InputID: ID(value.Latest.InputID), Identity: RequestIdentity{ClientID: ID(value.Latest.ClientID), RequestID: ID(value.Latest.RequestID)}}
	}
	return result
}

func ScheduleAdmissionFromDomain(value session.ScheduleAdmission) ScheduleAdmission {
	result := ScheduleAdmission{ID: ID(value.ID), DeletedAt: timeString(value.DeletedAt)}
	if value.Schedule != nil {
		result.Schedule = new(ScheduleFromDomain(*value.Schedule))
	}
	return result
}

func SchedulesFromDomain(value session.SchedulePage) SchedulesResult {
	result := SchedulesResult{Items: make([]ScheduleMetadata, len(value.Items))}
	for i, item := range value.Items {
		result.Items[i] = ScheduleFromDomain(item)
	}
	if value.NextAfter != nil {
		result.NextAfter = new(ID(*value.NextAfter))
	}
	if value.NextCursor != nil {
		result.NextCursor = &ScheduleCursor{ID: ID(value.NextCursor.ID), Due: scheduleTime(value.NextCursor.Due)}
	}
	return result
}
