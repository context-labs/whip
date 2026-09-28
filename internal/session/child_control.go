package session

// Child controls use the caller's tree-scoped authority. Mutation targets are
// proper descendants; submission is restricted further to direct children.
type ChildTarget struct {
	SessionID SessionID `json:"session_id"`
}

type ChildSubmit struct {
	SessionID SessionID `json:"session_id"`
	Parts     []Part    `json:"parts"`
}

type ChildInspect struct {
	SessionID SessionID `json:"session_id"`
	InputID   InputID   `json:"input_id"`
	Offset    int64     `json:"offset,string,omitempty"`
}

type ChildList struct {
	Relation string    `json:"relation"`
	After    SessionID `json:"after,omitempty"`
	Limit    int       `json:"limit,omitempty"`
}

// RelativeMetadata exposes coordination metadata, never another session's
// configuration, working directory, transcript, or content reference identities.
type RelativeMetadata struct {
	SessionID SessionID  `json:"session_id"`
	ParentID  *SessionID `json:"parent_id"`
	Lifecycle Lifecycle  `json:"lifecycle"`
}

// ChildOutcome is a bounded snapshot of one exact input. A queued input has no
// turn; an input with a terminal turn retains input_state=claimed.
type ChildOutcome struct {
	SessionID    SessionID  `json:"session_id"`
	InputID      InputID    `json:"input_id"`
	InputState   InputState `json:"input_state"`
	TurnID       *TurnID    `json:"turn_id"`
	TurnState    *TurnState `json:"turn_state"`
	Failure      *string    `json:"failure"`
	MessageID    *MessageID `json:"message_id"`
	Offset       int64      `json:"offset,string"`
	NextOffset   *int64     `json:"next_offset,string"`
	TotalBytes   int64      `json:"total_bytes,string"`
	Text         string     `json:"text"`
	Truncated    bool       `json:"truncated"`
	OmittedParts int64      `json:"omitted_parts,string"`
}

type ChildSubmission struct {
	SessionID SessionID `json:"session_id"`
	InputID   InputID   `json:"input_id"`
}

type ChildStopResult struct {
	SessionID       SessionID `json:"session_id"`
	StoppedSessions int64     `json:"stopped_sessions,string"`
	CancellingTurns int64     `json:"cancelling_turns,string"`
}
