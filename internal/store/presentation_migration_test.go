package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func removePresentationSchema(t *testing.T, s *Store) {
	t.Helper()
	start := strings.Index(schema, "CREATE TRIGGER message_immutable")
	end := strings.Index(schema[start:], "\n\n") + start
	execTest(t, s, "ALTER TABLE history_groups DROP COLUMN attempt_presentations; DROP TRIGGER message_immutable; ALTER TABLE messages DROP COLUMN presentation;"+schema[start:end]+"; PRAGMA user_version=56")
}
func TestPresentationMigrationPreservesLegacyAndRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	root, _ := operationCell(t, s)
	original, err := s.History(t.Context(), root.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	removePresentationSchema(t, s)
	identity := s.Identity()
	reopened := openTest(t, path)
	if reopened.Identity() != identity {
		t.Fatal("identity changed")
	}
	history, err := reopened.History(t.Context(), root.ID, 0, 100)
	if err != nil || len(history) != len(original) {
		t.Fatal(history, err)
	}
	for _, m := range history {
		if m.Presentation != nil {
			t.Fatal("invented backfill")
		}
	}
	var version int
	if err := reopened.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil || version != 57 {
		t.Fatal(version, err)
	}
	again := openTest(t, path)
	if again.Identity() != identity {
		t.Fatal("restart changed identity")
	}
}
func TestPresentationMigrationRollsBackDDLAndVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	removePresentationSchema(t, s)
	// Failure after ADD COLUMN must roll back that column and preserve schema56.
	execTest(t, s, "DROP TRIGGER message_immutable")
	if migrated, err := Open(t.Context(), path); err == nil {
		_ = migrated.Close()
		t.Fatal("accepted missing prerequisite trigger")
	}
	var version, columns int
	if err := s.db.QueryRowContext(t.Context(), "PRAGMA user_version").Scan(&version); err != nil || version != 56 {
		t.Fatal(version, err)
	}
	if err := s.db.QueryRowContext(t.Context(), "SELECT count(*) FROM pragma_table_info('messages') WHERE name='presentation'").Scan(&columns); err != nil || columns != 0 {
		t.Fatal("partial DDL survived", columns, err)
	}
	start := strings.Index(schema, "CREATE TRIGGER message_immutable")
	end := strings.Index(schema[start:], "\n\n") + start
	execTest(t, s, schema[start:end])
	if openTest(t, path).Identity() != s.Identity() {
		t.Fatal("recovery changed identity")
	}
}
