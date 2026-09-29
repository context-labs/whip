package store

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"

	"github.com/context-labs/whip/internal/session"
)

func validatePermissionInspection(ctx context.Context, q querier, spec session.OperationSpec) error {
	var request session.PermissionInspection
	decoder := json.NewDecoder(bytes.NewReader(spec.Arguments))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return err
	}
	if err := request.Validate(); err != nil {
		return err
	}
	var owner session.SessionID
	var revision session.Revision
	if err := q.QueryRowContext(ctx, `SELECT t.session_id,t.config_revision FROM cells c JOIN turns t ON t.id=c.turn_id WHERE c.id=?`, spec.CellID).Scan(&owner, &revision); err != nil {
		return found(err)
	}
	if request.SessionID != owner || request.ConfigRevision != revision || spec.Resource != string(owner) {
		return ErrConflict
	}
	configuration, err := readConfiguration(ctx, q, owner, revision)
	if err != nil {
		return err
	}
	if !slices.Contains(configuration.Modules, "permissions") {
		return ErrConflict
	}
	return nil
}

// Permission exposes only an operation owned by the requesting session. The
// operation's owner is immutable; deletion between reads yields not found.
func (s *Store) Permission(ctx context.Context, owner session.SessionID, id session.OperationID) (session.Permission, error) {
	operation, err := readOperation(ctx, s.db, id)
	if err != nil {
		return session.Permission{}, err
	}
	if operation.SessionID != owner {
		return session.Permission{}, ErrNotFound
	}
	return readPermission(ctx, s.db, id)
}
