package session

const (
	MaxHistoryReadBytes  = 64 << 10
	MaxHistoryQueryBytes = 256
)

// HistorySnapshot fixes the raw transcript boundary for subsequent reads.
type HistorySnapshot struct {
	SessionID       SessionID `json:"session_id"`
	ThroughSequence int64     `json:"through_sequence,string"`
	MessageCount    int64     `json:"message_count,string"`
}

type HistoryMetadata struct {
	ID         MessageID `json:"id"`
	SessionID  SessionID `json:"session_id"`
	TurnID     TurnID    `json:"turn_id"`
	InputID    *InputID  `json:"input_id"`
	Mail       *MailRef  `json:"mail"`
	Sequence   int64     `json:"sequence,string"`
	Role       Role      `json:"role"`
	PartsBytes int64     `json:"parts_bytes,string"`
}

type HistoryMetadataPage struct {
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
	Matches         []HistoryMatch `json:"matches"`
	ThroughSequence int64          `json:"through_sequence,string"`
	NextAfter       *int64         `json:"next_after,string"`
	ScannedMessages int64          `json:"scanned_messages,string"`
	ScannedBytes    int64          `json:"scanned_bytes,string"`
}
