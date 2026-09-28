package session

import (
	"context"
	"database/sql"
	"fmt"
)

// upgradeV17 records which named child of its parent's definition a retained
// agent runs, so restart resolves the same narrowed definition. Existing
// children ran the unnamed (inherited) definition, whose name is empty.
func upgradeV17(ctx context.Context, conn *sql.Conn) error {
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
	if version >= 18 && identity != "whip-recursive-runtime-v17" {
		return nil
	}
	if version != 17 || identity != "whip-recursive-runtime-v17" {
		return fmt.Errorf("named children upgrade requires version 17, found %d", version)
	}
	if _, err := conn.ExecContext(ctx, `
ALTER TABLE agents ADD COLUMN definition TEXT NOT NULL DEFAULT '';
UPDATE runtime_schema SET identity='whip-recursive-runtime-v18' WHERE id=1;
PRAGMA user_version=18;`); err != nil {
		return fmt.Errorf("upgrade named children: %w", err)
	}
	_, err := conn.ExecContext(ctx, `COMMIT`)
	return err
}
