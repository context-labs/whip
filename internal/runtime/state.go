package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/content"
	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) State(ctx context.Context, actor session.SessionID, scope session.StateScope, key string) (session.StateValue, error) {
	return r.store.State(ctx, actor, scope, key)
}

func (r *Runtime) ListState(ctx context.Context, actor session.SessionID, scope session.StateScope, after string, limit int) ([]session.StateValue, error) {
	return r.store.ListState(ctx, actor, scope, after, limit)
}

func (r *Runtime) StateHistory(ctx context.Context, actor session.SessionID, scope session.StateScope, key string, after int64, limit int) ([]session.StateValue, error) {
	return r.store.StateHistory(ctx, actor, scope, key, after, limit)
}

func (r *Runtime) WriteState(ctx context.Context, actor session.SessionID, scope session.StateScope, id, key string, expectedRevision int64, data []byte) (session.StateValue, error) {
	if err := r.Err(); err != nil {
		return session.StateValue{}, err
	}
	request := session.StateWrite{ID: id, SessionID: actor, Scope: scope, Key: key, ExpectedRevision: expectedRevision}
	if err := request.ValidateIdentity(); err != nil {
		return session.StateValue{}, err
	}
	if err := session.ValidateStateJSON(data); err != nil {
		return session.StateValue{}, err
	}
	if _, err := r.store.Session(ctx, actor); err != nil {
		return session.StateValue{}, err
	}
	body, err := r.content.Put(data)
	if err != nil {
		return session.StateValue{}, err
	}
	request.Digest, request.Size = body.Digest, body.Size
	value, err := r.store.WriteState(ctx, request)
	if err == nil && scope == session.TreeState {
		r.Wake()
	}
	return value, err
}

// ReadStateRange returns bounded bytes, not a partial JSON value. Transports
// encode the bytes as base64 so a range may split a UTF-8 code point safely.
func (r *Runtime) ReadStateRange(ctx context.Context, actor session.SessionID, id string, offset int64, length int) (session.StateValue, []byte, error) {
	if offset < 0 || length < 1 || length > session.MaxStateReadBytes {
		return session.StateValue{}, nil, session.ErrInvalid
	}
	value, err := r.store.StateValue(ctx, actor, id)
	if err != nil {
		return session.StateValue{}, nil, err
	}
	if offset > value.Size {
		return session.StateValue{}, nil, session.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return session.StateValue{}, nil, err
	}
	data, err := r.content.ReadVerifiedRange(content.Body{Digest: value.Digest, Size: value.Size}, offset, length)
	if err != nil {
		return session.StateValue{}, nil, err
	}
	return value, data, ctx.Err()
}

func (r *Runtime) ReadState(ctx context.Context, actor session.SessionID, id string) (session.StateValue, []byte, error) {
	value, err := r.store.StateValue(ctx, actor, id)
	if err != nil {
		return session.StateValue{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return session.StateValue{}, nil, err
	}
	data, err := r.content.ReadVerified(content.Body{Digest: value.Digest, Size: value.Size}, session.MaxStateValueBytes)
	if err != nil {
		return session.StateValue{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return session.StateValue{}, nil, err
	}
	return value, data, nil
}

func (r *Runtime) AppendState(ctx context.Context, actor session.SessionID, scope session.StateScope, id, key string, expectedRevision int64, suffix []byte) (session.StateValue, error) {
	if err := r.Err(); err != nil {
		return session.StateValue{}, err
	}
	if err := session.ValidateID(id); err != nil {
		return session.StateValue{}, err
	}
	request, err := r.stageState(ctx, actor, "append", session.StatePut{Scope: scope, Key: key, ExpectedRevision: &expectedRevision, Value: suffix})
	if err != nil {
		return session.StateValue{}, err
	}
	request.ID = id
	value, err := r.store.WriteState(ctx, request)
	if err == nil && scope == session.TreeState {
		r.Wake()
	}
	return value, err
}
