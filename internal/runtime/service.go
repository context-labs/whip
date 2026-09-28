package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func (r *Runtime) CreateTree(ctx context.Context, request store.CreateTree) (session.Tree, session.Session, error) {
	request.Defaults = r.host.Defaults.Clone()
	return r.store.CreateTree(ctx, request)
}

func (r *Runtime) SpawnSession(ctx context.Context, request store.SpawnSession) (session.Session, error) {
	return r.store.SpawnSession(ctx, request)
}

func (r *Runtime) Tree(ctx context.Context, id session.TreeID) (session.Tree, error) {
	return r.store.Tree(ctx, id)
}

func (r *Runtime) Session(ctx context.Context, id session.SessionID) (session.Session, error) {
	return r.store.Session(ctx, id)
}

func (r *Runtime) Sessions(ctx context.Context, id session.TreeID, after session.SessionID, limit int) ([]session.Session, error) {
	return r.store.Sessions(ctx, id, after, limit)
}

func (r *Runtime) UpdateTree(ctx context.Context, id session.TreeID, revision session.Revision, metadata session.TreeMetadata) (session.Tree, error) {
	return r.store.UpdateTree(ctx, id, revision, metadata)
}

func (r *Runtime) UpdateConfiguration(ctx context.Context, id session.SessionID, revision session.Revision, patch session.ConfigPatch) (session.Session, error) {
	return r.store.UpdateConfiguration(ctx, id, revision, patch)
}

func (r *Runtime) Admit(ctx context.Context, identity session.RequestIdentity, request store.Submission) (store.Admission, error) {
	if err := r.Err(); err != nil {
		return store.Admission{}, err
	}
	admitted, err := r.store.Admit(ctx, identity, request)
	if err == nil {
		r.Wake()
	}
	return admitted, err
}

func (r *Runtime) Admission(ctx context.Context, identity session.RequestIdentity) (store.Admission, error) {
	return r.store.Admission(ctx, identity)
}

func (r *Runtime) History(ctx context.Context, id session.SessionID, after int64, limit int) ([]session.Message, error) {
	return r.store.History(ctx, id, after, limit)
}

func (r *Runtime) Turn(ctx context.Context, id session.TurnID) (session.Turn, error) {
	return r.store.Turn(ctx, id)
}

func (r *Runtime) CancelTurn(ctx context.Context, id session.TurnID) (session.Turn, error) {
	result, err := r.store.CancelTurn(ctx, id)
	if err == nil {
		r.cancelTurn(id)
	}
	return result, err
}

func (r *Runtime) CancelInput(ctx context.Context, id session.InputID) (session.Input, error) {
	result, err := r.store.CancelInput(ctx, id)
	if err == nil && result.TurnID != nil {
		r.cancelTurn(*result.TurnID)
	}
	return result, err
}

func (r *Runtime) SetLifecycle(ctx context.Context, id session.SessionID, state session.Lifecycle) (session.Session, error) {
	result, err := r.store.SetLifecycle(ctx, id, state)
	if err != nil {
		return result, err
	}
	if state == session.Stopped {
		r.mu.Lock()
		if active, ok := r.active[id]; ok {
			active.cancel()
		}
		r.mu.Unlock()
	} else {
		r.Wake()
	}
	return result, nil
}

func (r *Runtime) DeleteSubtree(ctx context.Context, id session.SessionID) error {
	return r.store.DeleteSubtree(ctx, id)
}

func (r *Runtime) RegisterDefinition(ctx context.Context, document session.DefinitionDocument) (session.DefinitionRevision, error) {
	return r.store.RegisterDefinition(ctx, document)
}

func (r *Runtime) Definition(ctx context.Context, ref session.DefinitionRef) (session.DefinitionRevision, error) {
	return r.store.Definition(ctx, ref)
}

func (r *Runtime) Builtins() ([]session.DefinitionRef, error) {
	refs := []session.DefinitionRef{}
	for _, document := range session.Builtins() {
		_, _, ref, err := session.CanonicalDefinition(document)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}
