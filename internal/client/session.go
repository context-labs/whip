package client

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/protocol"
)

// Session is an inert scoped handle for either a root or a child. Constructing
// one never loads history, starts workers, or creates a polling subscription.
type Session struct {
	client *Client
	id     protocol.ID
}

func (c *Client) Session(id protocol.ID) (*Session, error) {
	raw, err := json.Marshal(protocol.SessionParams{SessionID: id})
	if err != nil {
		return nil, err
	}
	if err := protocol.Validate("SessionParams", raw); err != nil {
		return nil, err
	}
	return &Session{client: c, id: id}, nil
}
func (s *Session) ID() protocol.ID { return s.id }
func (s *Session) Get(ctx context.Context) (value protocol.Session, err error) {
	err = s.client.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: s.id}, &value)
	if err == nil && value.ID != s.id {
		err = errors.New("session ownership mismatch")
	}
	return
}

func (s *Session) Activity(ctx context.Context) (value protocol.SessionActivity, err error) {
	err = s.client.Call(ctx, "sessions.activity", protocol.SessionParams{SessionID: s.id}, &value)
	if err == nil && (value.SessionID != s.id || value.ActiveTurn != nil && value.ActiveTurn.SessionID != s.id) {
		err = errors.New("activity ownership mismatch")
	}
	return
}

func (s *Session) Input(ctx context.Context, id protocol.ID) (value protocol.Input, err error) {
	err = s.client.Call(ctx, "inputs.get", protocol.SessionInputParams{SessionID: s.id, InputID: id}, &value)
	if err == nil && (value.SessionID != s.id || value.ID != id) {
		err = errors.New("input ownership mismatch")
	}
	return
}

func (s *Session) Inputs(ctx context.Context, state string, after *protocol.Counter, limit int) (value protocol.InputPageResult, err error) {
	err = s.client.Call(ctx, "inputs.page", protocol.InputPageParams{SessionID: s.id, State: state, After: after, Limit: limit}, &value)
	if err != nil {
		return
	}
	cursor := protocol.Counter(0)
	if after != nil {
		cursor = *after
	}
	for _, input := range value.Items {
		if input.SessionID != s.id || input.Ordinal <= cursor {
			return protocol.InputPageResult{}, errors.New("input page ownership or cursor mismatch")
		}
		cursor = input.Ordinal
	}
	if len(value.Items) > limit || value.NextCursor != nil && (len(value.Items) == 0 || *value.NextCursor != cursor) {
		return protocol.InputPageResult{}, errors.New("invalid input page cursor")
	}
	return
}

func (s *Session) Turn(ctx context.Context, id protocol.ID) (value protocol.Turn, err error) {
	err = s.client.Call(ctx, "turns.get", protocol.TurnParams{TurnID: id}, &value)
	if err == nil && (value.SessionID != s.id || value.ID != id) {
		err = errors.New("turn ownership mismatch")
	}
	return
}

func (s *Session) CancelTurn(ctx context.Context, id protocol.ID) (protocol.Turn, error) {
	if _, err := s.Turn(ctx, id); err != nil {
		return protocol.Turn{}, err
	}
	var result protocol.Turn
	err := s.client.Call(ctx, "turns.cancel", protocol.TurnParams{TurnID: id}, &result)
	if err == nil && (result.SessionID != s.id || result.ID != id) {
		err = errors.New("cancelled turn ownership mismatch")
	}
	return result, err
}

func (s *Session) CancelInput(ctx context.Context, id protocol.ID) (protocol.Input, error) {
	if _, err := s.Input(ctx, id); err != nil {
		return protocol.Input{}, err
	}
	var result protocol.Input
	err := s.client.Call(ctx, "inputs.cancel", protocol.InputParams{InputID: id}, &result)
	if err == nil && (result.SessionID != s.id || result.ID != id) {
		err = errors.New("cancelled input ownership mismatch")
	}
	return result, err
}

