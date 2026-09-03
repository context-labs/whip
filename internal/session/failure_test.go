package session

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

// exec runs test-only SQL against the store's own database. Every fault below
// is injected this way — through the real store's real connection — so the
// production code under test is untouched.
func exec(t *testing.T, st *Store, query string, args ...any) {
	t.Helper()
	if _, err := st.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("fault injection %q: %v", query, err)
	}
}

// TestOpenRejectsIncompatibleDatabase: pointing the store at a database whose
// schema collides with whip's must return an error, not a half-built Store.
func TestOpenRejectsIncompatibleDatabase(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	// an index named "messages" makes CREATE TABLE messages fail even with
	// IF NOT EXISTS — the name is already taken in the schema namespace
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE t(a); CREATE INDEX messages ON t(a)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(p)
	if err == nil {
		st.Close()
		t.Fatal("Open should reject a database whose schema collides with whip's")
	}
	if !strings.Contains(err.Error(), "messages") {
		t.Fatalf("error should name the colliding object: %v", err)
	}
}

// TestClosedStoreDegradesGracefully is the graceful-degradation contract: once
// the database is gone, every accessor must report failure (error return, or
// nil for the ones that swallow errors) instead of panicking.
func TestClosedStoreDegradesGracefully(t *testing.T) {
	st, id := seeded(t)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	errCases := map[string]func() error{
		"LoadTasks":   func() error { _, err := st.LoadTasks(id); return err },
		"Save":        func() error { return st.Save(id, 0, []llm.Message{{Role: "user", Content: "x"}}, "m", "p") },
		"Load":        func() error { _, _, err := st.Load(id); return err },
		"Recent":      func() error { _, err := st.Recent(10); return err },
		"UserHistory": func() error { _, err := st.UserHistory(10); return err },
		"DeleteFrom":  func() error { return st.DeleteFrom(id, 1) },
		"Fork":        func() error { _, err := st.Fork(id, 2, "x"); return err },
		"ForksOf":     func() error { _, err := st.ForksOf(id); return err },
		"ForkTitle":   func() error { _, err := st.ForkTitle("base"); return err },
		"AddSchedule": func() error { _, err := st.AddSchedule(id, "@every 1m", "p", time.Now()); return err },
	}
	for name, fn := range errCases {
		if err := fn(); err == nil {
			t.Errorf("%s on a closed store should error", name)
		}
	}

	if got := st.Snapshots(id); got != nil {
		t.Errorf("Snapshots on a closed store = %v, want nil", got)
	}
	if got := st.Schedules(id); got != nil {
		t.Errorf("Schedules on a closed store = %v, want nil", got)
	}
	if got := st.Compactions(id); got != nil {
		t.Errorf("Compactions on a closed store = %v, want nil", got)
	}
	if got := st.RawMessages(id); got != nil {
		t.Errorf("RawMessages on a closed store = %v, want nil", got)
	}
	if got := st.Todos(id); got != "" {
		t.Errorf("Todos on a closed store = %q, want empty", got)
	}
}

// TestLoadReportsMissingMessageTable: the session row resolves but the message
// query fails — Load must surface that, not return a silently empty history.
func TestLoadReportsMissingMessageTable(t *testing.T) {
	st, id := seeded(t)
	exec(t, st, `DROP TABLE messages`)
	if _, _, err := st.Load(id); err == nil {
		t.Fatal("Load should report the failed message query")
	}
}

// TestScanMetasRejectsCorruptRow: a session row whose fork_seq holds
// non-numeric text (a hand-edited or corrupted database) must fail the scan
// rather than yield a bogus Meta.
func TestScanMetasRejectsCorruptRow(t *testing.T) {
	st, id := seeded(t)
	exec(t, st, `UPDATE sessions SET fork_seq='not-a-number' WHERE id=?`, id)

	if _, _, err := st.Load(id); err == nil {
		t.Error("Load should report the corrupt fork_seq")
	}
	if _, err := st.Recent(10); err == nil {
		t.Error("Recent should report the corrupt fork_seq")
	}
	// ForksOf reads the same columns; make the corrupt row a child of itself
	exec(t, st, `UPDATE sessions SET forked_from=? WHERE id=?`, id, id)
	if _, err := st.ForksOf(id); err == nil {
		t.Error("ForksOf should report the corrupt fork_seq")
	}
}

