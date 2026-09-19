package session

import (
	"context"
	"database/sql"
	"fmt"
)

// Legacy compactions did not record whether a user message was pinned. A
// split-pin v20 row cannot be distinguished from an older unpinned fold, so
// default to no pin rather than inventing retained authorization. This can
// change a v20 split fold's derived view once; raw messages and summaries stay
// intact. New rows explicitly preserve the live fold's decision.
func upgradeV20(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`) }()
	var version int
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version >= 21 {
		_, err := conn.ExecContext(ctx, `COMMIT`)
		return err
	}
	if version != 20 {
		return fmt.Errorf("compaction pin upgrade requires version 20, found %d", version)
	}
	if _, err := conn.ExecContext(ctx, `ALTER TABLE compactions ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0 CHECK(pinned IN (0,1))`); err != nil {
		return err
	}
	if err := normalizeLegacyStamps(ctx, conn); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `UPDATE runtime_schema SET identity='whip-recursive-runtime-v21' WHERE id=1;
PRAGMA user_version=21;`); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, `COMMIT`)
	return err
}
