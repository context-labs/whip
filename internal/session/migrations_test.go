package session

import (
	"bytes"
	"context"
	"database/sql"
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
	for _, table := range []string{"sessions", "agents", "turns", "transcript_messages", "agent_messages", "inbox", "commands", "events"} {
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
