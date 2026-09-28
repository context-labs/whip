package session

import (
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/schedule"
)

type ScheduleID string

type ScheduleSpec struct {
	Expression string `json:"expression"`
	Parts      []Part `json:"parts"`
}

func (s ScheduleSpec) Validate() error {
	if _, err := schedule.Parse(s.Expression); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return ValidateInputParts(s.Parts)
}

type ScheduleOccurrence struct {
	ScheduleID   ScheduleID `json:"schedule_id"`
	ScheduledFor time.Time  `json:"scheduled_for"`
}

// ScheduleInput projects immutable input provenance, never another execution state.
type ScheduleInput struct {
	ScheduleOccurrence
	InputID   InputID `json:"input_id"`
	ClientID  string  `json:"client_id"`
	RequestID string  `json:"request_id"`
}

type ScheduleMetadata struct {
	ID               ScheduleID     `json:"id"`
	SessionID        SessionID      `json:"session_id"`
	Expression       string         `json:"expression"`
	FirstDue         time.Time      `json:"first_due"`
	NextDue          *time.Time     `json:"next_due"`
	CancelledAt      *time.Time     `json:"cancelled_at"`
	Failure          *string        `json:"failure"`
	CreatedAt        time.Time      `json:"created_at"`
	PartsBytes       int64          `json:"parts_bytes,string"`
	Preview          string         `json:"preview"`
	PreviewTruncated bool           `json:"preview_truncated"`
	Latest           *ScheduleInput `json:"latest"`
}

type Schedule struct {
	ScheduleMetadata
	Parts []Part `json:"parts"`
}

type ScheduleAdmission struct {
	ID        ScheduleID        `json:"id"`
	Schedule  *ScheduleMetadata `json:"schedule"`
	DeletedAt *time.Time        `json:"deleted_at"`
}

type ScheduleCursor struct {
	Due time.Time  `json:"due"`
	ID  ScheduleID `json:"id"`
}

// A plain list uses After. Upcoming uses the exact (due,id) continuation instead.
type ScheduleList struct {
	After    ScheduleID      `json:"after"`
	Upcoming bool            `json:"upcoming"`
	Cursor   *ScheduleCursor `json:"cursor"`
	Limit    int             `json:"limit"`
}

type SchedulePage struct {
	Items      []ScheduleMetadata `json:"items"`
	NextAfter  *ScheduleID        `json:"next_after"`
	NextCursor *ScheduleCursor    `json:"next_cursor"`
}

type ScheduleIDRequest struct {
	ID ScheduleID `json:"id"`
}
