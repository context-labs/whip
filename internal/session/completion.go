package session

import "time"

// MaxCompletionSlots bounds live children plus deleted children whose latest
// completion is still awaiting publication, independently of mailbox capacity.
const MaxCompletionSlots = 128

// CompletionMetadata identifies the exact terminal outcome retained for a
// parent. It remains readable after the source child and its transcript vanish.
type CompletionMetadata struct {
	ParentID     SessionID  `json:"parent_id"`
	ChildID      SessionID  `json:"child_id"`
	TurnID       TurnID     `json:"turn_id"`
	InputID      *InputID   `json:"input_id"`
	MessageID    *MessageID `json:"message_id"`
	State        TurnState  `json:"state"`
	Failure      *string    `json:"failure"`
	Mode         ReportMode `json:"mode"`
	FinishedAt   time.Time  `json:"finished_at"`
	TextBytes    int64      `json:"text_bytes,string"`
	OmittedParts int64      `json:"omitted_parts,string"`
}

// Completion is the full text evidence for one completion. The text comes from
// its last assistant message; non-text parts are counted, never granted to the
// parent implicitly. Published evidence is this exact JSON representation.
type Completion struct {
	CompletionMetadata
	Text string `json:"text"`
}

// CompletionNotice is the bounded JSON body of canonical completion mail.
// Failure and Preview may be shortened; the mail's EvidenceRef owns the full Completion.
type CompletionNotice struct {
	CompletionMetadata
	Preview          string `json:"preview"`
	TextTruncated    bool   `json:"text_truncated"`
	FailureTruncated bool   `json:"failure_truncated"`
}