// TestSaveReportsMessageWriteFailure: a write that the database refuses must
// abort the whole Save (the transaction rolls back), not report success.
func TestSaveReportsMessageWriteFailure(t *testing.T) {
	st, id := seeded(t)
	exec(t, st, `CREATE TRIGGER no_messages BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT,'disk full'); END`)

	err := st.Save(id, 5, []llm.Message{{}, {}, {}, {}, {}, {Role: "user", Content: "new"}}, "m", "p")
	if err == nil {
		t.Fatal("Save should report the refused message write")
	}
	// the transaction rolled back: the seeded history is intact and unchanged
	exec(t, st, `DROP TRIGGER no_messages`)
	if _, msgs, err := st.Load(id); err != nil || len(msgs) != 4 {
		t.Fatalf("failed Save must not commit anything: %v %d msgs", err, len(msgs))
	}
}

// TestSaveReportsMetadataWriteFailure covers the second statement in the same
// transaction: the message rows land but the sessions row refuses the update.
func TestSaveReportsMetadataWriteFailure(t *testing.T) {
	st, id := seeded(t)
	exec(t, st, `CREATE TRIGGER no_meta BEFORE UPDATE ON sessions BEGIN SELECT RAISE(ABORT,'read only'); END`)

	if err := st.Save(id, 5, []llm.Message{{}, {}, {}, {}, {}, {Role: "user", Content: "new"}}, "m", "p"); err == nil {
		t.Fatal("Save should report the refused metadata update")
	}
	exec(t, st, `DROP TRIGGER no_meta`)
	if _, msgs, err := st.Load(id); err != nil || len(msgs) != 4 {
		t.Fatalf("the message insert must roll back with the metadata update: %v %d msgs", err, len(msgs))
	}
}

// TestSaveSkipsPlaceholderRows: zero-value messages (padding the caller never
// meant to persist) must not clobber the raw log.
func TestSaveSkipsPlaceholderRows(t *testing.T) {
	st, id := seeded(t)
	msgs := []llm.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "q1", Authored: true},
		{Role: "assistant", Content: "a1"},
		{}, // placeholder: must be skipped, not written over seq 3
		{Role: "assistant", Content: "a2"},
	}
	if err := st.Save(id, 3, msgs, "m", "p"); err != nil {
		t.Fatal(err)
	}
	raw := st.RawMessages(id)
	if len(raw) != 4 {
		t.Fatalf("placeholder should not add or replace a row: %d rows", len(raw))
	}
	if raw[2].Content != "q2" {
		t.Fatalf("seq 3 should keep its original content, got %q", raw[2].Content)
	}
}

// TestForkReportsWriteFailures: both halves of the fork transaction must
// surface a refused write.
func TestForkReportsWriteFailures(t *testing.T) {
	t.Run("session row", func(t *testing.T) {
		st, id := seeded(t)
		exec(t, st, `CREATE TRIGGER no_sessions BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT,'nope'); END`)
		if _, err := st.Fork(id, 2, "x"); err == nil {
			t.Fatal("Fork should report the refused session insert")
		}
	})
	t.Run("message rows", func(t *testing.T) {
		st, id := seeded(t)
		exec(t, st, `CREATE TRIGGER no_messages BEFORE INSERT ON messages BEGIN SELECT RAISE(ABORT,'nope'); END`)
		newID, err := st.Fork(id, 2, "x")
		if err == nil {
			t.Fatal("Fork should report the refused message copy")
		}
		if newID != "" {
			t.Fatalf("a failed fork must not report an id, got %q", newID)
		}
		exec(t, st, `DROP TRIGGER no_messages`)
		// the session row rolled back with it — no orphan fork
		forks, err := st.ForksOf(id)
		if err != nil || len(forks) != 0 {
			t.Fatalf("a failed fork must not leave a session row behind: %v %+v", err, forks)
		}
	})
}

