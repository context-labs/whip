package browserhost

import (
	"encoding/json"
	"errors"
)

// Event is observation, never authority. A sequence gap revokes this lineage;
// stale events from a retired handle leave unrelated siblings untouched.
func (p *Peer) Event(event Event) error {
	if !textBound(event.DocumentRevision, 128) || !textBound(event.URL, 8192) || !textBound(event.Title, 512) || !textBound(event.Method, 256) || len(event.Params) > 256<<10 || event.Sequence == 0 {
		return errors.New("invalid browser event")
	}
	switch event.Kind {
	case "cdp":
		if event.Method == "" || !json.Valid(event.Params) {
			return errors.New("invalid CDP event")
		}
	case "state", "document", "closed", "revoked", "preview_disconnected":
	default:
		return errors.New("unsupported browser event")
	}
	h := p.host
	h.mu.Lock()
	defer h.mu.Unlock()
	v := h.selected[event.RootID]
	if p.closed || v == nil || v.peer != p || v.binding.ProviderEpoch != event.ProviderEpoch || v.ctx.Err() != nil {
		return ErrStale
	}
	a := v.attachments[event.AttachmentID]
	if a != nil && a.value.Scope.TabID != event.TabID {
		return errors.New("event tab mismatch")
	}
	if a == nil || a.ctx.Err() != nil || a.value.Delegated || a.value.Scope.TabGeneration != event.TabGeneration || a.value.Scope.AttachmentGeneration != event.AttachmentGeneration {
		return ErrEventStale
	}
	if event.Sequence != a.sequence+1 {
		h.revokeLocked(a)
		return errors.New("event sequence gap; browser attachment revoked")
	}
	a.sequence = event.Sequence
	if event.DocumentRevision != "" && event.DocumentRevision != a.value.DocumentRevision {
		a.value.DocumentRevision = event.DocumentRevision
		if a.batch != nil && event.OperationID != a.batch.operationID {
			a.batch.cancel()
		}
	}
	if event.URL != "" {
		a.value.URL = cleanURL(event.URL)
	}
	if event.Title != "" {
		a.value.Title = cleanTitle(event.Title)
	}
	if event.Kind == "closed" {
		delete(v.created, event.TabID)
	}
	if event.Kind == "closed" || event.Kind == "revoked" || event.Kind == "preview_disconnected" {
		h.revokeLocked(a)
	}
	if event.Kind == "cdp" && a.batch != nil && !a.batch.closed {
		batch := a.batch
		event.Params = append(json.RawMessage(nil), event.Params...)
		raw, _ := json.Marshal(event)
		if len(batch.events) >= 64 || len(raw) > (2<<20)-batch.eventBytes || len(raw) > (16<<20)-h.eventBytes {
			h.revokeLocked(a)
			return ErrBusy
		}
		batch.events = append(batch.events, event)
		batch.eventBytes += len(raw)
		h.eventBytes += len(raw)
		select {
		case batch.eventReady <- struct{}{}:
		default:
		}
	}
	return nil
}
