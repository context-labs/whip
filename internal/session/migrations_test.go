package session

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFreshStoreUsesOnlyRecursiveSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	var version int
	if err := store.db.QueryRowContext(t.Context(), `PRAGMA user_version`).Scan(&version); err != nil || version != currentSchemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	var identity string
	if err := store.db.QueryRowContext(t.Context(), `SELECT identity FROM runtime_schema WHERE id=1`).Scan(&identity); err != nil || identity != schemaIdentity {
		t.Fatalf("schema identity=%q err=%v", identity, err)
	}
	for _, table := range []string{"sessions", "agents", "turns", "transcript_messages", "agent_messages", "inbox", "commands", "events", "permission_rules", "model_calls"} {
		var present int
		if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?`, table).Scan(&present); err != nil || present != 1 {
			t.Fatalf("table %s count=%d err=%v", table, present, err)
		}
	}
	for _, removed := range []string{"tasks", "child_executions"} {
		var present int
		if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_schema WHERE name=?`, removed).Scan(&present); err != nil || present != 0 {
			t.Fatalf("removed table %s count=%d err=%v", removed, present, err)
		}
	}
}

func TestIncompatibleDevelopmentStoreIsRejectedWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE tasks(id TEXT); PRAGMA user_version=3`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Open(path)
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "archive or remove") {
		t.Fatalf("incompatible error=%v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected database was modified")
	}
}

func TestVersionFiveStoreIsRejectedWithoutMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `CREATE TABLE runtime_schema(id INTEGER PRIMARY KEY,identity TEXT NOT NULL);
		INSERT INTO runtime_schema VALUES(1,'whip-recursive-runtime-v5');
		CREATE TABLE compactions(session_id TEXT,seq INTEGER,cutoff INTEGER,summary TEXT,created_at TEXT);
		INSERT INTO compactions VALUES('retained',1,3,'preserve this summary',''); PRAGMA user_version=5`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("v5 database opened under current runtime")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("rejected v5 database was modified")
	}
}

func TestPhaseFourAndFiveStoresAreRejectedWithoutMutation(t *testing.T) {
	for _, prior := range []struct {
		name          string
		version       int
		scratchColumn string
	}{
		{"primary-v8", 8, "program"}, {"phase4-v8", 8, "snapshot"}, {"phase5-v9", 9, "snapshot"},
	} {
		t.Run(prior.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sessions.db")
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			query := fmt.Sprintf(`CREATE TABLE runtime_schema(id INTEGER PRIMARY KEY, identity TEXT NOT NULL);
				INSERT INTO runtime_schema VALUES(1,'whip-recursive-runtime-v%d');
				CREATE TABLE agent_scratch(%s TEXT);
				INSERT INTO agent_scratch VALUES('preserve this checkpoint'); PRAGMA user_version=%d`, prior.version, prior.scratchColumn, prior.version)
			if _, err := db.ExecContext(t.Context(), query); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if opened, err := Open(path); err == nil {
				opened.Close()
				t.Fatal("incompatible phase schema accepted")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("incompatible checkpoint was modified")
			}
		})
	}
}

func TestSessionKindsEnforceModelContract(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Create(SessionKindAgent, t.TempDir(), "", ""); err == nil {
		t.Fatal("agent session without a model was accepted")
	}
	if _, err := store.Create(SessionKindToolHost, t.TempDir(), "sentinel", "local"); err == nil {
		t.Fatal("tool host with a sentinel model was accepted")
	}
	id, err := store.Create(SessionKindToolHost, t.TempDir(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	meta, _, err := store.Load(id)
	if err != nil || meta.Kind != SessionKindToolHost || meta.Model != "" || meta.Provider != "" {
		t.Fatalf("tool-host meta=%+v err=%v", meta, err)
	}
}

// This fixture is the exact v10 schema, independent of the current initializer.
func versionTenDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	schema, err := os.ReadFile("testdata/schema_v10.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `
 INSERT INTO runtime_schema VALUES(1,'whip-recursive-runtime-v10','persistent-runtime',17);
 INSERT INTO sessions(id,kind,created_at,updated_at,cwd,model,provider,title,goal,pinned,effort,usage_in,history_revision)
 VALUES('saved-root','agent','2026-09-01T00:00:00Z','2026-09-02T00:00:00Z','/project/界','model','provider','Saved title','ship',1,'high',123,9);
 INSERT INTO messages VALUES('saved-root',1,'user','{"role":"user","content":"retained conversation"}');
 INSERT INTO schedules VALUES('saved-root',1,'@every 1h','keep working','2026-09-01T00:00:00Z','','2026-09-01T00:00:00Z');
 INSERT INTO commands(client_id,command_id,scope,root_id,operation,request_digest,status,payload_inline,outcome_inline,ingress_seq,created_at,updated_at)
 VALUES('client','saved-command','root','saved-root','session.rename','digest','succeeded','{"title":"Saved title"}','{"title":"Saved title"}',1,'','');
 PRAGMA user_version=10;`); err != nil {
		t.Fatal(err)
	}
	return path, db
}

func TestVersionTenUpgradePreservesStateAndRestarts(t *testing.T) {
	path, db := versionTenDatabase(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		store, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		metadata, err := store.SessionMetadata(t.Context(), "saved-root")
		if err != nil || metadata.Title != "Saved title" || metadata.CWD != "/project/界" || metadata.HistoryRevision != 9 || metadata.Archived != (attempt == 1) {
			t.Fatalf("metadata after open %d = %+v, %v", attempt, metadata, err)
		}
		meta, messages, err := store.Load("saved-root")
		if err != nil || meta.Goal != "ship" || !meta.Pinned || meta.Effort != "high" || meta.UsageIn != 123 || len(messages) != 1 || messages[0].Content != "retained conversation" {
			t.Fatalf("session changed during upgrade: %+v %+v %v", meta, messages, err)
		}
		var identity, runtime string
		var revision int64
		if err := store.db.QueryRowContext(t.Context(), `SELECT identity,runtime_id,catalog_revision FROM runtime_schema WHERE id=1`).Scan(&identity, &runtime, &revision); err != nil {
			t.Fatal(err)
		}
		if identity != schemaIdentity || runtime != "persistent-runtime" || revision != int64(18+attempt) {
			t.Fatalf("runtime identity/revision changed: %s %s %d", identity, runtime, revision)
		}
		command, err := store.LoadCommand(t.Context(), "client", "saved-command")
		if err != nil || command.Status != "succeeded" || string(command.Outcome.Inline) != `{"title":"Saved title"}` {
			t.Fatalf("command recovery lost: %+v %v", command, err)
		}
		if schedules := store.Schedules("saved-root"); len(schedules) != 1 || schedules[0].Prompt != "keep working" {
			t.Fatalf("schedule lost: %+v", schedules)
		}
		if attempt == 0 {
			if err := store.SetArchived(t.Context(), "saved-root", true); err != nil {
				t.Fatal(err)
			}
		}
		store.Close()
	}
}

func TestVersionTenUpgradeRollsBackAndCanRetry(t *testing.T) {
	path, db := versionTenDatabase(t)
	// Fail after ALTER TABLE, while replacing the old catalog trigger.
	if _, err := db.ExecContext(t.Context(), `DROP TRIGGER session_catalog_update`); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("upgrade with missing v10 trigger succeeded")
	}
	var version, columns int
	var identity string
	if err := db.QueryRowContext(t.Context(), `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT identity FROM runtime_schema WHERE id=1`).Scan(&identity); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM pragma_table_info('sessions') WHERE name='archived'`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if version != 10 || identity != "whip-recursive-runtime-v10" || columns != 0 {
		t.Fatalf("partial schema upgrade: version=%d identity=%s archived columns=%d", version, identity, columns)
	}
	if _, err := db.ExecContext(t.Context(), `CREATE TRIGGER session_catalog_update AFTER UPDATE OF title,model,provider,cwd,pinned,updated_at ON sessions
 BEGIN UPDATE runtime_schema SET catalog_revision=catalog_revision+1 WHERE id=1; END;`); err != nil {
		t.Fatal(err)
	}
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
}
