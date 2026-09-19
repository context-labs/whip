package session

import (
	"context"
	"database/sql"
	"fmt"
)

const inboxSteerTrigger = `CREATE TRIGGER inbox_steer_turn_finished AFTER UPDATE OF status ON turns
WHEN NEW.status<>'running' BEGIN
 UPDATE inbox SET steer_turn_id='' WHERE root_id=NEW.root_id AND agent_id=NEW.agent_id AND steer_turn_id=NEW.id;
END;`

func upgradeV19(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer func() { _, _ = conn.ExecContext(context.WithoutCancel(ctx), `ROLLBACK`) }()
	var version int
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	var identity string
	if err := conn.QueryRowContext(ctx, `SELECT identity FROM runtime_schema WHERE id=1`).Scan(&identity); err != nil {
		return err
	}
	// A peer may have committed this step without reaching the latest schema.
	if version == 20 && identity == "whip-recursive-runtime-v20" ||
		version == currentSchemaVersion && identity == schemaIdentity {
		return nil
	}
	if version != 19 || identity != "whip-recursive-runtime-v19" {
		return fmt.Errorf("queue upgrade requires version 19, found %d", version)
	}
	if _, err := conn.ExecContext(ctx, `
ALTER TABLE inbox ADD COLUMN origin TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox ADD COLUMN command_client_id TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox ADD COLUMN command_id TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox ADD COLUMN steer_turn_id TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox ADD COLUMN delivery_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE inbox ADD COLUMN preview BLOB NOT NULL DEFAULT '{}';
UPDATE inbox SET origin='client',
 command_client_id=(SELECT client_id FROM commands c WHERE c.root_id=inbox.root_id AND c.scope='root' AND c.ingress_seq=inbox.seq),
 command_id=(SELECT command_id FROM commands c WHERE c.root_id=inbox.root_id AND c.scope='root' AND c.ingress_seq=inbox.seq)
WHERE agent_id=root_id AND kind IN ('submit','submit.parts','steer','steer.parts') AND EXISTS(
 SELECT 1 FROM commands c WHERE c.root_id=inbox.root_id AND c.scope='root' AND c.ingress_seq=inbox.seq AND c.operation IN ('submit','steer'));
`+inboxSteerTrigger+`
UPDATE runtime_schema SET identity='whip-recursive-runtime-v20' WHERE id=1;
PRAGMA user_version=20;`); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, `COMMIT`)
	return err
}
