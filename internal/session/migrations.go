package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"modernc.org/sqlite"
)

const currentSchemaVersion = 1

var sessionColumns = []struct{ name, definition string }{
	{"goal", "goal TEXT NOT NULL DEFAULT ''"},
	{"forked_from", "forked_from TEXT NOT NULL DEFAULT ''"},
	{"fork_seq", "fork_seq INTEGER NOT NULL DEFAULT 0"},
	{"tags", "tags TEXT NOT NULL DEFAULT ''"},
	{"pinned", "pinned INTEGER NOT NULL DEFAULT 0"},
	{"effort", "effort TEXT NOT NULL DEFAULT ''"},
	{"usage_in", "usage_in INTEGER NOT NULL DEFAULT 0"},
	{"usage_cached", "usage_cached INTEGER NOT NULL DEFAULT 0"},
	{"usage_out", "usage_out INTEGER NOT NULL DEFAULT 0"},
	{"todos", "todos TEXT NOT NULL DEFAULT ''"},
	{"task_id", "task_id TEXT NOT NULL DEFAULT ''"},
	{"mode", "mode TEXT NOT NULL DEFAULT 'classic' CHECK(mode IN ('classic','rlm'))"},
}

const sessionsSchema = `CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
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
	task_id TEXT NOT NULL DEFAULT '',
	mode TEXT NOT NULL DEFAULT 'classic' CHECK(mode IN ('classic','rlm'))
);`

