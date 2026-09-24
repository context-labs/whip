package session

import (
	"context"
	"database/sql"
	"fmt"
)

// upgradeV14 preserves existing scratch; legacy sessions are explicitly Starlark.
func upgradeV14(ctx context.Context, conn *sql.Conn) error {
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
	if version >= 15 && identity != "whip-recursive-runtime-v14" {
		return nil
	}
	if version != 14 || identity != "whip-recursive-runtime-v14" {
		return fmt.Errorf("execution engine upgrade requires version 14, found %d", version)
	}
	if _, err := conn.ExecContext(ctx, `
ALTER TABLE sessions ADD COLUMN execution_engine TEXT NOT NULL DEFAULT 'starlark' CHECK(execution_engine IN ('starlark','quickjs'));
CREATE TRIGGER session_engine_immutable BEFORE UPDATE OF execution_engine ON sessions
WHEN NEW.execution_engine<>OLD.execution_engine BEGIN SELECT RAISE(ABORT,'session execution engine is immutable'); END;
CREATE TABLE agent_checkpoints (
 root_id TEXT NOT NULL, agent_id TEXT NOT NULL, envelope BLOB NOT NULL, image BLOB NOT NULL,
 bytes INTEGER NOT NULL CHECK(bytes>0 AND bytes<=41943040), updated_at TEXT NOT NULL,
 PRIMARY KEY(root_id,agent_id), FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id)
);
UPDATE runtime_schema SET identity='whip-recursive-runtime-v15' WHERE id=1;
PRAGMA user_version=15;`); err != nil {
		return fmt.Errorf("upgrade execution engines: %w", err)
	}
	_, err := conn.ExecContext(ctx, `COMMIT`)
	return err
}
