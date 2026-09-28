package runtime

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

const maxPreviewBytes = 128 << 10

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
}
type CallPreview struct {
	Index               int
	ID, Name, Arguments string
}
type Observation struct {
	Snapshot session.HistorySnapshot
	Epoch    string
	Messages []session.Message
	Preview  *Preview
}
type (
	callPreviewBuffer struct{ id, name, arguments strings.Builder }
	livePreview       struct {
		preview   Preview
		text      strings.Builder
		reasoning strings.Builder
		calls     map[int]*callPreviewBuffer
		bytes     int
	}
)

// BeginPreview's callbacks live only for the dispatched attempt. Late callbacks
// cannot overwrite another attempt. Capacity is bounded separately from history.
func (r *Runtime) BeginPreview(turn session.Turn, id session.ModelAttemptID, messageID session.MessageID) (func(model.Chunk), func()) {
	live := &livePreview{preview: Preview{historyRevision: turn.HistoryRevision, AttemptID: id, TurnID: turn.ID, MessageID: messageID}, calls: map[int]*callPreviewBuffer{}}
	r.previewMu.Lock()
	if len(r.previews) >= 64 {
		r.previewMu.Unlock()
		return func(model.Chunk) {}, func() {}
	}
	r.previews[turn.SessionID] = live
	r.previewMu.Unlock()
	emit := func(chunk model.Chunk) {
		r.previewMu.Lock()
		defer r.previewMu.Unlock()
		if r.previews[turn.SessionID] != live || live.preview.Truncated {
			return
		}
		if chunk.Text == "" && chunk.Reasoning == "" && chunk.Call == nil {
			return
		}
		live.preview.Revision++
		live.append(&live.text, chunk.Text, maxPreviewBytes)
		live.append(&live.reasoning, chunk.Reasoning, maxPreviewBytes)
		if chunk.Call == nil {
			return
		}
		c := chunk.Call
		if c.Index < 0 || c.Index >= session.MaxToolCalls {
			live.preview.Truncated = true
			return
		}
		buffer := live.calls[c.Index]
		if buffer == nil {
			buffer = &callPreviewBuffer{}
			live.calls[c.Index] = buffer
		}
		live.append(&buffer.id, c.ID, 128)
		live.append(&buffer.name, c.Name, 64)
		live.append(&buffer.arguments, c.Arguments, maxPreviewBytes)
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

func (p *livePreview) append(target *strings.Builder, value string, limit int) {
	if p.preview.Truncated {
		return
	}
	if !utf8.ValidString(value) {
		p.preview.Truncated = true
		return
	}
	count := min(len(value), maxPreviewBytes-p.bytes, limit-target.Len())
	if count < len(value) {
		p.preview.Truncated = true
		for count > 0 && !utf8.ValidString(value[:count]) {
			count--
		}
	}
	target.WriteString(value[:count])
	p.bytes += count
}

func (r *Runtime) preview(id session.SessionID) *Preview {
	r.previewMu.Lock()
	defer r.previewMu.Unlock()
	live := r.previews[id]
	if live == nil {
		return nil
	}
	result := live.preview
	result.Text = strings.Clone(live.text.String())
	result.Reasoning = strings.Clone(live.reasoning.String())
	result.Calls = []CallPreview{}
	for index := range session.MaxToolCalls {
		if call := live.calls[index]; call != nil {
			result.Calls = append(result.Calls, CallPreview{Index: index, ID: strings.Clone(call.id.String()), Name: strings.Clone(call.name.String()), Arguments: strings.Clone(call.arguments.String())})
		}
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
	return Observation{Snapshot: snapshot, Epoch: r.epoch, Messages: messages, Preview: preview}, nil
}
