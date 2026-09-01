package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

type historicalShape struct {
	name                              string
	goal, tasks, forks, effort, todos bool
	snapshots, compactions, schedules bool
	taskID                            bool
}

func historicalShapes() []historicalShape {
	return []historicalShape{
		{name: "H0"},
		{name: "H1", goal: true},
		{name: "H2a", goal: true, tasks: true},
		{name: "H2b", goal: true, forks: true},
		{name: "H3", goal: true, tasks: true, forks: true},
		{name: "effort-usage", goal: true, tasks: true, forks: true, effort: true},
		{name: "todos", goal: true, tasks: true, forks: true, effort: true, todos: true},
		{name: "snapshots", goal: true, tasks: true, forks: true, effort: true, todos: true, snapshots: true},
		{name: "compactions", goal: true, tasks: true, forks: true, effort: true, todos: true, snapshots: true, compactions: true},
		{name: "schedules", goal: true, tasks: true, forks: true, effort: true, todos: true, snapshots: true, compactions: true, schedules: true},
		{name: "task-id", goal: true, tasks: true, forks: true, effort: true, todos: true, snapshots: true, compactions: true, schedules: true, taskID: true},
	}
}

const historicalMessage = `{"role":"user","content":"preserve  bytes","authored":true}`

func createHistoricalStore(t *testing.T, path string, shape historicalShape) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	cols := []string{
		"id TEXT PRIMARY KEY", "created_at TEXT NOT NULL", "updated_at TEXT NOT NULL",
		"cwd TEXT NOT NULL", "model TEXT NOT NULL", "provider TEXT NOT NULL", "title TEXT NOT NULL DEFAULT ''",
	}
	if shape.goal {
		cols = append(cols, "goal TEXT NOT NULL DEFAULT ''")
	}
	if shape.forks {
		cols = append(cols,
			"forked_from TEXT NOT NULL DEFAULT ''", "fork_seq INTEGER NOT NULL DEFAULT 0",
			"tags TEXT NOT NULL DEFAULT ''", "pinned INTEGER NOT NULL DEFAULT 0")
	}
	if shape.effort {
		cols = append(cols,
			"effort TEXT NOT NULL DEFAULT ''", "usage_in INTEGER NOT NULL DEFAULT 0",
			"usage_cached INTEGER NOT NULL DEFAULT 0", "usage_out INTEGER NOT NULL DEFAULT 0")
	}
	if shape.todos {
		cols = append(cols, "todos TEXT NOT NULL DEFAULT ''")
	}
	if shape.taskID {
		cols = append(cols, "task_id TEXT NOT NULL DEFAULT ''")
	}
	schema := `CREATE TABLE sessions (` + strings.Join(cols, ",") + `);
		CREATE TABLE messages (session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, role TEXT NOT NULL, content TEXT NOT NULL, PRIMARY KEY(session_id,seq));`
	if shape.tasks {
		schema += `CREATE TABLE tasks (session_id TEXT NOT NULL REFERENCES sessions(id), task_id TEXT NOT NULL, description TEXT NOT NULL, prompt TEXT NOT NULL, status TEXT NOT NULL, report TEXT NOT NULL DEFAULT '', started_at TEXT NOT NULL, ended_at TEXT NOT NULL DEFAULT '', PRIMARY KEY(session_id,task_id));`
	}
	if shape.snapshots {
		schema += `CREATE TABLE snapshots (session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, ref TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(session_id,seq));`
	}
	if shape.compactions {
		schema += `CREATE TABLE compactions (session_id TEXT NOT NULL REFERENCES sessions(id), seq INTEGER NOT NULL, cutoff INTEGER NOT NULL, summary TEXT NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(session_id,seq));`
	}
	if shape.schedules {
		schema += `CREATE TABLE schedules (session_id TEXT NOT NULL REFERENCES sessions(id), id INTEGER NOT NULL, schedule TEXT NOT NULL, prompt TEXT NOT NULL, anchor TEXT NOT NULL, last_fire TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, PRIMARY KEY(session_id,id));`
	}
	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO sessions(id,created_at,updated_at,cwd,model,provider,title) VALUES('legacy','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z','/old','m','p','old title')`); err != nil {
		t.Fatal(err)
	}
	updates := []string{}
	if shape.goal {
		updates = append(updates, `goal='legacy goal'`)
	}
	if shape.forks {
		updates = append(updates, `forked_from='parent',fork_seq=7,tags='one,two',pinned=1`)
	}
	if shape.effort {
		updates = append(updates, `effort='high',usage_in=101,usage_cached=20,usage_out=9`)
	}
	if shape.todos {
		updates = append(updates, `todos='[{"content":"keep"}]'`)
	}
	if shape.taskID {
		updates = append(updates, `task_id='child-7'`)
	}
	if len(updates) > 0 {
		if _, err := db.ExecContext(context.Background(), `UPDATE sessions SET `+strings.Join(updates, ",")+` WHERE id='legacy'`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(context.Background(), `INSERT INTO messages VALUES('legacy',1,'user',?),('legacy',3,'assistant','{"role":"assistant","content":"answer"}')`, historicalMessage); err != nil {
		t.Fatal(err)
	}
	if shape.tasks {
		_, err = db.ExecContext(context.Background(), `INSERT INTO tasks VALUES('legacy','t1','desc','prompt','done','report','2026-01-01T00:00:00Z','2026-01-01T00:01:00Z')`)
	}
	if err == nil && shape.snapshots {
		_, err = db.ExecContext(context.Background(), `INSERT INTO snapshots VALUES('legacy',3,'stash@{2}','2026-01-01T00:00:00Z')`)
	}
	if err == nil && shape.compactions {
		_, err = db.ExecContext(context.Background(), `INSERT INTO compactions VALUES('legacy',1,2,'summary','2026-01-01T00:00:00Z')`)
	}
	if err == nil && shape.schedules {
		_, err = db.ExecContext(context.Background(), `INSERT INTO schedules VALUES('legacy',4,'@every 1h','wake','2026-01-01T00:00:00Z','','2026-01-01T00:00:00Z')`)
	}
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestHistoricalVersionZeroShapesMigrateWithoutChangingHistory(t *testing.T) {
	for _, shape := range historicalShapes() {
		t.Run(shape.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			db := createHistoricalStore(t, path, shape)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}

			st, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			assertHistoricalStore(t, st, shape)
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			backup := migrationBackupPath(path, 0)
			before, err := os.Stat(backup)
			if err != nil {
				t.Fatalf("pre-migration backup: %v", err)
			}

			st, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			assertHistoricalStore(t, st, shape)
			st.Close()
			after, err := os.Stat(backup)
			if err != nil || !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
				t.Fatalf("reopen should not replace migration backup: before=%v after=%v err=%v", before, after, err)
			}
		})
	}
}

func assertHistoricalStore(t *testing.T, st *Store, shape historicalShape) {
	t.Helper()
	var version int
	if err := st.db.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&version); err != nil || version != currentSchemaVersion {
		t.Fatalf("user_version = %d, err %v", version, err)
	}
	meta, msgs, err := st.Load("legacy")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Mode != ModeClassic {
		t.Fatalf("migrated session mode = %q", meta.Mode)
	}
	if len(msgs) != 2 || msgs[0].Content != "preserve  bytes" {
		t.Fatalf("history changed: %+v", msgs)
	}
	if shape.compactions && !strings.Contains(msgs[1].Content, "summary") || !shape.compactions && msgs[1].Content != "answer" {
		t.Fatalf("visible compaction history changed: %+v", msgs)
	}
	if shape.goal && meta.Goal != "legacy goal" || !shape.goal && meta.Goal != "" {
		t.Fatalf("goal = %q", meta.Goal)
	}
	if shape.forks && (meta.ForkedFrom != "parent" || meta.ForkSeq != 7 || !meta.Pinned || strings.Join(meta.Tags, ",") != "one,two") {
		t.Fatalf("fork metadata changed: %+v", meta)
	}
	if shape.effort && (meta.Effort != "high" || meta.UsageIn != 101 || meta.UsageCached != 20 || meta.UsageOut != 9) {
		t.Fatalf("usage metadata changed: %+v", meta)
	}
	var seqs, raw string
	if err := st.db.QueryRowContext(context.Background(), `SELECT group_concat(seq), group_concat(content,'|') FROM messages WHERE session_id='legacy' ORDER BY seq`).Scan(&seqs, &raw); err != nil {
		t.Fatal(err)
	}
	if seqs != "1,3" || !strings.HasPrefix(raw, historicalMessage+"|") {
		t.Fatalf("message keys or JSON bytes changed: seqs=%q raw=%q", seqs, raw)
	}
	checks := []struct {
		present bool
		query   string
		want    string
	}{
		{shape.tasks, `SELECT report FROM tasks WHERE task_id='t1'`, "report"},
		{shape.snapshots, `SELECT ref FROM snapshots WHERE seq=3`, "stash@{2}"},
		{shape.compactions, `SELECT summary FROM compactions WHERE seq=1`, "summary"},
		{shape.schedules, `SELECT prompt FROM schedules WHERE id=4`, "wake"},
	}
	for _, check := range checks {
		if !check.present {
			continue
		}
		var got string
		if err := st.db.QueryRowContext(context.Background(), check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("%s = %q, err %v, want %q", check.query, got, err, check.want)
		}
	}
}

func TestFreshStoreAppliesVersionedSchemaOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	if err := st.db.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&version); err != nil || version != currentSchemaVersion {
		t.Fatalf("version = %d, err %v", version, err)
	}
	for _, table := range []string{"sessions", "commands", "agents", "turns", "child_executions", "operations", "leases", "content_objects", "content_references", "content_grants"} {
		var n int
		if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s missing: count=%d err=%v", table, n, err)
		}
	}
	st.Close()
	if _, err := os.Stat(migrationBackupPath(path, 0)); !os.IsNotExist(err) {
		t.Fatalf("fresh database should not create a pre-migration backup: %v", err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.db.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&version); err != nil || version != currentSchemaVersion {
		t.Fatalf("reopen version = %d, err %v", version, err)
	}
}

func TestUnknownFutureSchemaVersionIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), fmt.Sprintf(`PRAGMA user_version=%d`, currentSchemaVersion+1)); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("future schema error = %v", err)
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var journalMode string
	if err := db.QueryRowContext(context.Background(), `PRAGMA journal_mode`).Scan(&journalMode); err != nil || journalMode != "delete" {
		t.Fatalf("rejected future schema changed journal mode to %q, err %v", journalMode, err)
	}
}

func TestUnknownVersionZeroTableCollisionIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db := createHistoricalStore(t, path, historicalShape{name: "H0"})
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE events(unrelated TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), `table "events" already exists`) {
		t.Fatalf("runtime table collision error = %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&version); err != nil || version != 0 {
		t.Fatalf("rejected database version=%d err=%v", version, err)
	}
	var value string
	if err := db.QueryRowContext(context.Background(), `SELECT unrelated FROM events`).Scan(&value); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rejected database changed colliding table: %v", err)
	}
}

func TestUnknownVersionZeroHistoricalShapeIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `CREATE TABLE sessions (
		id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, cwd TEXT NOT NULL,
		model TEXT NOT NULL, provider TEXT NOT NULL, title TEXT NOT NULL DEFAULT ''
	);
	CREATE TABLE messages (session_id TEXT, seq INTEGER, role TEXT, content TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "primary key") {
		t.Fatalf("malformed historical table error = %v", err)
	}
}

