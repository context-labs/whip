package session

import (
	"context"
	"database/sql"
	"fmt"
)

// upgradeV18 adds the durable trace: the spans table, the causal identity
// columns on inbox rows and mailbox messages, and the cost split on model
// calls. Existing rows keep their whole-second stamps and empty identities;
// nothing is backfilled because a span needs a start time nobody recorded.
func upgradeV18(ctx context.Context, conn *sql.Conn) error {
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
	if version >= 19 && identity != "whip-recursive-runtime-v18" {
		return nil
	}
	if version != 18 || identity != "whip-recursive-runtime-v18" {
		return fmt.Errorf("trace upgrade requires version 18, found %d", version)
	}
	if _, err := conn.ExecContext(ctx, `
ALTER TABLE inbox ADD COLUMN parent_span_id TEXT NOT NULL DEFAULT '';
ALTER TABLE inbox ADD COLUMN span_trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_messages ADD COLUMN sender_span_id TEXT NOT NULL DEFAULT '';
ALTER TABLE agent_messages ADD COLUMN span_trace_id TEXT NOT NULL DEFAULT '';
ALTER TABLE model_calls ADD COLUMN cost_input_micros INTEGER NOT NULL DEFAULT 0;
ALTER TABLE model_calls ADD COLUMN cost_cache_read_micros INTEGER NOT NULL DEFAULT 0;
ALTER TABLE model_calls ADD COLUMN cost_output_micros INTEGER NOT NULL DEFAULT 0;
CREATE TABLE spans (
	root_id TEXT NOT NULL REFERENCES sessions(id), id TEXT PRIMARY KEY, trace_id TEXT NOT NULL,
	parent_id TEXT NOT NULL DEFAULT '', agent_id TEXT NOT NULL, turn_id TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL CHECK(kind IN ('agent','llm','tool','host','wait')), name TEXT NOT NULL,
	start_ns INTEGER NOT NULL CHECK(start_ns>0), end_ns INTEGER NOT NULL DEFAULT 0 CHECK(end_ns=0 OR end_ns>=start_ns),
	status TEXT NOT NULL, attrs TEXT NOT NULL DEFAULT '{}', links TEXT NOT NULL DEFAULT '[]',
	updated_seq INTEGER NOT NULL
);
CREATE INDEX spans_root_trace ON spans(root_id,trace_id,start_ns);
CREATE INDEX spans_root_updated ON spans(root_id,updated_seq);
UPDATE runtime_schema SET identity='whip-recursive-runtime-v19' WHERE id=1;
PRAGMA user_version=19;`); err != nil {
		return fmt.Errorf("upgrade trace schema: %w", err)
	}
	_, err := conn.ExecContext(ctx, `COMMIT`)
	return err
}
