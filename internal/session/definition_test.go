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
	// The chain continues through schema 17: registered definitions and pinned revisions.
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
		if err != nil || meta.Definition != "coding" || meta.DefinitionRevision != "" || meta.ExecutionEngine != "starlark" || meta.Title != "Saved title" {
			t.Fatalf("open %d meta=%+v error=%v", attempt, meta, err)
		}
		if _, err := store.ListDefinitions(t.Context()); err != nil {
			t.Fatalf("definitions table missing after upgrade: %v", err)
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
	record, err := store.CreateSessionForCommandWithDefinition(t.Context(), "definitions", id, SessionKindAgent, t.TempDir(), "model", "provider", "", "", definition, "")
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

func TestRegisteredDefinitionsAreIdempotentAndPinned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.RegisterDefinition(t.Context(), "support-bot", "rev-a", []byte(`{"id":"support-bot"}`), "app")
	if err != nil || !created {
		t.Fatalf("first registration created=%v error=%v", created, err)
	}
	created, err = store.RegisterDefinition(t.Context(), "support-bot", "rev-a", []byte(`{"id":"support-bot"}`), "other")
	if err != nil || created {
		t.Fatalf("repeat registration created=%v error=%v", created, err)
	}
	if _, err := store.RegisterDefinition(t.Context(), "", "rev", []byte(`{}`), "app"); err == nil {
		t.Fatal("empty id accepted")
	}
	record, err := store.LoadDefinition(t.Context(), "support-bot", "rev-a")
	if err != nil || record.RegisteredBy != "app" || string(record.Body) != `{"id":"support-bot"}` || record.CreatedAt.IsZero() {
		t.Fatalf("record=%+v error=%v", record, err)
	}
	if _, err := store.LoadDefinition(t.Context(), "support-bot", "missing"); !errors.Is(err, ErrNoDefinition) {
		t.Fatalf("missing revision error=%v", err)
	}
	id := NewAgentID()
	if _, err := store.AdmitCommand(t.Context(), CommandAdmission{ClientID: "definitions", CommandID: id, Scope: CommandScopeDaemon, RequestDigest: id}); err != nil {
		t.Fatal(err)
	}
	created2, err := store.CreateSessionForCommandWithDefinition(t.Context(), "definitions", id, SessionKindAgent, t.TempDir(), "model", "provider", "", "", "support-bot", "rev-a")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		RootID string `json:"root_id"`
	}
	if err := json.Unmarshal(created2.Outcome.Inline, &result); err != nil {
		t.Fatal(err)
	}
	// A later registration becomes the latest without moving the pinned session.
	if _, err := store.RegisterDefinition(t.Context(), "support-bot", "rev-b", []byte(`{"id":"support-bot","v":2}`), "app"); err != nil {
		t.Fatal(err)
	}
	latest, err := store.LatestDefinition(t.Context(), "support-bot")
	if err != nil || latest.Revision != "rev-b" {
		t.Fatalf("latest=%+v error=%v", latest, err)
	}
	if _, err := store.EnsureAuthority(t.Context(), result.RootID); err != nil {
		t.Fatal(err)
	}
	fork, err := store.Fork(result.RootID, 0, "fork")
	if err != nil {
		t.Fatal(err)
	}
	for _, sessionID := range []string{result.RootID, fork} {
		meta, _, err := store.Load(sessionID)
		if err != nil || meta.Definition != "support-bot" || meta.DefinitionRevision != "rev-a" {
			t.Fatalf("%s meta=%+v error=%v", sessionID, meta, err)
		}
	}
	list, err := store.ListDefinitions(t.Context())
	if err != nil || len(list) != 1 || list[0].Revision != "rev-b" {
		t.Fatalf("list=%+v error=%v", list, err)
	}
	if _, err := store.RegisterDefinition(t.Context(), "another", "rev-z", []byte(`{}`), "app"); err != nil {
		t.Fatal(err)
	}
	if list, err = store.ListDefinitions(t.Context()); err != nil || len(list) != 2 || list[0].ID != "another" || list[1].ID != "support-bot" {
		t.Fatalf("list=%+v error=%v", list, err)
	}
}
