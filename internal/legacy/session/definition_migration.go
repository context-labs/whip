package session

import (
	"context"
	"database/sql"
	"fmt"
)

// upgradeV15 records which agent definition a session runs. Every existing
// session predates definitions and ran the coding agent.
func upgradeV15(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`) }()
	var version int
	var identity string
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `SELECT identity FROM runtime_schema WHERE id=1`).Scan(&identity); err != nil {
		return err
	}
	if version >= 16 && identity != "whip-recursive-runtime-v15" {
		return nil
	}
	if version != 15 || identity != "whip-recursive-runtime-v15" {
		return fmt.Errorf("agent definition upgrade requires version 15, found %d", version)
	}
	if _, err := conn.ExecContext(ctx, `
ALTER TABLE sessions ADD COLUMN definition TEXT NOT NULL DEFAULT 'coding';
UPDATE runtime_schema SET identity='whip-recursive-runtime-v16' WHERE id=1;
PRAGMA user_version=16;`); err != nil {
		return fmt.Errorf("upgrade agent definitions: %w", err)
	}
	_, err := conn.ExecContext(ctx, `COMMIT`)
	return err
}