// TestForkTitleUnwrapsExistingSuffix: forking a fork increments the counter
// instead of nesting "(fork #N)" suffixes.
func TestForkTitleUnwrapsExistingSuffix(t *testing.T) {
	st, id := seeded(t)
	if _, err := st.Fork(id, 2, "notes (fork #1)"); err != nil {
		t.Fatal(err)
	}
	got, err := st.ForkTitle("notes (fork #1)")
	if err != nil {
		t.Fatal(err)
	}
	if got != "notes (fork #2)" {
		t.Fatalf("forking a fork should increment, got %q", got)
	}
	// a suffix with trailing text is not a fork marker and stays verbatim
	got, err = st.ForkTitle("notes (fork #9) draft")
	if err != nil {
		t.Fatal(err)
	}
	if got != "notes (fork #9) draft (fork #1)" {
		t.Fatalf("only an exact suffix unwraps, got %q", got)
	}
}

// TestSchedulesSkipsCorruptRow: one unreadable schedule row must not take the
// rest of the list down with it.
func TestSchedulesSkipsCorruptRow(t *testing.T) {
	st, id := seeded(t)
	anchor := time.Now().Truncate(time.Second)
	if _, err := st.AddSchedule(id, "@every 10m", "first", anchor); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSchedule(id, "@every 20m", "second", anchor); err != nil {
		t.Fatal(err)
	}
	exec(t, st, `UPDATE schedules SET id='corrupt' WHERE session_id=? AND prompt='first'`, id)

	got := st.Schedules(id)
	if len(got) != 1 || got[0].Prompt != "second" {
		t.Fatalf("the readable schedule should survive, got %+v", got)
	}
}

// TestUserHistorySkipsMalformedRows: a row whose content isn't valid message
// JSON is skipped rather than failing the whole recall.
func TestUserHistorySkipsMalformedRows(t *testing.T) {
	st, id := seeded(t)
	exec(t, st, `INSERT INTO messages (session_id, seq, role, content) VALUES (?,?,?,?)`,
		id, 99, "user", "{not json")

	got, err := st.UserHistory(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "q2" || got[1] != "q1" {
		t.Fatalf("malformed row should be skipped, got %q", got)
	}
}

// TestApplyCompactionKeepsPriorSummary: a second compaction must keep the
// previous compaction's saved summary row — it covers history the new summary
// does not reach.
func TestApplyCompactionKeepsPriorSummary(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	id, err := st.Create("/tmp", "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	msgs := []llm.Message{
		{Role: "system", Content: "sys"}, // seq 0 is never persisted
		{Role: "user", Content: "q1", Authored: true},
		{Role: "system", Content: "Summary of the conversation so far:\n\nfirst gen"},
		{Role: "user", Content: "q2", Authored: true},
		{Role: "assistant", Content: "a2"},
	}
	if err := st.Save(id, 1, msgs, "m", "p"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordCompaction(id, 3, "second gen", "", llm.Usage{}); err != nil {
		t.Fatal(err)
	}
	_, got, err := st.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	// the raw log is [q1, first-gen summary, q2, a2]; folding at 3 keeps the
	// head slot, the new summary, the prior summary, and the raw tail
	if len(got) != 4 {
		t.Fatalf("compacted view: %d msgs %+v", len(got), got)
	}
	if !strings.Contains(got[1].Content, "second gen") {
		t.Fatalf("the newest summary should come first: %q", got[1].Content)
	}
	if !strings.Contains(got[2].Content, "first gen") {
		t.Fatalf("the prior summary must be kept: %q", got[2].Content)
	}
	if got[3].Content != "a2" {
		t.Fatalf("raw tail lost: %+v", got[3:])
	}
}
