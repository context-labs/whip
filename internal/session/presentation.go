package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

const MaxPresentationBytes = 64 << 10
const MaxPresentationParts = 128

// MessagePresentation is display evidence, never model input or execution authority.
// IDs are local to AttemptID. Forked messages retain this source identity, scoped
// by their new owning session/message. Text ranges are UTF-8 byte offsets into
// concatenated text Parts (or live preview text); reasoning is explicit display
// text, never provider continuation. Failed attempts embed their partial bodies.
type MessagePresentation struct {
	Version   int                `json:"version"`
	AttemptID ModelAttemptID     `json:"attempt_id"`
	Parts     []PresentationPart `json:"parts"`
	Truncated bool               `json:"truncated"`
}
type PresentationPart struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Text      string            `json:"text,omitempty"`
	Start     *int              `json:"start,omitempty"`
	End       *int              `json:"end,omitempty"`
	CallIndex *int              `json:"call_index,omitempty"`
	CallID    string            `json:"call_id,omitempty"`
	Call      *PresentationCall `json:"call,omitempty"`
}
type PresentationCall struct {
	Index     int    `json:"index"`
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
type AttemptPresentation struct {
	GroupID         HistoryGroupID       `json:"group_id"`
	SourceSessionID *SessionID           `json:"source_session_id,omitempty"`
	AttemptID       ModelAttemptID       `json:"attempt_id"`
	TurnID          TurnID               `json:"turn_id"`
	MessageID       MessageID            `json:"message_id,omitempty"`
	State           ModelAttemptState    `json:"state"`
	Presentation    *MessagePresentation `json:"presentation"`
}

func (p *MessagePresentation) Validate() error {
	if p == nil {
		return nil
	}
	if p.Version != 1 || ValidateID(string(p.AttemptID)) != nil || len(p.Parts) > MaxPresentationParts {
		return fmt.Errorf("%w: invalid presentation", ErrInvalid)
	}
	ids := make(map[string]bool, len(p.Parts))
	for _, part := range p.Parts {
		if ValidateID(part.ID) != nil || ids[part.ID] || !utf8.ValidString(part.Text) {
			return fmt.Errorf("%w: invalid presentation slot", ErrInvalid)
		}
		ids[part.ID] = true
		switch part.Type {
		case "text":
			if part.Call != nil || part.CallIndex != nil || part.CallID != "" || (part.Start == nil) != (part.End == nil) {
				return ErrInvalid
			}
			if part.Start != nil && (part.Text != "" || *part.Start < 0 || *part.End < *part.Start || *part.End > MaxDocumentBytes) {
				return ErrInvalid
			}
		case "reasoning":
			if part.Start != nil || part.End != nil || part.CallIndex != nil || part.CallID != "" || part.Call != nil {
				return ErrInvalid
			}
		case "tool_call":
			if part.Start != nil || part.End != nil || part.Text != "" || part.CallIndex == nil || *part.CallIndex < 0 || *part.CallIndex >= MaxToolCalls {
				return ErrInvalid
			}
			if part.CallID != "" && ValidateID(part.CallID) != nil {
				return ErrInvalid
			}
			if part.Call != nil && (part.Call.Index != *part.CallIndex || len(part.Call.ID) > 128 || len(part.Call.Name) > 64 || !utf8.ValidString(part.Call.ID+part.Call.Name+part.Call.Arguments)) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	raw, err := json.Marshal(p)
	if err != nil || len(raw) > MaxPresentationBytes {
		return fmt.Errorf("%w: oversized presentation", ErrInvalid)
	}
	return nil
}

func (p *MessagePresentation) ValidateMessage(parts []Part) error {
	if err := p.Validate(); err != nil || p == nil {
		return err
	}
	var text strings.Builder
	calls := map[string]bool{}
	for _, part := range parts {
		if part.Type == "text" {
			text.WriteString(part.Text)
		}
		if part.Call != nil {
			calls[part.Call.ID] = true
		}
	}
	value := text.String()
	for _, part := range p.Parts {
		if part.Type == "text" && (part.Start == nil || *part.End > len(value) || !utf8.ValidString(value[*part.Start:*part.End])) {
			return ErrInvalid
		}
		if part.Type == "tool_call" && (part.Call != nil || !calls[part.CallID]) {
			return ErrInvalid
		}
	}
	return nil
}
