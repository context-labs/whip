package runtime

import (
	"context"

	"github.com/context-labs/whip/internal/lsp"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) PrepareDiagnostics(ctx context.Context, owner session.Session) (tool.DiagnosticRun, error) {
	generation := r.languageServers.Generation()
	snapshot, err := r.HostConfiguration().Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, id session.OperationID, file tool.FileSnapshot) (any, error) {
		retain, err := r.store.DiagnosticRetention(ctx, id)
		if err != nil {
			return nil, err
		}
		return r.languageServers.Diagnostics(ctx, string(owner.ID), owner.WorkingDirectory, file.Path, file.Text, snapshot.Host.LSP, retain, generation, file.WorkspaceIdentity)
	}, nil
}

// LSPStatus is human inspection only. It never starts a server or grants access.
func (r *Runtime) LSPStatus(ctx context.Context, owner session.SessionID) ([]lsp.Status, error) {
	if _, err := r.store.Session(ctx, owner); err != nil {
		return nil, err
	}
	snapshot, err := r.HostConfiguration().Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	return r.languageServers.Statuses(string(owner), snapshot.Host.LSP), nil
}
