package store

import (
	"context"
	"database/sql"

	"github.com/context-labs/whip/internal/session"
)

// DiagnosticRetention verifies that the dispatched diagnostic operation still
// has standing authority or the same eligible automatic policy captured at
// admission. One-use approval never retains a language server.
func (s *Store) DiagnosticRetention(ctx context.Context, id session.OperationID) (retain bool, err error) {
	err = s.write(ctx, func(tx *sql.Tx) error {
		operation, err := readOperation(ctx, tx, id)
		if err != nil {
			return err
		}
		if operation.State != session.OperationDispatched || operation.Capability != "lsp.diagnostics" {
			return ErrConflict
		}
		if err := authorizeOperation(ctx, tx, operation); err != nil {
			return err
		}
		if operation.PermissionRevision != nil {
			retain = true
			return nil
		}
		if operation.GrantID == nil {
			return ErrConflict
		}
		grant, err := readGrant(ctx, tx, *operation.GrantID)
		if err != nil {
			return err
		}
		retain = grant.OperationID == nil
		return nil
	})
	return
}
