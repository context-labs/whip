package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

// versionFifteenDatabase upgrades the version-ten fixture to the last schema
// without agent definitions.
func versionFifteenDatabase(t *testing.T) (string, *sql.DB) {
	t.Helper()
	path, db := versionTwelveDatabase(t)
	conn, err := db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, upgrade := range []func(*sql.Conn) error{
		func(conn *sql.Conn) error { return upgradeV12(t.Context(), conn) },
		func(conn *sql.Conn) error { return upgradeV13(t.Context(), conn) },
		func(conn *sql.Conn) error { return upgradeV14(t.Context(), conn) },
	} {
		if err := upgrade(conn); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	return path, db
}

func TestVersionFifteenUpgradeDefaultsDefinitionToCoding(t *testing.T) {
	path, db := versionFifteenDatabase(t)
	var version int
	if err := db.QueryRowContext(t.Context(), `PRAGMA user_version`).Scan(&version); err != nil || version != 15 {
		t.Fatalf("fixture version=%d error=%v", version, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		store, err := Open(path)
		if err != nil {
			t.Fatalf("open %d: %v", attempt, err)
		}
		meta, _, err := store.Load("saved-root")
		if err != nil || meta.Definition != "coding" || meta.ExecutionEngine != "starlark" || meta.Title != "Saved title" {
			t.Fatalf("open %d meta=%+v error=%v", attempt, meta, err)
		}
		var identity string
		if err := store.db.QueryRowContext(t.Context(), `SELECT identity FROM runtime_schema WHERE id=1`).Scan(&identity); err != nil || identity != schemaIdentity {
			t.Fatalf("identity=%q error=%v", identity, err)
		}
		if err := store.db.QueryRowContext(t.Context(), `PRAGMA user_version`).Scan(&version); err != nil || version != currentSchemaVersion {
			t.Fatalf("version=%d error=%v", version, err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func createDefinitionSession(t *testing.T, store *Store, definition string) (string, error) {
	t.Helper()
	id := NewAgentID()
	if _, err := store.AdmitCommand(t.Context(), CommandAdmission{ClientID: "definitions", CommandID: id, Scope: CommandScopeDaemon, RequestDigest: id}); err != nil {
		t.Fatal(err)
	}
	record, err := store.CreateSessionForCommandWithDefinition(t.Context(), "definitions", id, SessionKindAgent, t.TempDir(), "model", "provider", "", "", definition)
	if err != nil {
		return "", err
	}
	var result struct {
		RootID string `json:"root_id"`
	}
	if err := json.Unmarshal(record.Outcome.Inline, &result); err != nil {
		t.Fatal(err)
	}
	return result.RootID, nil
}

func TestSessionDefinitionPersistsThroughForkAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "definitions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := createDefinitionSession(t, store, "junior-developer")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createDefinitionSession(t, store, ""); err == nil {
		t.Fatal("agent session created without a definition")
	}
	legacy, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	fork, err := store.Fork(root, 0, "fork")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for id, want := range map[string]string{root: "junior-developer", fork: "junior-developer", legacy: "coding"} {
		meta, _, err := store.Load(id)
		if err != nil || meta.Definition != want {
			t.Fatalf("%s definition=%q want %q error=%v", id, meta.Definition, want, err)
		}
	}
	forks, err := store.ForksOf(root)
	if err != nil || len(forks) != 1 || forks[0].Definition != "junior-developer" {
		t.Fatalf("forks=%+v error=%v", forks, err)
	}
}

// Root grants come from the definition's capabilities at first bootstrap and
// are never widened afterwards.
func TestRootGrantsFollowDefinitionAtBootstrap(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "grants.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := store.Create(SessionKindAgent, cwd, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureRootAuthority(t.Context(), narrow, RootGrants{Files: []string{"read"}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(cwd, "notes.txt")
	if err := store.AuthorizeCapability(t.Context(), narrow, narrow, authority.Files, "read", file); err != nil {
		t.Fatalf("read denied: %v", err)
	}
	if err := store.AuthorizeCapability(t.Context(), narrow, narrow, authority.Files, "write", file); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("write allowed without the write capability: %v", err)
	}
	if err := store.AuthorizeCapability(t.Context(), narrow, narrow, authority.Shell, "bash", ""); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("bash allowed without the shell capability: %v", err)
	}
	if err := store.AuthorizeMCP(t.Context(), narrow, narrow, authority.MCP, capability.MCPSelector{Server: "docs", Tool: "search"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("mcp allowed without the mcp capability: %v", err)
	}
	// Reopening with full grants must not widen a root that was bootstrapped narrow.
	reopened, err := store.EnsureAuthority(t.Context(), narrow)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeCapability(t.Context(), narrow, narrow, reopened.Files, "write", file); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("reopen widened the root: %v", err)
	}
	full, err := store.Create(SessionKindAgent, cwd, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	fullAuthority, err := store.EnsureRootAuthority(t.Context(), full, FullRootGrants())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AuthorizeCapability(t.Context(), full, full, fullAuthority.Files, "write", file); err != nil {
		t.Fatalf("full root denied write: %v", err)
	}
	if err := store.AuthorizeCapability(t.Context(), full, full, fullAuthority.Shell, "bash", ""); err != nil {
		t.Fatalf("full root denied bash: %v", err)
	}
}
