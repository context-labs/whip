package session

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDaemonGenerationIsDurable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if generation, err := store.BeginDaemonGeneration(ctx, "build-a"); err != nil || generation != 1 {
		t.Fatalf("first generation = %d, %v", generation, err)
	}
	if generation, err := store.BeginDaemonGeneration(ctx, "build-b"); err != nil || generation != 2 {
		t.Fatalf("second generation = %d, %v", generation, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if generation, buildID, status, err := store.DaemonGeneration(ctx); err != nil || generation != 2 || buildID != "build-b" || status != "running" {
		t.Fatalf("durable daemon state = %d %q %q, %v", generation, buildID, status, err)
	}
	if err := store.SetDaemonStatus(ctx, 1, "stopping"); err == nil {
		t.Fatal("stale generation changed daemon state")
	}
	if _, err := store.BeginDaemonGeneration(ctx, ""); err == nil {
		t.Fatal("empty build ID was accepted")
	}
}

func TestFormerApprovalTableDoesNotRequireDatabaseReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	// Existing schema-7 databases can retain the unused approval table.
	if _, err := store.db.Exec(`CREATE TABLE client_identities (
		client_id TEXT PRIMARY KEY, kind TEXT NOT NULL, public_key BLOB NOT NULL,
		paired_by TEXT NOT NULL, created_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	meta, _, err := store.Load(id)
	if err != nil || meta.ID != id {
		t.Fatalf("existing session = %+v, %v", meta, err)
	}
}

func TestRuntimeIDAndCommandOperationSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, err := store.RuntimeID(t.Context())
	if err != nil || id == "" {
		t.Fatalf("ID=%q,%v", id, err)
	}
	admission := CommandAdmission{ClientID: "client", CommandID: "command", Scope: CommandScopeDaemon, Operation: "session.create", RequestDigest: "digest"}
	if _, err := store.AdmitCommand(t.Context(), admission); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BeginDaemonGeneration(t.Context(), "one"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.BeginDaemonGeneration(t.Context(), "two"); err != nil {
		t.Fatal(err)
	}
	if actual, err := store.RuntimeID(t.Context()); err != nil || actual != id {
		t.Fatalf("ID=%q,%v", actual, err)
	}
	record, err := store.LoadCommand(t.Context(), "client", "command")
	if err != nil || record.Operation != admission.Operation {
		t.Fatalf("record=%+v,%v", record, err)
	}
	admission.Operation = "session.delete"
	if _, err := store.AdmitCommand(t.Context(), admission); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("changed operation=%v", err)
	}
}
