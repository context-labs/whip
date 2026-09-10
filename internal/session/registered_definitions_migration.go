package session

import (
	"context"
	"database/sql"
	"fmt"
)

// upgradeV16 adds registered agent definitions and pins each session to a
// definition revision. Existing sessions run built-ins, whose revision is empty.
func upgradeV16(ctx context.Context, conn *sql.Conn) error {
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
	if version >= 17 && identity != "whip-recursive-runtime-v16" {
		return nil
	}
	if version != 16 || identity != "whip-recursive-runtime-v16" {
		return fmt.Errorf("registered definitions upgrade requires version 16, found %d", version)
	}
	if _, err := conn.ExecContext(ctx, `
ALTER TABLE sessions ADD COLUMN definition_revision TEXT NOT NULL DEFAULT '';
CREATE TABLE definitions (
	id TEXT NOT NULL, revision TEXT NOT NULL, body BLOB NOT NULL, registered_by TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL, PRIMARY KEY(id,revision)
);
UPDATE runtime_schema SET identity='whip-recursive-runtime-v17' WHERE id=1;
PRAGMA user_version=17;`); err != nil {
		return fmt.Errorf("upgrade registered definitions: %w", err)
	}
	_, err := conn.ExecContext(ctx, `COMMIT`)
	return err
}
