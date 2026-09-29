package runtime

import (
	"context"
	"errors"
	"maps"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func (r *Runtime) CreateTree(ctx context.Context, request store.CreateTree) (session.Tree, session.Session, error) {
	current, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return session.Tree{}, session.Session{}, err
	}
	resources, err := session.ResolveResourceLimits(current.Host.Resources, request.Resources)
	if err != nil {
		return session.Tree{}, session.Session{}, err
	}
	request.Resources = resources
	request.Defaults = current.Host.Defaults.Clone()
	if request.Engine == "" {
		request.Engine = current.Host.Engine
	}
	if request.PermissionMode == nil {
		mode, err := session.ResolvePermissionMode(current.Host.DefaultPermissionMode)
		if err != nil {
			return session.Tree{}, session.Session{}, err
		}
		request.PermissionMode = &mode
	}
	return r.store.CreateTree(ctx, request)
}

// CreateRoot preserves the exact caller request across uncertain delivery.
func (r *Runtime) CreateRoot(ctx context.Context, request session.TreeCreationRequest) (session.TreeCreationResult, error) {
	if result, exists, err := r.store.CreationRetry(ctx, request); exists || err != nil {
		return result, err
	}
	current, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return session.TreeCreationResult{}, err
	}
	return r.store.CreateRoot(ctx, request, session.TreeCreationDefaults{
		Engine:        current.Host.Engine,
		Configuration: current.Host.Defaults.Clone(), Resources: current.Host.Resources,
		PermissionMode: current.Host.DefaultPermissionMode,
	})
}

func (r *Runtime) TreeCreation(ctx context.Context, id session.CreationID) (session.TreeCreationResult, error) {
	return r.store.TreeCreation(ctx, id)
}

func (r *Runtime) SpawnChild(ctx context.Context, identity session.RequestIdentity, request store.ChildRequest) (store.ChildAdmission, error) {
	if err := r.Err(); err != nil {
		return store.ChildAdmission{}, err
	}
	result, err := r.store.SpawnChild(ctx, identity, request)
	if err == nil {
		r.Wake()
	}
	return result, err
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

func (r *Runtime) ModelAttempts(ctx context.Context, id session.TurnID, after session.ModelAttemptID, limit int) ([]session.ModelAttempt, error) {
	return r.store.ModelAttempts(ctx, id, after, limit)
}

func (r *Runtime) CancelTurn(ctx context.Context, id session.TurnID) (session.Turn, error) {
	result, err := r.store.CancelTurn(ctx, id)
	if err == nil {
		r.languageServers.RetireAll()
		r.cancelTurn(id)
	}
	return result, err
}

func (r *Runtime) CancelInput(ctx context.Context, id session.InputID) (session.Input, error) {
	result, err := r.store.CancelInput(ctx, id)
	if err == nil && result.TurnID != nil {
		r.languageServers.RetireAll()
		r.cancelTurn(*result.TurnID)
	}
	return result, err
}

func (r *Runtime) SetLifecycle(ctx context.Context, id session.SessionID, state session.Lifecycle) (session.Session, error) {
	result, err := r.store.SetLifecycle(ctx, id, state)
	if err != nil {
		return session.Session{}, err
	}
	r.applyLifecycleChange(result)
	return result.Session, nil
}

func (r *Runtime) applyLifecycleChange(change store.LifecycleChange) {
	if change.Session.Lifecycle != session.Active {
		r.shells.Retire(string(change.Session.ID))
		r.languageServers.RetireAll()
	}
	if change.CancelTurnID != nil {
		r.cancelTurn(*change.CancelTurnID)
	}
	if change.Session.Lifecycle == session.Active {
		r.Wake()
	}
}

func (r *Runtime) DeleteSubtree(ctx context.Context, id session.SessionID) error {
	if err := r.store.DeleteSubtree(ctx, id); err != nil {
		return err
	}
	return r.cleanupDeletedKernels(ctx)
}

func (r *Runtime) cleanupDeletedKernels(ctx context.Context) error {
	if err := r.cleanupShellOwners(ctx); err != nil {
		return err
	}
	r.languageServers.RetireAll()
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	// SQL rejects active subtrees, so deleted sessions cannot still be executing.
	// Snapshot handles without holding the scheduler mutex during process cleanup.
	r.mu.Lock()
	entries := maps.Clone(r.kernels)
	r.mu.Unlock()
	for key, entry := range entries {
		if _, err := r.store.Session(cleanup, key); errors.Is(err, store.ErrNotFound) {
			r.discardKernel(key, entry)
		} else if err != nil {
			return err
		}
	}
	return r.cleanupDeletedMCP(cleanup)
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