const runtimeSchema = `
CREATE TABLE IF NOT EXISTS messages (
	session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL,
	PRIMARY KEY(session_id, seq)
);
CREATE TABLE IF NOT EXISTS tasks (
	session_id TEXT NOT NULL REFERENCES sessions(id), task_id TEXT NOT NULL, description TEXT NOT NULL, prompt TEXT NOT NULL,
	status TEXT NOT NULL, report TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, ended_at TEXT NOT NULL DEFAULT '',
	PRIMARY KEY(session_id, task_id)
);
CREATE TABLE IF NOT EXISTS snapshots (
	session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, ref TEXT NOT NULL, created_at TEXT NOT NULL,
	PRIMARY KEY(session_id, seq)
);
CREATE TABLE IF NOT EXISTS schedules (
	session_id TEXT NOT NULL REFERENCES sessions(id), id INTEGER NOT NULL, schedule TEXT NOT NULL, prompt TEXT NOT NULL,
	anchor TEXT NOT NULL, last_fire TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
	PRIMARY KEY(session_id, id)
);
CREATE TABLE IF NOT EXISTS compactions (
	session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, cutoff INTEGER NOT NULL, summary TEXT NOT NULL,
	created_at TEXT NOT NULL, PRIMARY KEY(session_id, seq)
);
CREATE TABLE IF NOT EXISTS agents (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), parent_id TEXT, status TEXT NOT NULL,
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(root_id, id),
	FOREIGN KEY(root_id, parent_id) REFERENCES agents(root_id, id)
);
CREATE INDEX IF NOT EXISTS agents_root_parent ON agents(root_id, parent_id);
CREATE TABLE IF NOT EXISTS content_objects (
	digest TEXT PRIMARY KEY, size INTEGER NOT NULL CHECK(size >= 0), created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS content_references (
	id TEXT PRIMARY KEY, digest TEXT NOT NULL REFERENCES content_objects(digest), size INTEGER NOT NULL,
	media_type TEXT NOT NULL DEFAULT '', source TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS content_grants (
	reference_id TEXT NOT NULL REFERENCES content_references(id), root_id TEXT NOT NULL REFERENCES sessions(id),
	agent_id TEXT NOT NULL DEFAULT '', scope TEXT NOT NULL CHECK(scope IN ('root','agent','subtree')),
	created_at TEXT NOT NULL, revoked_at TEXT NOT NULL DEFAULT '', PRIMARY KEY(reference_id, root_id, agent_id, scope)
);
CREATE TABLE IF NOT EXISTS commands (
	client_id TEXT NOT NULL, command_id TEXT NOT NULL, scope TEXT NOT NULL CHECK(scope IN ('daemon','root')),
	root_id TEXT REFERENCES sessions(id), request_digest TEXT NOT NULL, status TEXT NOT NULL,
	payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id), outcome_inline BLOB,
	outcome_ref TEXT REFERENCES content_references(id), ingress_seq INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL, PRIMARY KEY(client_id, command_id),
	CHECK((scope='daemon' AND root_id IS NULL) OR (scope='root' AND root_id IS NOT NULL)),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	CHECK(NOT(outcome_inline IS NOT NULL AND outcome_ref IS NOT NULL))
);
CREATE TABLE IF NOT EXISTS turns (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	FOREIGN KEY(root_id, agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS child_executions (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), parent_agent_id TEXT NOT NULL,
	child_agent_id TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	FOREIGN KEY(root_id, parent_agent_id) REFERENCES agents(root_id, id),
	FOREIGN KEY(root_id, child_agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS events (
	root_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, kind TEXT NOT NULL, payload_inline BLOB,
	payload_ref TEXT REFERENCES content_references(id), created_at TEXT NOT NULL, PRIMARY KEY(root_id, seq),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL))
);
CREATE TABLE IF NOT EXISTS inbox (
	root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL, seq INTEGER NOT NULL,
	kind TEXT NOT NULL, status TEXT NOT NULL, payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id),
	created_at TEXT NOT NULL, PRIMARY KEY(root_id, agent_id, seq),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	FOREIGN KEY(root_id, agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS agent_state (
	root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL, key TEXT NOT NULL,
	version INTEGER NOT NULL, author_agent_id TEXT NOT NULL, payload_inline BLOB,
	payload_ref TEXT REFERENCES content_references(id), updated_at TEXT NOT NULL, PRIMARY KEY(root_id, agent_id, key),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	FOREIGN KEY(root_id, agent_id) REFERENCES agents(root_id, id),
	FOREIGN KEY(root_id, author_agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS capabilities (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	issuer_agent_id TEXT NOT NULL DEFAULT '', operations TEXT NOT NULL, scopes TEXT NOT NULL, generation INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	FOREIGN KEY(root_id, agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS budgets (
	root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL,
	limit_value INTEGER NOT NULL, used_value INTEGER NOT NULL DEFAULT 0, reserved_value INTEGER NOT NULL DEFAULT 0,
	updated_at TEXT NOT NULL, PRIMARY KEY(root_id, agent_id, kind)
);
CREATE TABLE IF NOT EXISTS operations (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	command_client_id TEXT NOT NULL DEFAULT '', command_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
	payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id), result_inline BLOB,
	result_ref TEXT REFERENCES content_references(id), created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	UNIQUE(root_id, id),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	CHECK(NOT(result_inline IS NOT NULL AND result_ref IS NOT NULL)),
	FOREIGN KEY(root_id, agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS leases (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	operation_id TEXT NOT NULL, capability_id TEXT NOT NULL DEFAULT '', trace_id TEXT NOT NULL DEFAULT '',
	command_client_id TEXT NOT NULL DEFAULT '', command_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
	created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	FOREIGN KEY(root_id, agent_id) REFERENCES agents(root_id, id),
	FOREIGN KEY(root_id, operation_id) REFERENCES operations(root_id, id)
);
CREATE TABLE IF NOT EXISTS blackboard (
	root_id TEXT NOT NULL REFERENCES sessions(id), key TEXT NOT NULL, version INTEGER NOT NULL, author_agent_id TEXT NOT NULL,
	payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id), updated_at TEXT NOT NULL, PRIMARY KEY(root_id, key),
	CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	FOREIGN KEY(root_id, author_agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS blackboard_history (
	root_id TEXT NOT NULL REFERENCES sessions(id), key TEXT NOT NULL, version INTEGER NOT NULL, author_agent_id TEXT NOT NULL,
	payload_inline BLOB, payload_ref TEXT REFERENCES content_references(id), created_at TEXT NOT NULL,
	PRIMARY KEY(root_id, key, version), CHECK(NOT(payload_inline IS NOT NULL AND payload_ref IS NOT NULL)),
	FOREIGN KEY(root_id, key) REFERENCES blackboard(root_id, key),
	FOREIGN KEY(root_id, author_agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS subscriptions (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	key TEXT NOT NULL, cursor INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	FOREIGN KEY(root_id, agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS permission_requests (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL,
	operation_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, request_inline BLOB,
	request_ref TEXT REFERENCES content_references(id), created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
	CHECK(NOT(request_inline IS NOT NULL AND request_ref IS NOT NULL)),
	FOREIGN KEY(root_id, agent_id) REFERENCES agents(root_id, id)
);
CREATE TABLE IF NOT EXISTS usage_charges (
	id TEXT PRIMARY KEY, root_id TEXT NOT NULL REFERENCES sessions(id), agent_id TEXT NOT NULL DEFAULT '',
	command_client_id TEXT NOT NULL DEFAULT '', command_id TEXT NOT NULL DEFAULT '', input_tokens INTEGER NOT NULL DEFAULT 0,
	cached_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0, cost_micros INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS content_orphans (
	digest TEXT PRIMARY KEY, size INTEGER NOT NULL, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL
);`

