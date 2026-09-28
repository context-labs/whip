package session

import "fmt"

// RewindRequest names one immutable history edit. ObservedThrough and
// ExpectedRevision must come from the same HistorySnapshot. KeepThrough is zero
// or the final active message in a whole terminal history group.
type RewindRequest struct {
	ID               HistoryEditID
	SessionID        SessionID
	ExpectedRevision Revision
	ObservedThrough  int64
	KeepThrough      int64
}

func (r RewindRequest) Validate() error {
	for _, id := range []string{string(r.ID), string(r.SessionID)} {
		if err := ValidateID(id); err != nil {
			return err
		}
	}
	if r.ExpectedRevision < 1 || r.ObservedThrough < 0 || r.KeepThrough < 0 || r.KeepThrough > r.ObservedThrough {
		return fmt.Errorf("%w: invalid rewind boundary", ErrInvalid)
	}
	return nil
}
