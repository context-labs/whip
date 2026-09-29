package session

import "fmt"

type HistoryPageRequest struct {
	SessionID        SessionID
	Direction        string
	Cursor           *int64
	ExpectedRevision *Revision
	Limit            int
}

func (r HistoryPageRequest) Validate() error {
	if err := ValidateID(string(r.SessionID)); err != nil {
		return err
	}
	if r.Direction != "forward" && r.Direction != "backward" || r.Cursor != nil && *r.Cursor < 0 || r.ExpectedRevision != nil && *r.ExpectedRevision < 1 || r.Limit < 1 || r.Limit > 100 {
		return fmt.Errorf("%w: invalid history page bounds", ErrInvalid)
	}
	return nil
}

// TranscriptPage always presents messages in ascending order. NextCursor names
// the exclusive continuation in the requested direction, including sequence gaps.
type TranscriptPage struct {
	Snapshot   HistorySnapshot
	Messages   []Message
	NextCursor *int64
}