type migration struct {
	version int
	apply   func(context.Context, *sql.Conn) error
}

var migrations = []migration{{version: 1, apply: migrateVersionOne}}

func migrate(ctx context.Context, db *sql.DB, path string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var version int
	if err := conn.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("sessions database schema %d is newer than supported version %d", version, currentSchemaVersion)
	}
	var objects int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'`).Scan(&objects); err != nil {
		return err
	}
	if version < currentSchemaVersion && objects > 0 {
		if err := checkpointWAL(ctx, conn); err != nil {
			return fmt.Errorf("checkpoint before migration: %w", err)
		}
		if err := backupDatabase(ctx, conn, path, version); err != nil {
			return fmt.Errorf("backup before migration: %w", err)
		}
	}
	for _, m := range migrations {
		if m.version <= version {
			continue
		}
		if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
			return err
		}
		if err := m.apply(ctx, conn); err != nil {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
			return fmt.Errorf("migrate schema to version %d: %w", m.version, err)
		}
		if _, err := conn.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version=%d`, m.version)); err != nil {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
			return err
		}
		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
			return err
		}
		version = m.version
	}
	return nil
}

func migrateVersionOne(ctx context.Context, conn *sql.Conn) error {
	if _, err := conn.ExecContext(ctx, sessionsSchema); err != nil {
		return err
	}
	columns, err := validateTable(ctx, conn, "sessions",
		[]string{"id", "created_at", "updated_at", "cwd", "model", "provider", "title"}, []string{"id"})
	if err != nil {
		return err
	}
	if columns["title"].defaultValue.String != "''" {
		return fmt.Errorf("incompatible sessions table: title must default to empty text")
	}
	for _, column := range sessionColumns {
		if _, ok := columns[column.name]; ok {
			continue
		}
		if _, err := conn.ExecContext(ctx, `ALTER TABLE sessions ADD COLUMN `+column.definition); err != nil {
			return err
		}
	}
	if err := validateVersionZeroTables(ctx, conn); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, runtimeSchema)
	return err
}

func validateVersionZeroTables(ctx context.Context, conn *sql.Conn) error {
	historical := []struct {
		name     string
		required []string
		primary  []string
	}{
		{"messages", []string{"session_id", "seq", "role", "content"}, []string{"session_id", "seq"}},
		{"tasks", []string{"session_id", "task_id", "description", "prompt", "status", "report", "started_at", "ended_at"}, []string{"session_id", "task_id"}},
		{"snapshots", []string{"session_id", "seq", "ref", "created_at"}, []string{"session_id", "seq"}},
		{"schedules", []string{"session_id", "id", "schedule", "prompt", "anchor", "last_fire", "created_at"}, []string{"session_id", "id"}},
		{"compactions", []string{"session_id", "seq", "cutoff", "summary", "created_at"}, []string{"session_id", "seq"}},
	}
	for _, table := range historical {
		columns, err := validateTable(ctx, conn, table.name, table.required, table.primary)
		if err != nil {
			return err
		}
		if len(columns) == 0 {
			continue
		}
	}
	for _, name := range []string{
		"agents", "content_objects", "content_references", "content_grants", "commands", "turns", "child_executions",
		"events", "inbox", "agent_state", "capabilities", "budgets", "operations", "leases", "blackboard",
		"blackboard_history", "subscriptions", "permission_requests", "usage_charges", "content_orphans",
	} {
		var objectType string
		err := conn.QueryRowContext(ctx, `SELECT type FROM sqlite_schema WHERE name=?`, name).Scan(&objectType)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("incompatible version-zero database: %s %q already exists", objectType, name)
	}
	return nil
}

type tableColumn struct {
	primaryKey   int
	defaultValue sql.NullString
}

func validateTable(ctx context.Context, conn *sql.Conn, table string, required, primaryKey []string) (map[string]tableColumn, error) {
	var objectType string
	err := conn.QueryRowContext(ctx, `SELECT type FROM sqlite_schema WHERE name=?`, table).Scan(&objectType)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if objectType != "table" {
		return nil, fmt.Errorf("incompatible %s object: expected table, got %s", table, objectType)
	}
	rows, err := conn.QueryContext(ctx, `SELECT name,pk,dflt_value FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns := map[string]tableColumn{}
	for rows.Next() {
		var name string
		var column tableColumn
		if err := rows.Scan(&name, &column.primaryKey, &column.defaultValue); err != nil {
			return nil, err
		}
		columns[name] = column
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, name := range required {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("incompatible %s table: missing %s", table, name)
		}
	}
	actualPrimaryKey := make([]string, len(primaryKey))
	primaryKeyColumns := 0
	for name, column := range columns {
		if column.primaryKey == 0 {
			continue
		}
		primaryKeyColumns++
		if column.primaryKey <= len(actualPrimaryKey) {
			actualPrimaryKey[column.primaryKey-1] = name
		}
	}
	if primaryKeyColumns != len(primaryKey) {
		return nil, fmt.Errorf("incompatible %s table: primary key must be %v", table, primaryKey)
	}
	for i, name := range primaryKey {
		if actualPrimaryKey[i] != name {
			return nil, fmt.Errorf("incompatible %s table: primary key must be %v", table, primaryKey)
		}
	}
	return columns, nil
}

