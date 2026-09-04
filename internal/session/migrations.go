package session

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	currentSchemaVersion = 4
	schemaIdentity       = "whip-recursive-runtime-v4"
)

// MaxInboxRetries bounds how many times a failed turn may return its claimed
// inbox input to the queue before the input is marked interrupted.
const MaxInboxRetries = 3

// cleanSchema is the only schema supported by the pre-release runtime-v2
// store. The persisted model mirrors the runtime: sessions own recursive
// agents, not legacy tasks or one-shot child executions.
const cleanSchema = `
CREATE TABLE runtime_schema (
	id INTEGER PRIMARY KEY CHECK(id=1), identity TEXT NOT NULL
);
CREATE TABLE sessions (
	id TEXT PRIMARY KEY,
	kind TEXT NOT NULL CHECK(kind IN ('agent','tool_host')),
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	cwd TEXT NOT NULL,
	model TEXT NOT NULL,
	provider TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	goal TEXT NOT NULL DEFAULT '',
	forked_from TEXT NOT NULL DEFAULT '',
	fork_seq INTEGER NOT NULL DEFAULT 0,
	tags TEXT NOT NULL DEFAULT '',
	pinned INTEGER NOT NULL DEFAULT 0,
	effort TEXT NOT NULL DEFAULT '',
	usage_in INTEGER NOT NULL DEFAULT 0,
	usage_cached INTEGER NOT NULL DEFAULT 0,
	usage_out INTEGER NOT NULL DEFAULT 0,
	todos TEXT NOT NULL DEFAULT '',
	CHECK((kind='agent' AND model<>'' AND provider<>'') OR (kind='tool_host' AND model='' AND provider=''))
);
CREATE TABLE messages (
	session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL,
	PRIMARY KEY(session_id,seq)
);
CREATE TABLE snapshots (
	session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, ref TEXT NOT NULL, created_at TEXT NOT NULL,
	PRIMARY KEY(session_id,seq)
);
CREATE TABLE schedules (
	session_id TEXT NOT NULL REFERENCES sessions(id), id INTEGER NOT NULL, schedule TEXT NOT NULL, prompt TEXT NOT NULL,
	anchor TEXT NOT NULL, last_fire TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
	PRIMARY KEY(session_id,id)
);
CREATE TABLE compactions (
	session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, cutoff INTEGER NOT NULL, summary TEXT NOT NULL,
	created_at TEXT NOT NULL, PRIMARY KEY(session_id,seq)
);
CREATE TABLE agents (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), parent_id TEXT,
	name TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '', provider TEXT NOT NULL DEFAULT '',
	effort TEXT NOT NULL DEFAULT '', cwd TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(root_id,id),
	FOREIGN KEY(root_id,parent_id) REFERENCES agents(root_id,id)
);
CREATE INDEX agents_root_parent ON agents(root_id,parent_id);
CREATE TABLE turns (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	status TEXT NOT NULL, trigger TEXT NOT NULL DEFAULT 'inbox' CHECK(trigger IN ('inbox','mailbox')),
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE content_objects (
	digest TEXT PRIMARY KEY, size INTEGER NOT NULL CHECK(size>=0), created_at TEXT NOT NULL
);
CREATE TABLE content_references (
	id TEXT PRIMARY KEY, digest TEXT NOT NULL REFERENCES content_objects(digest), size INTEGER NOT NULL,
	media_type TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE TABLE content_grants (
	reference_id TEXT NOT NULL REFERENCES content_references(id), root_id TEXT NOT NULL REFERENCES sessions(id),
	agent_id TEXT NOT NULL DEFAULT '', scope TEXT NOT NULL CHECK(scope IN ('root','agent','subtree')),
	created_at TEXT NOT NULL, revoked_at TEXT NOT NULL DEFAULT '', PRIMARY KEY(reference_id,root_id,agent_id,scope)
);
CREATE TABLE transcript_messages (
	root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	seq INTEGER NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL,
	PRIMARY KEY(root_id,agent_id,seq),
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE agent_messages (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id),
	sender_agent_id TEXT NOT NULL, recipient_agent_id TEXT NOT NULL,
	kind TEXT NOT NULL DEFAULT 'message',
	delivery TEXT NOT NULL DEFAULT 'queued' CHECK(delivery IN ('steer','queued','next_turn')),
	upsert_key TEXT NOT NULL DEFAULT '',
	subject TEXT NOT NULL DEFAULT '', excerpt TEXT NOT NULL DEFAULT '', body_inline BLOB,
	body_ref TEXT REFERENCES content_references(id), evidence_ref TEXT REFERENCES content_references(id),
	status TEXT NOT NULL CHECK(status IN ('pending','delivered','done')),
	available_at TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
	delivered_at TEXT NOT NULL DEFAULT '', delivered_turn_id TEXT NOT NULL DEFAULT '', done_at TEXT NOT NULL DEFAULT '',
	CHECK(NOT(body_inline IS NOT NULL AND body_ref IS NOT NULL)),
	FOREIGN KEY(root_id,sender_agent_id) REFERENCES agents(root_id,id),
	FOREIGN KEY(root_id,recipient_agent_id) REFERENCES agents(root_id,id)
);
CREATE INDEX agent_messages_ready ON agent_messages(root_id,recipient_agent_id,status,available_at);
CREATE INDEX agent_messages_recipient_status
	ON agent_messages(root_id,recipient_agent_id,status,created_at,id);
CREATE TABLE commands (
	client_id TEXT NOT NULL, command_id TEXT NOT NULL, scope TEXT NOT NULL CHECK(scope IN ('daemon','root')),
	root_id TEXT REFERENCES sessions(id), request_digest TEXT NOT NULL, status TEXT NOT NULL,
	payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id), outcome_inline BLOB,
	outcome_ref TEXT REFERENCES content_references(id), ingress_seq INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(client_id,command_id),
	CHECK((scope='daemon' AND root_id IS NULL) OR (scope='root' AND root_id IS NOT NULL)),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	CHECK(NOT(outcome_inline IS NOT NULL AND outcome_ref IS NOT NULL))
);
CREATE UNIQUE INDEX commands_root_ingress ON commands(root_id,ingress_seq)
	WHERE scope='root' AND ingress_seq>0;
CREATE UNIQUE INDEX commands_daemon_ingress ON commands(ingress_seq)
	WHERE scope='daemon' AND ingress_seq>0;
CREATE TABLE events (
	root_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, kind TEXT NOT NULL, payload_inline BLOB,
	payload_ref TEXT REFERENCES content_references(id), created_at TEXT NOT NULL, PRIMARY KEY(root_id,seq),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL))
);
CREATE TABLE inbox (
	root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL, seq INTEGER NOT NULL,
	kind TEXT NOT NULL, status TEXT NOT NULL, payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id),
	retries INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL, PRIMARY KEY(root_id,agent_id,seq),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE agent_state (
	root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL, key TEXT NOT NULL,
	version INTEGER NOT NULL, author_agent_id TEXT NOT NULL, payload_inline BLOB,
	payload_ref TEXT REFERENCES content_references(id), updated_at TEXT NOT NULL, PRIMARY KEY(root_id,agent_id,key),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id),
	FOREIGN KEY(root_id,author_agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE agent_scratch (
	root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	program TEXT NOT NULL, manifest TEXT NOT NULL DEFAULT '', bytes INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL, PRIMARY KEY(root_id,agent_id),
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE capabilities (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	issuer_agent_id TEXT NOT NULL DEFAULT '', operations TEXT NOT NULL, scopes TEXT NOT NULL,
	generation INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE budgets (
	root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL,
	limit_value INTEGER NOT NULL, used_value INTEGER NOT NULL DEFAULT 0, reserved_value INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL, PRIMARY KEY(root_id,agent_id,kind)
);
CREATE TABLE operations (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	command_client_id TEXT NOT NULL DEFAULT '', command_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
	payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id), result_inline BLOB,
	result_ref TEXT REFERENCES content_references(id), created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	UNIQUE(root_id,id), CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	CHECK(NOT(result_inline IS NOT NULL AND result_ref IS NOT NULL)),
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE leases (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	operation_id TEXT NOT NULL, capability_id TEXT NOT NULL DEFAULT '', trace_id TEXT NOT NULL DEFAULT '',
	command_client_id TEXT NOT NULL DEFAULT '', command_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id),
	FOREIGN KEY(root_id,operation_id) REFERENCES operations(root_id,id)
);
CREATE TABLE blackboard (
	root_id TEXT NOT NULL REFERENCES sessions(id), key TEXT NOT NULL, version INTEGER NOT NULL, author_agent_id TEXT NOT NULL,
	payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id), updated_at TEXT NOT NULL, PRIMARY KEY(root_id,key),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	FOREIGN KEY(root_id,author_agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE blackboard_history (
	root_id TEXT NOT NULL REFERENCES sessions(id), key TEXT NOT NULL, version INTEGER NOT NULL, author_agent_id TEXT NOT NULL,
	payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id), created_at TEXT NOT NULL,
	PRIMARY KEY(root_id,key,version), CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	FOREIGN KEY(root_id,key) REFERENCES blackboard(root_id,key),
	FOREIGN KEY(root_id,author_agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE subscriptions (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	key TEXT NOT NULL, cursor INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE permission_requests (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	operation_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, request_inline BLOB,
	request_ref TEXT REFERENCES content_references(id), created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	CHECK(NOT(request_inline IS NOT NULL AND request_ref IS NOT NULL)),
	FOREIGN KEY(root_id,agent_id) REFERENCES agents(root_id,id)
);
CREATE TABLE permission_rules (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), operation TEXT NOT NULL, rule TEXT NOT NULL,
	principal_id TEXT NOT NULL, created_at TEXT NOT NULL, UNIQUE(root_id,operation,rule)
);
CREATE TABLE usage_charges (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL DEFAULT '',
	command_client_id TEXT NOT NULL DEFAULT '', command_id TEXT NOT NULL DEFAULT '', input_tokens INTEGER NOT NULL DEFAULT 0,
	cached_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0, cost_micros INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);
CREATE TABLE content_orphans (
	digest TEXT PRIMARY KEY, size INTEGER NOT NULL, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL
);
CREATE TABLE daemon_state (
	id INTEGER PRIMARY KEY CHECK(id=1), generation INTEGER NOT NULL CHECK(generation>=0),
	build_id TEXT NOT NULL, status TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE client_identities (
	client_id TEXT PRIMARY KEY, kind TEXT NOT NULL, public_key BLOB NOT NULL,
	paired_by TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, CHECK(length(public_key)=32)
);`

func migrate(ctx context.Context, db *sql.DB, path string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	var objects, version int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'`).Scan(&objects); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if objects == 0 && version == 0 {
		if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, cleanSchema); err != nil {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
			return err
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO runtime_schema(id,identity) VALUES(1,?)`, schemaIdentity); err != nil {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
			return err
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version=%d`, currentSchemaVersion)); err != nil {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
			return err
		}
		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
			return err
		}
		return nil
	}

	var identity string
	identityErr := conn.QueryRowContext(ctx, `SELECT identity FROM runtime_schema WHERE id=1`).Scan(&identity)
	if version == currentSchemaVersion && identityErr == nil && identity == schemaIdentity {
		return nil
	}
	return fmt.Errorf("incompatible development runtime database %q (schema version %d): archive or remove it, then restart WHIP", path, version)
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
