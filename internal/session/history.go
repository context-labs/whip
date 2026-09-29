package session

import "time"

type (
	HistoryGroupID string
	HistoryEditID  string
)

// Imported history has local identities and immutable source provenance. Source
// identities grant no access and survive deletion of the original conversation.
type HistoryGroupSource struct {
	SessionID SessionID      `json:"session_id"`
	GroupID   HistoryGroupID `json:"group_id"`
}

type MessageSource struct {
	SessionID SessionID `json:"session_id"`
	MessageID MessageID `json:"message_id"`
	Sequence  int64     `json:"sequence,string"`
}

// A native group belongs to one prompt turn. Imported groups have no local turn;
// their copied messages and opening-input marker need no synthetic input.
type HistoryGroup struct {
	ID        HistoryGroupID
	SessionID SessionID
	TurnID    *TurnID
	Source    *HistoryGroupSource
	CreatedAt time.Time
}

// HistoryEdit records an immutable change to the current transcript. Retired
// messages retain exact evidence and sequences; their bodies never change.
type HistoryEdit struct {
	ID               HistoryEditID
	SessionID        SessionID
	Digest           string
	ExpectedRevision Revision
	Revision         Revision
	ObservedThrough  int64
	KeepThrough      int64
	CreatedAt        time.Time
}

const (
	MaxHistoryReadBytes  = 64 << 10
	MaxHistoryQueryBytes = 256
)

// HistorySnapshot fixes the raw transcript boundary for subsequent reads.
type HistorySnapshot struct {
	Revision        Revision  `json:"revision,string"`
	SessionID       SessionID `json:"session_id"`
	ThroughSequence int64     `json:"through_sequence,string"`
	MessageCount    int64     `json:"message_count,string"`
}

type HistoryMetadata struct {
	InputIdentity   *RequestIdentity `json:"input_identity"`
	GroupID         HistoryGroupID   `json:"group_id"`
	OpeningInput    bool             `json:"opening_input"`
	Source          *MessageSource   `json:"source"`
	RetiredBy       *HistoryEditID   `json:"retired_by"`
	RetiredRevision *Revision        `json:"retired_revision,string"`
	ID              MessageID        `json:"id"`
	SessionID       SessionID        `json:"session_id"`
	TurnID          TurnID           `json:"turn_id"`
	InputID         *InputID         `json:"input_id"`
	Mail            *MailRef         `json:"mail"`
	Sequence        int64            `json:"sequence,string"`
	Role            Role             `json:"role"`
	PartsBytes      int64            `json:"parts_bytes,string"`
}

type HistoryMetadataPage struct {
	Revision        Revision          `json:"revision,string"`
	Items           []HistoryMetadata `json:"items"`
	ThroughSequence int64             `json:"through_sequence,string"`
	NextAfter       *int64            `json:"next_after,string"`
}

// HistoryRead pages the exact serialized parts. Byte ranges may split UTF-8 or
// JSON tokens; callers concatenate decoded base64 bytes before interpreting them.
type HistoryRead struct {
	Message    HistoryMetadata `json:"message"`
	Offset     int64           `json:"offset,string"`
	NextOffset *int64          `json:"next_offset,string"`
	Data       []byte          `json:"data_base64"`
}

type HistoryMatch struct {
	Message   HistoryMetadata `json:"message"`
	PartIndex int             `json:"part_index"`
	Field     string          `json:"field"`
	Offset    int64           `json:"offset,string"`
	Snippet   string          `json:"snippet"`
	Truncated bool            `json:"truncated"`
}

// NextAfter is the last searched sequence, even when Matches is empty. A nil
// cursor means the fixed snapshot has been completely searched.
type HistorySearchPage struct {
	Revision        Revision       `json:"revision,string"`
	Matches         []HistoryMatch `json:"matches"`
	ThroughSequence int64          `json:"through_sequence,string"`
	NextAfter       *int64         `json:"next_after,string"`
	ScannedMessages int64          `json:"scanned_messages,string"`
	ScannedBytes    int64          `json:"scanned_bytes,string"`
}
