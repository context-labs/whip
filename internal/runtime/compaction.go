package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func (r *Runtime) AdmitCompaction(ctx context.Context, identity session.RequestIdentity, owner session.SessionID) (store.Admission, error) {
	return r.Admit(ctx, identity, store.Submission{SessionID: owner, Source: session.UserInput, Kind: session.CompactInput})
}

func (r *Runtime) ContextHead(ctx context.Context, owner session.SessionID) (session.ContextHead, error) {
	return r.store.ContextHead(ctx, owner)
}

func (r *Runtime) Compaction(ctx context.Context, owner session.SessionID, id session.CompactionID) (session.Compaction, error) {
	return r.store.Compaction(ctx, owner, id)
}

func (r *Runtime) Compactions(ctx context.Context, owner session.SessionID, after session.CompactionID, limit int) ([]session.CompactionMetadata, error) {
	return r.store.Compactions(ctx, owner, after, limit)
}

func (r *Runtime) SelectCompaction(ctx context.Context, owner session.SessionID, revision int64, id *session.CompactionID) (session.ContextHead, error) {
	return r.store.SelectCompaction(ctx, owner, revision, id)
}
