package client

import (
	"context"
	"errors"
	"slices"
	"sync"

	"github.com/context-labs/whip/internal/protocol"
)

// ObservationCursor belongs to one session history revision. Epoch identifies
// provisional process state only; a restart does not erase durable history.
type ObservationCursor struct {
	After    protocol.Counter
	Revision *protocol.Counter
	Epoch    protocol.ID
}
type Observation struct {
	protocol.SessionObservation
	Reset  bool
	Cursor ObservationCursor
}

// Observer is pull based. Next performs one bounded page read (two when a
// rewind invalidates the prior revision), and owns no goroutine or history cache.
// Concurrent calls fail rather than queueing unbounded consumer work.
type Observer struct {
	session   *Session
	mu        sync.Mutex
	cursor    ObservationCursor
	reset     bool
	committed []protocol.ID
}

func (s *Session) Observer(cursor ObservationCursor) (*Observer, error) {
	if cursor.After < 0 || cursor.After != 0 && cursor.Revision == nil || cursor.Revision != nil && *cursor.Revision <= 0 {
		return nil, errors.New("resuming observation requires a valid history revision")
	}
	if cursor.Revision != nil {
		cursor.Revision = new(*cursor.Revision)
	}
	return &Observer{session: s, cursor: cursor}, nil
}

func (o *Observer) Next(ctx context.Context) (Observation, error) {
	if !o.mu.TryLock() {
		return Observation{}, errors.New("observation read is already pending")
	}
	defer o.mu.Unlock()
	for range 2 {
		var value protocol.SessionObservation
		err := o.session.client.Call(ctx, "sessions.observe", protocol.HistoryParams{SessionID: o.session.id, After: o.cursor.After, ExpectedRevision: o.cursor.Revision, Limit: 100}, &value)
		var remote *Error
		if errors.As(err, &remote) && remote.Kind == "CONFLICT" && o.cursor.Revision != nil {
			o.cursor = ObservationCursor{}
			o.committed = nil
			o.reset = true
			continue
		}
		if err != nil {
			return Observation{}, err
		}
		if value.Snapshot.SessionID != o.session.id || o.cursor.Revision != nil && value.Snapshot.Revision != *o.cursor.Revision || len(value.Messages) > 100 || o.cursor.After > value.Snapshot.ThroughSequence {
			return Observation{}, errors.New("observation ownership or revision mismatch")
		}
		after := o.cursor.After
		for _, message := range value.Messages {
			if message.SessionID != o.session.id || message.Sequence <= after || message.Sequence > value.Snapshot.ThroughSequence {
				return Observation{}, errors.New("observation history cursor did not advance")
			}
			after = message.Sequence
		}
		epochChanged := o.cursor.Epoch != "" && o.cursor.Epoch != value.Epoch
		if epochChanged {
			o.committed = nil
		}
		for _, message := range value.Messages {
			o.committed = append(o.committed, message.ID)
		}
		if len(o.committed) > 256 {
			o.committed = append([]protocol.ID{}, o.committed[len(o.committed)-256:]...)
		}
		if value.Preview != nil && slices.Contains(o.committed, value.Preview.MessageID) {
			value.Preview = nil
		}

		o.cursor = ObservationCursor{After: after, Revision: new(value.Snapshot.Revision), Epoch: value.Epoch}
		result := Observation{SessionObservation: value, Reset: o.reset, Cursor: o.cursor}
		result.Cursor.Revision = new(*o.cursor.Revision)
		o.reset = false
		return result, nil
	}
	return Observation{}, errors.New("history revision changed repeatedly during observation")
}
