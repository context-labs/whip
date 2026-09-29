package session

import "time"

// Activity is a read projection of durable work, independent of live previews
// and loaded workers. A running turn without a permit is still unfinished work.
type Activity struct {
	SessionID               SessionID
	Lifecycle               Lifecycle
	ActiveTurn              *Turn
	ActiveInputID           *InputID
	QueuedInputCount        int64
	PendingPermissionCount  int64
	PendingQuestionCount    int64
	ExecutionPermit         bool
	ActiveWorkspaceActionID *WorkspaceActionID
}

// InputSummary keeps queue discovery separate from explicit payload reads.
type InputSummary struct {
	// Identity is the immutable admission receipt, absent for inputs without one.
	Identity         *RequestIdentity
	Steering         *InputSteeringRef
	ID               InputID
	SessionID        SessionID
	Ordinal          int64
	Source           InputSource
	Kind             InputKind
	State            InputState
	TurnID           *TurnID
	CreatedAt        time.Time
	TextPreview      string
	PreviewTruncated bool
	AttachmentCount  int64
}

type InputPage struct {
	Items      []InputSummary
	NextCursor *int64
}

// TurnPage exposes canonical execution metadata, including direct work that has
// no transcript. Items are newest first, ordered by immutable start time and ID.
type TurnPage struct {
	Items      []Turn
	NextCursor *TurnID
}
