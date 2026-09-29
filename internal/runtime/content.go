package runtime

import (
	"context"
	"fmt"

	"github.com/context-labs/whip/internal/content"
	"github.com/context-labs/whip/internal/session"
)

func (r *Runtime) PutContent(ctx context.Context, owner session.SessionID, id, mediaType string, data []byte) (session.ContentReference, error) {
	if err := session.ValidateID(id); err != nil {
		return session.ContentReference{}, err
	}
	if err := session.ValidateMediaType(mediaType); err != nil {
		return session.ContentReference{}, err
	}
	if len(data) > session.MaxContentBytes {
		return session.ContentReference{}, fmt.Errorf("%w: content exceeds 4 MiB", session.ErrInvalid)
	}
	if _, err := r.store.Session(ctx, owner); err != nil {
		return session.ContentReference{}, err
	}
	body, err := r.content.Put(data)
	if err != nil {
		return session.ContentReference{}, err
	}
	return r.store.RegisterContent(ctx, session.ContentReference{
		ID: id, SessionID: owner, Digest: body.Digest, Size: body.Size, MediaType: mediaType,
	})
}

// ContentReference inspects immutable owner-scoped metadata without reading bytes.
func (r *Runtime) ContentReference(ctx context.Context, owner session.SessionID, id string) (session.ContentReference, error) {
	return r.store.ContentReference(ctx, owner, id)
}

func (r *Runtime) ReadContent(ctx context.Context, owner session.SessionID, id string, maxBytes int64) (session.ContentReference, []byte, error) {
	reference, err := r.store.ContentReference(ctx, owner, id)
	if err != nil {
		return session.ContentReference{}, nil, err
	}
	if maxBytes < 0 || reference.Size > maxBytes || maxBytes > session.MaxContentBytes {
		return session.ContentReference{}, nil, fmt.Errorf("%w: content exceeds read limit", session.ErrInvalid)
	}
	data, err := r.content.ReadVerified(content.Body{Digest: reference.Digest, Size: reference.Size}, maxBytes)
	if err != nil {
		return session.ContentReference{}, nil, err
	}
	if err := ctx.Err(); err != nil {
		return session.ContentReference{}, nil, err
	}
	return reference, data, nil
}
