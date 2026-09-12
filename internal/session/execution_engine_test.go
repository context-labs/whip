package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func createEngineSession(t *testing.T, store *Store, engine string) string {
	t.Helper()
	id := NewAgentID()
	if _, err := store.AdmitCommand(t.Context(), CommandAdmission{ClientID: "engines", CommandID: id, Scope: CommandScopeDaemon, RequestDigest: id}); err != nil {
		t.Fatal(err)
	}
	record, err := store.CreateSessionForCommandWithEngine(t.Context(), "engines", id, SessionKindAgent, t.TempDir(), "model", "provider", "", engine)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		RootID string `json:"root_id"`
	}
	if err := json.Unmarshal(record.Outcome.Inline, &result); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), result.RootID); err != nil {
		t.Fatal(err)
	}
	return result.RootID
}

func TestSessionEngineTreeForkAndRestart(t *testing.T) {
	for _, engine := range []string{"starlark", "quickjs"} {
		t.Run(engine, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "engine.db")
			store, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			root := createEngineSession(t, store, engine)
			admitTestChild(t, store, root, root, "child")
			admitTestChild(t, store, root, "child", "grandchild")
			if _, err := store.db.ExecContext(t.Context(), `UPDATE sessions SET execution_engine=? WHERE id=?`, map[string]string{"starlark": "quickjs", "quickjs": "starlark"}[engine], root); err == nil {
				t.Fatal("engine changed")
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
			for _, id := range []string{root, fork} {
				meta, _, err := store.Load(id)
				if err != nil || meta.ExecutionEngine != engine {
					t.Fatalf("meta=%+v err=%v", meta, err)
				}
			}
			for _, id := range []string{root, "child", "grandchild"} {
				value, err := store.LoadAgent(t.Context(), root, id)
				if err != nil || value.ExecutionEngine != engine {
					t.Fatalf("agent=%+v err=%v", value, err)
				}
			}
			retained, err := store.LoadRetainedAgents(t.Context(), root)
			if err != nil || len(retained) != 2 {
				t.Fatalf("retained=%+v %v", retained, err)
			}
			for _, value := range retained {
				if value.ExecutionEngine != engine {
					t.Fatalf("retained engine=%s", value.ExecutionEngine)
				}
			}
			snapshot, err := store.SnapshotRoot(t.Context(), root)
			if err != nil || snapshot.Meta.ExecutionEngine != engine {
				t.Fatalf("snapshot engine=%s %v", snapshot.Meta.ExecutionEngine, err)
			}
			for _, value := range snapshot.Agents {
				if value.ExecutionEngine != engine {
					t.Fatalf("snapshot agent engine=%s", value.ExecutionEngine)
				}
			}
		})
	}
}

func TestEngineCreationRetriesRetainSelection(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "engine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	admission := CommandAdmission{ClientID: "client", CommandID: "create", Scope: CommandScopeDaemon, RequestDigest: "create"}
	if _, err := store.AdmitCommand(t.Context(), admission); err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateSessionForCommandWithEngine(t.Context(), "client", "create", SessionKindAgent, "/tmp", "model", "provider", "", "quickjs")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateSessionForCommandWithEngine(t.Context(), "client", "create", SessionKindAgent, "/tmp", "model", "provider", "", "starlark")
	if err != nil || !bytes.Equal(first.Outcome.Inline, second.Outcome.Inline) {
		t.Fatalf("retry changed selection: %v", err)
	}
	if _, err := store.CreateSessionForCommandWithEngine(t.Context(), "client", "invalid", SessionKindAgent, "/tmp", "model", "provider", "", "node"); err == nil {
		t.Fatal("accepted unknown engine")
	}
}

func testCheckpoint(t *testing.T, root, agent, engine string, image []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(image)
	encoded, err := json.Marshal(checkpointIdentity{FormatVersion: 1, RootID: root, AgentID: agent, Engine: engine, Bytes: len(image), SHA256: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestCheckpointIntegrityOwnershipAndPublication(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "engine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := createEngineSession(t, store, "quickjs")
	image := []byte("opaque guest image")
	envelope := testCheckpoint(t, root, root, "quickjs", image)
	if err := store.SaveAgentCheckpoint(t.Context(), root, root, envelope, image); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		root, agent     string
		envelope, image []byte
	}{
		{root, "other", envelope, image},
		{root, root, testCheckpoint(t, root, root, "starlark", image), image},
		{root, root, envelope, []byte("corrupt")},
	}
	for _, c := range cases {
		if err := store.SaveAgentCheckpoint(t.Context(), c.root, c.agent, c.envelope, c.image); err == nil {
			t.Fatal("accepted invalid image")
		}
	}
	saved, body, err := store.LoadAgentCheckpoint(t.Context(), root, root)
	if err != nil || !bytes.Equal(saved, envelope) || !bytes.Equal(body, image) {
		t.Fatalf("lost last good image: %v", err)
	}
	if _, err := store.db.ExecContext(t.Context(), `UPDATE agent_checkpoints SET image='corrupt' WHERE root_id=?`, root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.LoadAgentCheckpoint(t.Context(), root, root); err == nil {
		t.Fatal("accepted corrupt retained image")
	}
}

func TestCheckpointRootQuotaAndDeletion(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "engine.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := createEngineSession(t, store, "quickjs")
	image := []byte("image")
	for i := range 7 {
		id := strings.Repeat("x", i+1)
		admitTestChild(t, store, root, root, id)
		envelope := testCheckpoint(t, root, id, "quickjs", image)
		if err := store.SaveAgentCheckpoint(t.Context(), root, id, envelope, image); err != nil {
			t.Fatal(err)
		}
		// Model occupied image capacity without allocating seven 40 MiB fixtures.
		if _, err := store.db.ExecContext(t.Context(), `UPDATE agent_checkpoints SET bytes=? WHERE root_id=? AND agent_id=?`, MaxCheckpointBytes, root, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveAgentCheckpoint(t.Context(), root, root, testCheckpoint(t, root, root, "quickjs", image), image); err == nil || !strings.Contains(err.Error(), "quota") {
		t.Fatalf("quota error=%v", err)
	}
	if err := store.DeleteSession(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := store.db.QueryRowContext(t.Context(), `SELECT count(*) FROM agent_checkpoints`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("retained deleted images: %d %v", count, err)
	}
}

func TestExecutionEngineMigrationPreservesLegacyScratch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "engine.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	root := createEngineSession(t, store, "starlark")
	if err := store.SaveAgentScratch(t.Context(), root, root, `{"memo":1}`, []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `DROP TRIGGER session_engine_immutable; DROP TABLE agent_checkpoints; DROP TABLE definitions; ALTER TABLE agents DROP COLUMN definition; ALTER TABLE sessions DROP COLUMN definition_revision; ALTER TABLE sessions DROP COLUMN definition; ALTER TABLE sessions DROP COLUMN execution_engine; UPDATE runtime_schema SET identity='whip-recursive-runtime-v14'; PRAGMA user_version=14;`); err != nil {
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
	meta, _, err := store.Load(root)
	if err != nil || meta.ExecutionEngine != "starlark" {
		t.Fatalf("legacy engine=%s %v", meta.ExecutionEngine, err)
	}
	image, _, err := store.LoadAgentScratch(t.Context(), root, root)
	if err != nil || image != `{"memo":1}` {
		t.Fatalf("legacy scratch=%s %v", image, err)
	}
}
