package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	PermissionModePrompt    = "prompt"
	PermissionModeAutomatic = "automatic"
)

// PermissionMode reads the saved consent choice, including for an unopened root.
func (s *Store) PermissionMode(ctx context.Context, rootID string) (string, error) {
	var mode string
	err := s.db.QueryRowContext(ctx, `SELECT permission_mode FROM sessions WHERE id=?`, rootID).Scan(&mode)
	return mode, err
}

// SetPermissionMode commits the session choice and client event together. The
// root actor applies the live policy only after this succeeds.
func (s *Store) SetPermissionMode(ctx context.Context, rootID, mode string) error {
	if mode != PermissionModePrompt && mode != PermissionModeAutomatic {
		return fmt.Errorf("unknown permission mode %q", mode)
	}
	if rootID == "" {
		return errors.New("permission mode requires a root ID")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE sessions SET permission_mode=? WHERE id=?`, mode, rootID)
	if err != nil {
		return fmt.Errorf("save permission mode: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("session %q does not exist: %w", rootID, sql.ErrNoRows)
	}
	payload, err := json.Marshal(struct {
		PermissionMode string `json:"permission_mode"`
	}{PermissionMode: mode})
	if err != nil {
		return err
	}
	if _, err := s.appendRootEventTx(ctx, tx, rootID, "session.permission_mode.updated", RuntimePayload{
		Data: payload, MediaType: "application/json", Source: "session.permission_mode.updated",
	}); err != nil {
		return err
	}
	return tx.Commit()
}
