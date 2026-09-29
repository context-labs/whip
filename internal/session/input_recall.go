package session

// InputText is human text for editor recall, not a replayable input payload.
// It carries no attachment, design context, receipt or execution authority.
type InputText struct {
	SessionID SessionID
	InputID   InputID
	Ordinal   int64
	Text      string
}

type InputTextPage struct {
	Items        []InputText
	NextCursor   *int64
	ScannedCount int
	SkippedCount int // Oversized eligible inputs; text is never silently truncated.
}

const MaxInputRecallBytes = 256 << 10
