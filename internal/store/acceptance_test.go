package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/session"
)

func TestJournalInitializationWaitIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.db")
	reader, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	writer, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := reader.ExecContext(t.Context(), "CREATE TABLE example (id INTEGER)"); err != nil {
		t.Fatal(err)
	}
	tx, err := reader.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(t.Context(), "SELECT count(*) FROM example").Scan(&n); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := enableWAL(ctx, writer); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("journal wait: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := enableWAL(t.Context(), writer); err != nil {
		t.Fatal(err)
	}
}

func TestTreeConfigurationAndDeletionRollback(t *testing.T) {
	s := fresh(t)
	execTest(t, s, "CREATE TRIGGER fail_config BEFORE INSERT ON session_configurations BEGIN SELECT RAISE(ABORT,'injected'); END")
	_, _, ref, err := session.CanonicalDefinition(session.Builtins()[0])
	if err != nil {
		t.Fatal(err)
	}
	request := CreateTree{Engine: session.Starlark, Definition: ref, WorkingDirectory: t.TempDir(), Defaults: session.Configuration{Model: session.ModelSelection{Provider: "test", Name: "scripted"}}}
	if _, _, err := s.CreateTree(t.Context(), request); err == nil {
		t.Fatal("creation unexpectedly succeeded")
	}
	if count(t, s, "session_trees") != 0 || count(t, s, "sessions") != 0 {
		t.Fatal("rootless tree survived")
	}
	execTest(t, s, "DROP TRIGGER fail_config")
	tree, root, err := s.CreateTree(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	execTest(t, s, "CREATE TRIGGER fail_pointer BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT,'injected'); END")
	if _, err := s.UpdateConfiguration(t.Context(), root.ID, 1, session.ConfigPatch{}); err == nil {
		t.Fatal("configuration update unexpectedly succeeded")
	}
	if count(t, s, "session_configurations") != 1 {
		t.Fatal("orphan configuration revision")
	}
	execTest(t, s, "DROP TRIGGER fail_pointer")
	submit(t, s, root.ID, "queued")
	execTest(t, s, "CREATE TRIGGER fail_delete BEFORE DELETE ON sessions BEGIN SELECT RAISE(ABORT,'injected'); END")
	if err := s.DeleteSubtree(t.Context(), root.ID); err == nil {
		t.Fatal("deletion unexpectedly succeeded")
	}
	receipt, err := s.Admission(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "queued"})
	if err != nil || receipt.Receipt.DeletedAt != nil || receipt.Input == nil {
		t.Fatal("partial deletion", err)
	}
	execTest(t, s, "DROP TRIGGER fail_delete")
	title := "A new title"
	if updated, err := s.UpdateTree(t.Context(), tree.ID, 1, session.TreeMetadata{Title: &title}); err != nil || updated.Revision != 2 {
		t.Fatal("metadata update", err)
	}
	if _, err := s.UpdateTree(t.Context(), tree.ID, 1, session.TreeMetadata{}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale metadata accepted", err)
	}
}

func TestDefinitionAndHostEditsDoNotChangeRetainedSessions(t *testing.T) {
	s := fresh(t)
	directory := t.TempDir()
	host := config.Default()
	host.Providers["test"] = config.Provider{Kind: "openai-chat", BaseURL: "https://example.test"}
	host.Defaults.Model = session.ModelSelection{Provider: "test", Name: "original"}
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	document := session.DefinitionDocument{ID: "custom", Name: "Original", Defaults: session.ConfigPatch{Instructions: &session.Instructions{Text: "original instructions", ProjectFiles: []string{"AGENTS.md"}}}}
	registered, err := s.RegisterDefinition(t.Context(), document)
	if err != nil {
		t.Fatal(err)
	}
	_, root, err := s.CreateTree(t.Context(), CreateTree{Engine: host.Engine, Resources: host.Resources, Definition: registered.Ref, Defaults: host.Defaults, WorkingDirectory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	document.Defaults.Instructions.ProjectFiles[0] = "changed"
	document.Name = "Updated"
	updated, err := s.RegisterDefinition(t.Context(), document)
	if err != nil || updated.Ref == registered.Ref {
		t.Fatal("definition revision did not change", err)
	}
	host.Defaults.Model.Name = "changed"
	if err := config.Save(directory, host); err != nil {
		t.Fatal(err)
	}
	saved, err := s.Session(t.Context(), root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Config.Model.Name != "original" || saved.Definition != registered.Ref || saved.Config.Instructions.ProjectFiles[0] != "AGENTS.md" {
		t.Fatal("retained configuration changed")
	}
	saved.Config.Instructions.ProjectFiles[0] = "caller mutation"
	another, err := s.Session(t.Context(), root.ID)
	if err != nil || another.Config.Instructions.ProjectFiles[0] != "AGENTS.md" {
		t.Fatal("read aliases persisted state", err)
	}
	child, err := s.SpawnSession(t.Context(), SpawnSession{ParentID: root.ID})
	if err != nil || child.Definition != registered.Ref {
		t.Fatal("child inherited a moving definition", err)
	}
}

func TestHistoryByteBudgetAndCursor(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	submit(t, s, root.ID, "one")
	active := claim(t, s, root.ID)
	text := strings.Repeat("x", session.MaxDocumentBytes-128)
	for _, id := range []session.MessageID{"a", "b", "c", "d", "e"} {
		if _, err := s.AppendMessage(t.Context(), active.Turn.ID, session.MessageDraft{ID: id, Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: text}}}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.History(t.Context(), root.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) <= 1 || len(first) >= 6 {
		t.Fatal("history byte limit not applied", len(first))
	}
	second, err := s.History(t.Context(), root.ID, first[len(first)-1].Sequence, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(first)+len(second) != 6 || second[0].Sequence != first[len(first)-1].Sequence+1 {
		t.Fatal("byte-limited cursor skipped or duplicated messages")
	}
}

func TestClosedStoreRecoveryRetainsQueuedWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	submit(t, s, root.ID, "running")
	active := claim(t, s, root.ID)
	queued := submit(t, s, root.ID, "queued")
	identity := s.Identity()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	if reopened.Identity() != identity {
		t.Fatal("identity changed")
	}
	if n, err := reopened.Recover(t.Context()); err != nil || n != 1 {
		t.Fatal("recovery", err)
	}
	original, err := reopened.Turn(t.Context(), active.Turn.ID)
	if err != nil || original.State != session.Interrupted {
		t.Fatal("running turn", err)
	}
	next := claim(t, reopened, root.ID)
	if next.Input.ID != queued.Input.ID {
		t.Fatal("claimed input replayed")
	}
}