func (s *Session) History(ctx context.Context, p protocol.HistoryPageParams) (value protocol.HistoryPageResult, err error) {
	p.SessionID = s.id
	err = s.client.Call(ctx, "sessions.history_page", p, &value)
	if err != nil {
		return
	}
	if value.Snapshot.SessionID != s.id || p.ExpectedRevision != nil && value.Snapshot.Revision != *p.ExpectedRevision || len(value.Messages) > p.Limit {
		return protocol.HistoryPageResult{}, errors.New("history ownership or revision mismatch")
	}
	var previous protocol.Counter
	for i, message := range value.Messages {
		if message.SessionID != s.id || message.Sequence > value.Snapshot.ThroughSequence || i > 0 && message.Sequence <= previous {
			return protocol.HistoryPageResult{}, errors.New("history page sequence mismatch")
		}
		if p.Cursor != nil && (p.Direction == "forward" && message.Sequence <= *p.Cursor || p.Direction == "backward" && message.Sequence >= *p.Cursor) {
			return protocol.HistoryPageResult{}, errors.New("history page exceeded requested cursor")
		}
		previous = message.Sequence
	}
	if value.NextCursor != nil {
		if len(value.Messages) == 0 {
			return protocol.HistoryPageResult{}, errors.New("empty history page has continuation")
		}
		expected := value.Messages[len(value.Messages)-1].Sequence
		if p.Direction == "backward" {
			expected = value.Messages[0].Sequence
		}
		if *value.NextCursor != expected {
			return protocol.HistoryPageResult{}, errors.New("history continuation mismatch")
		}
	}
	return
}

func (s *Session) Submission(params protocol.SubmitParams) (*InputCommand, error) {
	params.SessionID = s.id
	return s.client.PrepareInput("sessions.submit", params)
}

// PutContent uploads at most one bounded complete body under this owner. Handle
// and bytes are caller supplied and stable, making explicit retry idempotent.
func (s *Session) PutContent(ctx context.Context, id protocol.ID, media string, data []byte) (protocol.ContentReference, error) {
	if len(data) > 4<<20 {
		return protocol.ContentReference{}, errors.New("content exceeds 4 MiB")
	}
	var ref protocol.ContentReference
	err := s.client.Call(ctx, "content.put", protocol.PutContentParams{SessionID: s.id, ReferenceID: id, MediaType: media, DataBase64: base64.StdEncoding.EncodeToString(data)}, &ref)
	if err == nil {
		err = checkContent(s.id, id, ref, data)
		if err == nil && ref.MediaType != media {
			err = errors.New("content media type mismatch")
		}
	}
	return ref, err
}

func (s *Session) ReadContent(ctx context.Context, id protocol.ID) (protocol.ContentReference, []byte, error) {
	var result protocol.ReadContentResult
	if err := s.client.Call(ctx, "content.read", protocol.ReadContentParams{SessionID: s.id, ReferenceID: id}, &result); err != nil {
		return protocol.ContentReference{}, nil, err
	}
	if len(result.DataBase64) > base64.StdEncoding.EncodedLen(4<<20) {
		return protocol.ContentReference{}, nil, errors.New("encoded content exceeds limit")
	}
	data, err := base64.StdEncoding.DecodeString(result.DataBase64)
	if err != nil {
		return protocol.ContentReference{}, nil, err
	}
	if err := checkContent(s.id, id, result.Reference, data); err != nil {
		return protocol.ContentReference{}, nil, err
	}
	return result.Reference, data, nil
}

func checkContent(owner, id protocol.ID, ref protocol.ContentReference, data []byte) error {
	digest := sha256.Sum256(data)
	if ref.SessionID != owner || ref.ID != id || int64(ref.Size) != int64(len(data)) || ref.Digest != hex.EncodeToString(digest[:]) {
		return errors.New("content owner, size, or digest mismatch")
	}
	return nil
}
