package session

import (
	"fmt"
	"time"
	"unicode/utf8"
)

type CompactionID string

const (
	MaxCompactionBytes = 64 << 10
	MaxCompactionPins  = 32
)

// CompactionPolicy selects the helper route and proactive context threshold.
// A nil model uses the turn's captured conversation model. Threshold zero asks
// Resolve to capture the default of 50 percent; explicit values are 1 to 100.
type CompactionPolicy struct {
	Model            *ModelSelection `json:"model"`
	ThresholdPercent int             `json:"threshold_percent"`
}

func (p CompactionPolicy) Validate() error {
	if p.ThresholdPercent < 0 || p.ThresholdPercent > 100 {
		return fmt.Errorf("%w: compaction threshold must be 1–100 percent or zero for the default", ErrInvalid)
	}
	if p.Model != nil {
		return p.Model.Validate()
	}
	return nil
}

// ContextHead selects a derived summary without modifying raw history. An
// existing session with no selection history has revision zero and a nil ID.
type ContextHead struct {
	SessionID    SessionID     `json:"session_id"`
	Revision     int64         `json:"revision,string"`
	CompactionID *CompactionID `json:"compaction_id"`
}

// CompactionMetadata identifies the exact raw prefix and pinned raw messages
// used by an immutable summary. TextBytes is derived from the stored text.
type CompactionMetadata struct {
	ID               CompactionID   `json:"id"`
	SessionID        SessionID      `json:"session_id"`
	TurnID           TurnID         `json:"turn_id"`
	AttemptID        ModelAttemptID `json:"attempt_id"`
	BaseID           *CompactionID  `json:"base_id"`
	ExpectedRevision int64          `json:"expected_revision,string"`
	ThroughSequence  int64          `json:"through_sequence,string"`
	PinnedMessageIDs []MessageID    `json:"pinned_message_ids"`
	TextBytes        int64          `json:"text_bytes,string"`
	CreatedAt        time.Time      `json:"created_at"`
}

type Compaction struct {
	CompactionMetadata
	Text string `json:"text"`
}

// CompactionSettlement reports current selection, not whether an earlier call
// selected the summary. A semantic rejection never discards settled billing.
type CompactionSettlement struct {
	Attempt    ModelAttempt
	Compaction *Compaction
	Head       ContextHead
	Selected   bool
	Rejection  *string
}

// CompactionDraft freezes the candidate's source selection before dispatch.
// Its owner, turn and attempt are derived from the accounting ledger.
type CompactionDraft struct {
	ID               CompactionID
	ExpectedRevision int64
	BaseID           *CompactionID
	ThroughSequence  int64
	PinnedMessageIDs []MessageID
	Text             string
}

func (d CompactionDraft) Validate() error {
	if err := ValidateID(string(d.ID)); err != nil {
		return err
	}
	if d.ExpectedRevision < 0 || d.ThroughSequence < 1 || len(d.PinnedMessageIDs) > MaxCompactionPins {
		return fmt.Errorf("%w: invalid compaction boundary or pin count", ErrInvalid)
	}
	if d.BaseID != nil {
		if err := ValidateID(string(*d.BaseID)); err != nil {
			return err
		}
		if *d.BaseID == d.ID || d.ExpectedRevision == 0 {
			return fmt.Errorf("%w: invalid compaction base", ErrInvalid)
		}
	}
	seen := make(map[MessageID]bool, len(d.PinnedMessageIDs))
	for _, id := range d.PinnedMessageIDs {
		if err := ValidateID(string(id)); err != nil {
			return err
		}
		if seen[id] {
			return fmt.Errorf("%w: duplicate compaction pin", ErrInvalid)
		}
		seen[id] = true
	}
	if !utf8.ValidString(d.Text) {
		return fmt.Errorf("%w: compaction text must be UTF-8", ErrInvalid)
	}
	return ValidateText(d.Text, MaxCompactionBytes)
}
