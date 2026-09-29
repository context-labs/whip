package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

const maxPreviewBytes = model.MaxPreviewBytes

// Preview contains provisional provider text and reasoning, not a Message.
// Partial tool arguments may be invalid JSON and must never be executed.
type Preview struct {
	historyRevision session.Revision
	AttemptID       session.ModelAttemptID
	TurnID          session.TurnID
	MessageID       session.MessageID
	Revision        int64
	Text            string
	Reasoning       string
	Calls           []CallPreview
	Truncated       bool
	Presentation    *session.MessagePresentation
}
type CallPreview struct {
	Index               int
	ID, Name, Arguments string
}
type Observation struct {
	AttemptPresentations          []session.AttemptPresentation
	AttemptPresentationsTruncated bool
	Snapshot                      session.HistorySnapshot
	Epoch                         string
	Messages                      []session.Message
	Preview                       *Preview
}
type livePreview struct {
	preview     Preview
	accumulator *model.PresentationAccumulator
}

// BeginPreview's callbacks live only for the dispatched attempt. Late callbacks
// cannot overwrite another attempt. Capacity is bounded separately from history.
func (r *Runtime) BeginPreview(turn session.Turn, id session.ModelAttemptID, messageID session.MessageID, accumulator *model.PresentationAccumulator) (func(model.Chunk), func()) {
	live := &livePreview{preview: Preview{historyRevision: turn.HistoryRevision, AttemptID: id, TurnID: turn.ID, MessageID: messageID}, accumulator: accumulator}
	r.previewMu.Lock()
	if len(r.previews) >= 64 {
		r.previewMu.Unlock()
		return accumulator.Append, func() {}
	}
	r.previews[turn.SessionID] = live
	r.previewMu.Unlock()
	emit := func(chunk model.Chunk) {
		r.previewMu.Lock()
		defer r.previewMu.Unlock()
		if r.previews[turn.SessionID] != live {
			return
		}
		if chunk.Text == "" && chunk.Reasoning == "" && chunk.Call == nil {
			return
		}
		live.preview.Revision++
		accumulator.Append(chunk)
	}
	end := func() {
		r.previewMu.Lock()
		defer r.previewMu.Unlock()
		if r.previews[turn.SessionID] == live {
			delete(r.previews, turn.SessionID)
		}
	}
	return emit, end
}

func (r *Runtime) preview(id session.SessionID) *Preview {
	r.previewMu.Lock()
	defer r.previewMu.Unlock()
	live := r.previews[id]
	if live == nil {
		return nil
	}
	result := live.preview
	snapshot := live.accumulator.Snapshot()
	result.Text = snapshot.Text
	result.Reasoning = snapshot.Reasoning
	result.Truncated = snapshot.Truncated
	result.Presentation = snapshot.Presentation
	result.Calls = []CallPreview{}
	for _, call := range snapshot.Calls {
		result.Calls = append(result.Calls, CallPreview{Index: call.Index, ID: call.ID, Name: call.Name, Arguments: call.Arguments})
	}
	return &result
}

// Observe reads live state before durable state. A settled attempt hides its
// preview before history is read; a concurrent settlement is also reconciled by
// message ID. SQL never waits on the presentation mutex or on an observer.
func (r *Runtime) Observe(ctx context.Context, id session.SessionID, after int64, limit int) (Observation, error) {
	return r.ObserveRevision(ctx, id, after, limit, nil)
}

// ObserveRevision rejects cursors from retired history. Preview state is still
// provisional; its attempt is validated before the atomic durable page read.
func (r *Runtime) ObserveRevision(ctx context.Context, id session.SessionID, after int64, limit int, expected *session.Revision) (Observation, error) {
	preview := r.preview(id)
	if preview != nil {
		attempt, err := r.store.ModelAttempt(ctx, preview.AttemptID)
		if err != nil {
			return Observation{}, err
		}
		if attempt.State != session.AttemptDispatched {
			preview = nil
		}
	}
	snapshot, messages, err := r.store.HistoryPage(ctx, id, after, limit, expected)
	if err != nil {
		return Observation{}, err
	}
	if preview != nil && preview.historyRevision != snapshot.Revision {
		preview = nil
	}
	if preview != nil {
		for _, message := range messages {
			if message.ID == preview.MessageID {
				preview = nil
				break
			}
		}
	}
	through := snapshot.ThroughSequence
	if len(messages) > 0 {
		through = messages[len(messages)-1].Sequence
	}
	attempts, truncated, err := r.store.AttemptPresentations(ctx, id, after, through, snapshot.Revision)
	if err != nil {
		return Observation{}, err
	}
	if preview != nil {
		for _, attempt := range attempts {
			if attempt.AttemptID == preview.AttemptID {
				preview = nil
				break
			}
		}
	}
	return Observation{Snapshot: snapshot, Epoch: r.epoch, Messages: messages, Preview: preview, AttemptPresentations: attempts, AttemptPresentationsTruncated: truncated}, nil
}