func TestVersionZeroCompatibilityErrors(t *testing.T) {
	base := `id TEXT PRIMARY KEY, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, cwd TEXT NOT NULL, model TEXT NOT NULL, provider TEXT NOT NULL`
	for _, test := range []struct {
		name   string
		schema string
		want   string
	}{
		{name: "missing title", schema: `CREATE TABLE sessions (` + base + `)`, want: "missing title"},
		{name: "title default", schema: `CREATE TABLE sessions (` + base + `, title TEXT NOT NULL DEFAULT 'legacy')`, want: "title must default"},
		{name: "historical view", schema: `CREATE TABLE sessions (` + base + `, title TEXT NOT NULL DEFAULT ''); CREATE VIEW messages AS SELECT '' AS session_id, 0 AS seq, '' AS role, '' AS content`, want: "expected table"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(context.Background(), test.schema); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if st, err := Open(path); err == nil {
				st.Close()
				t.Fatal("incompatible version-zero database was accepted")
			} else if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("compatibility error=%v, want %q", err, test.want)
			}
		})
	}
}

func TestMigrationBackupRefusesOccupiedDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db := createHistoricalStore(t, path, historicalShape{name: "H0"})
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	target := migrationBackupPath(path, 0)
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "keep"), []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if st, err := Open(path); err == nil {
		st.Close()
		t.Fatal("migration should fail when its backup destination is occupied")
	} else if !strings.Contains(err.Error(), "backup before migration") {
		t.Fatalf("backup failure error=%v", err)
	}
	if _, err := os.Stat(target + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("failed backup left temporary file: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&version); err != nil || version != 0 {
		t.Fatalf("backup failure changed source version=%d err=%v", version, err)
	}
}
