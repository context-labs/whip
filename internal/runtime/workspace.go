package runtime

import (
	"context"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func (r *Runtime) CaptureWorkspace(ctx context.Context, request session.WorkspaceRequest) (session.WorkspaceResult, error) {
	return r.workspaceAction(ctx, session.WorkspaceCapture, request)
}

func (r *Runtime) RestoreWorkspace(ctx context.Context, request session.WorkspaceRequest) (session.WorkspaceResult, error) {
	return r.workspaceAction(ctx, session.WorkspaceRestore, request)
}

func (r *Runtime) ReleaseWorkspace(ctx context.Context, request session.WorkspaceRequest) (session.WorkspaceResult, error) {
	return r.workspaceAction(ctx, session.WorkspaceRelease, request)
}

func (r *Runtime) WorkspaceSnapshot(ctx context.Context, owner session.SessionID, id session.WorkspaceSnapshotID) (session.WorkspaceSnapshot, error) {
	return r.store.WorkspaceSnapshot(ctx, owner, id)
}

func (r *Runtime) WorkspaceSnapshots(ctx context.Context, owner session.SessionID, after session.WorkspaceSnapshotID, limit int) ([]session.WorkspaceSnapshot, error) {
	return r.store.WorkspaceSnapshots(ctx, owner, after, limit)
}

func (r *Runtime) beginWorkspace() (func(), error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	select {
	case r.workspaceSlots <- struct{}{}:
	default:
		return nil, store.ErrLimit
	}
	r.workspaceCalls.Add(1)
	return func() { <-r.workspaceSlots; r.workspaceCalls.Done() }, nil
}

func (r *Runtime) workspaceAction(ctx context.Context, kind session.WorkspaceActionKind, request session.WorkspaceRequest) (session.WorkspaceResult, error) {
	done, err := r.beginWorkspace()
	if err != nil {
		return session.WorkspaceResult{}, err
	}
	defer done()
	// A retry needs no current source owner, path, ref or pin, including a receipt
	// whose owner was deleted after explicit snapshot release.
	previous, err := r.store.WorkspaceRetry(ctx, kind, request)
	if err == nil {
		return previous, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return session.WorkspaceResult{}, err
	}
	if err := request.Validate(); err != nil {
		return session.WorkspaceResult{}, err
	}
	current, err := r.store.Session(ctx, request.SessionID)
	if err != nil {
		return session.WorkspaceResult{}, err
	}
	preflight, cancel := r.workspace.Context(ctx)
	defer cancel()
	binding, err := r.workspace.Inspect(preflight, current.WorkingDirectory)
	if err != nil {
		return session.WorkspaceResult{}, err
	}
	unlock, err := r.workspace.Lock(preflight, binding)
	if err != nil {
		return session.WorkspaceResult{}, err
	}
	defer unlock()
	if previous, retryErr := r.store.WorkspaceRetry(preflight, kind, request); retryErr == nil {
		return previous, nil
	} else if !errors.Is(retryErr, store.ErrNotFound) {
		return session.WorkspaceResult{}, retryErr
	}
	// Recheck identity under the worktree lock before any durable effect claim.
	if kind != session.WorkspaceCapture {
		snapshot, err := r.store.WorkspaceSnapshot(preflight, request.SessionID, request.SnapshotID)
		if err != nil {
			return session.WorkspaceResult{}, err
		}
		if snapshot.Binding != binding {
			return session.WorkspaceResult{}, store.ErrConflict
		}
		if err := r.workspace.ValidatePin(preflight, snapshot, kind == session.WorkspaceRelease); err != nil {
			return session.WorkspaceResult{}, err
		}
	}
	result, claimed, err := r.store.ClaimWorkspace(preflight, kind, request, binding)
	if err != nil || !claimed {
		return result, err
	}
	// Once claimed, runtime lifetime and the action deadline own Git. A request
	// disconnect cannot turn accepted work into an unrecorded cancelled effect.
	owned, stop := r.workspace.Context(context.WithoutCancel(ctx))
	defer stop()
	switch kind {
	case session.WorkspaceCapture:
		var object string
		object, err = r.workspace.Capture(owned, binding)
		if err == nil {
			err = r.store.StageWorkspaceSnapshot(owned, request.ID, object)
		}
		if err == nil {
			result.Snapshot.ObjectID = &object
			err = r.workspace.Pin(owned, result.Snapshot)
		}
	case session.WorkspaceRestore:
		err = r.workspace.Restore(owned, result.Snapshot)
	case session.WorkspaceRelease:
		err = r.workspace.Release(owned, result.Snapshot)
	}
	// SQL stays open while Close joins these calls. Even a cancelled Git process
	// gets a bounded chance to record uncertainty; restart handles lost settlement.
	settlement, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finish()
	settled, settleErr := r.store.SettleWorkspace(settlement, request.ID, err == nil)
	r.Wake()
	if settleErr != nil {
		return result, settleErr
	}
	// The accepted action is the result, including uncertainty. Do not encourage a
	// client to reinterpret a post-claim error as safe to reissue with a new ID.
	return settled, nil
}
