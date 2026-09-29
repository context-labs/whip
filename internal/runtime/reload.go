package runtime

import (
	"context"
	"errors"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// ReloadSession admits captured settings only. The ordinary runtime scheduler
// applies them at an idle tree boundary, including after a process restart.
func (r *Runtime) ReloadSession(ctx context.Context, request session.ReloadRequest) (session.ReloadEdit, error) {
	if prior, err := r.store.ReloadRetry(ctx, request); err == nil {
		return prior, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return session.ReloadEdit{}, err
	}
	done, err := r.beginWorkspace()
	if err != nil {
		return session.ReloadEdit{}, err
	}
	defer done()
	snapshot, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return session.ReloadEdit{}, err
	}
	result, err := r.store.AdmitReload(ctx, request, snapshot.Host.Defaults, snapshot.Revision)
	if err == nil {
		r.Wake()
	}
	return result, err
}

func (r *Runtime) ReloadEdit(ctx context.Context, owner session.SessionID, id string) (session.ReloadEdit, error) {
	return r.store.ReloadEdit(ctx, owner, id)
}

// processReloads shares the existing bounded scheduler. It never waits for a
// busy resource reservation and considers one durable candidate per pass.
func (r *Runtime) processReloads(ctx context.Context) error {
	candidate, err := r.store.NextReload(ctx, r.reloadCursor)
	if errors.Is(err, store.ErrNotFound) {
		r.reloadCursor = ""
		return nil
	}
	if err != nil {
		return err
	}
	r.reloadCursor = candidate.ID
	err = r.applyReload(ctx, candidate)
	if errors.Is(err, store.ErrBusy) || errors.Is(err, store.ErrLimit) || errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

func (r *Runtime) applyReload(ctx context.Context, candidate store.PendingReload) error {
	edit, err := r.store.ReloadEdit(ctx, candidate.SessionID, candidate.ID)
	if err != nil {
		return err
	}
	if edit.State != session.ReloadPending {
		return nil
	}
	gate, drop := r.controlGate(candidate.TreeID)
	defer drop()
	if !gate.mu.TryLock() {
		return store.ErrBusy
	}
	defer gate.mu.Unlock()
	root, err := r.store.Session(ctx, candidate.SessionID)
	if errors.Is(err, store.ErrNotFound) {
		_, _, err = r.store.ApplyReload(ctx, candidate.ID)
		return err
	}
	if err != nil {
		return err
	}
	if root.ConfigRevision != edit.ExpectedRevision {
		_, _, err = r.store.ApplyReload(ctx, candidate.ID)
		return err
	}
	entry, err := r.controlMCPRoot(root)
	if err != nil {
		return err
	}
	select {
	case entry.actions <- struct{}{}:
		defer func() { <-entry.actions }()
	default:
		return store.ErrBusy
	}
	owners, err := r.store.ControlOwners(ctx, candidate.TreeID)
	if err != nil {
		return err
	}
	names := make([]string, len(owners))
	for i, id := range owners {
		names[i] = string(id)
	}
	release, err := r.shells.PauseIdle(names)
	if err != nil {
		return store.ErrBusy
	}
	defer release()
	_, changed, err := r.store.ApplyReload(ctx, candidate.ID)
	if err != nil {
		return err
	}
	if changed {
		// These joins occur before the tree can claim its next turn. No connection
		// is restarted and no standing grant is expanded by a changed selection.
		r.languageServers.Retire(string(candidate.SessionID))
		r.retireControlMCP(entry)
	}
	return nil
}

func (r *Runtime) CancelReload(ctx context.Context, owner session.SessionID, id string) (session.ReloadEdit, error) {
	result, err := r.store.CancelReload(ctx, owner, id)
	if err == nil {
		r.Wake()
	}
	return result, err
}
