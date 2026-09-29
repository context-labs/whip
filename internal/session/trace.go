package session

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// TraceAttribute preserves exact integer quantities separately from display text.
// Exactly one value is present. Bodies are bounded previews, never replay input.
type TraceAttribute struct {
	Key   string
	Text  *string
	Count *int64
	Flag  *bool
}

type TraceSpan struct {
	TraceID      string
	ParentSpanID *string
	Kind         string
	Name         string
	State        string
	StartNS      int64
	EndNS        *int64
	Attributes   []TraceAttribute
}

// A nil Span is a deletion tombstone. Source identities are retained even when
// their canonical rows are gone. Sequence denotes the latest source change.
type TraceRow struct {
	Sequence   int64
	RootID     SessionID
	SessionID  SessionID
	TurnID     TurnID
	SourceKind string
	SourceID   string
	SpanID     string
	Span       *TraceSpan
}

type TraceQuery struct {
	RootID           SessionID
	After            int64
	Backward         bool
	Before           *int64
	ExpectedRevision *int64
	TraceID          string
	RootsOnly        bool
	Limit            int
	MaxBytes         int
}

type TracePage struct {
	ObservedAtNS int64
	Items        []TraceRow
	Revision     int64
	Next         int64
	HasMore      bool
}

func TraceSpanID(kind, source string) string {
	digest := sha256.Sum256([]byte("whip.trace.span.v1\x00" + kind + "\x00" + source))
	return hex.EncodeToString(digest[:8])
}

func TraceID(turn TurnID) string {
	digest := sha256.Sum256([]byte("whip.trace.v1\x00" + string(turn)))
	return hex.EncodeToString(digest[:16])
}

func (q TraceQuery) Validate() error {
	if err := ValidateID(string(q.RootID)); err != nil {
		return err
	}
	if q.Backward && q.After != 0 || q.Before != nil && (!q.Backward || *q.Before < 0) || q.After < 0 || q.ExpectedRevision != nil && *q.ExpectedRevision < q.After || q.Limit < 1 || q.Limit > 2048 || q.MaxBytes < 4096 || q.MaxBytes > 512<<10 {
		return fmt.Errorf("%w: invalid trace page bounds", ErrInvalid)
	}
	if q.TraceID != "" {
		raw, err := hex.DecodeString(q.TraceID)
		if err != nil || len(raw) != 16 || hex.EncodeToString(raw) != q.TraceID {
			return fmt.Errorf("%w: invalid trace ID", ErrInvalid)
		}
	}
	return nil
}