func checkpointWAL(ctx context.Context, conn *sql.Conn) error {
	var busy, pages, checkpointed int
	if err := conn.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &pages, &checkpointed); err != nil {
		return err
	}
	if busy != 0 {
		return fmt.Errorf("WAL checkpoint busy (%d of %d pages checkpointed)", checkpointed, pages)
	}
	return nil
}

func migrationBackupPath(path string, version int) string {
	return fmt.Sprintf("%s.pre-v%d.bak", path, version)
}

func backupDatabase(ctx context.Context, conn *sql.Conn, path string, version int) error {
	target := migrationBackupPath(path, version)
	tmp := target + ".tmp"
	_ = os.Remove(tmp)
	type backuper interface {
		NewBackup(string) (*sqlite.Backup, error)
	}
	if err := conn.Raw(func(driverConn any) error {
		b, ok := driverConn.(backuper)
		if !ok {
			return fmt.Errorf("sqlite driver does not support online backup")
		}
		backup, err := b.NewBackup(tmp)
		if err != nil {
			return err
		}
		for more := true; more; {
			more, err = backup.Step(-1)
			if err != nil {
				_ = backup.Finish()
				return err
			}
		}
		return backup.Finish()
	}); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	f, err := os.Open(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	err = f.Sync()
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	dir, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr = dir.Close()
	if err == nil {
		err = closeErr
	}
	return err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
