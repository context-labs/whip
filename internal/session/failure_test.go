package session

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
)

func TestMigrationFailureKeepsCheckpointedSourceAndSyncedBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db := createHistoricalStore(t, path, historicalShape{name: "H0"})
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE blocker(x); CREATE INDEX events ON blocker(x)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO messages VALUES('legacy',9,'user','{"role":"user","content":"committed in WAL"}')`); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("fixture did not retain committed WAL data: info=%v err=%v", info, err)
	}

	if st, err := Open(path); err == nil {
		st.Close()
		t.Fatal("migration should fail on the colliding events index")
	}
	var sourceRows, sourceVersion int
	if err := db.QueryRow(`SELECT count(*) FROM messages WHERE seq=9`).Scan(&sourceRows); err != nil || sourceRows != 1 {
		t.Fatalf("source lost WAL row: rows=%d err=%v", sourceRows, err)
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&sourceVersion); err != nil || sourceVersion != 0 {
		t.Fatalf("failed migration changed source version: %d, %v", sourceVersion, err)
	}
	var modeColumn int
	if err := db.QueryRow(`SELECT count(*) FROM pragma_table_info('sessions') WHERE name='mode'`).Scan(&modeColumn); err != nil || modeColumn != 0 {
		t.Fatalf("failed migration left partial columns: count=%d err=%v", modeColumn, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	backup, err := sql.Open("sqlite", migrationBackupPath(path, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	var backupRows, backupVersion int
	if err := backup.QueryRow(`SELECT count(*) FROM messages WHERE seq=9`).Scan(&backupRows); err != nil || backupRows != 1 {
		t.Fatalf("backup lost checkpointed WAL row: rows=%d err=%v", backupRows, err)
	}
	if err := backup.QueryRow(`PRAGMA user_version`).Scan(&backupVersion); err != nil || backupVersion != 0 {
		t.Fatalf("backup version=%d err=%v", backupVersion, err)
	}
}

func TestRuntimeTransitionFailureRollsBackRowsAndDiagnosesBody(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rootID, _ := st.Create("/workspace", "m", "p")
	exec(t, st, `CREATE TRIGGER reject_event BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'event failure'); END`)
	large := bytes.Repeat([]byte("orphan-on-rollback"), 1024)
	_, err = st.CommitRuntime(context.Background(), RuntimeTransition{
		Agent:   &RuntimeAgent{ID: "a", RootID: rootID, Status: "idle"},
		Command: &RuntimeCommand{ClientID: "c", ID: "cmd", Scope: CommandScopeRoot, RootID: rootID, RequestDigest: "d", Status: "queued", Payload: RuntimePayload{Data: large}},
		Inbox:   &RuntimeInbox{RootID: rootID, AgentID: "a", Seq: 1, Kind: "command", Status: "queued", Payload: RuntimePayload{Data: large}},
		State:   &RuntimeState{RootID: rootID, AgentID: "a", Key: "k", Version: 1, AuthorAgentID: "a", Payload: RuntimePayload{Data: large}},
		Event:   &RuntimeEvent{RootID: rootID, Seq: 1, Kind: "event", Payload: RuntimePayload{Data: large}},
		Usage:   &RuntimeUsage{ID: "u", RootID: rootID, AgentID: "a"},
	})
	if err == nil {
		t.Fatal("trigger should abort the runtime transition")
	}
	exec(t, st, `DROP TRIGGER reject_event`)
	for _, table := range []string{"agents", "commands", "inbox", "agent_state", "events", "usage_charges", "content_objects", "content_references", "content_grants"} {
		var n int
		if err := st.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("failed transition left %s rows=%d err=%v", table, n, err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	orphans, err := st.OrphanContent(context.Background())
	if err != nil || len(orphans) != 1 {
		t.Fatalf("orphan diagnoses=%+v err=%v", orphans, err)
	}
	if body, err := st.content.Read(orphans[0].Digest, 0, len(large)); err != nil || !bytes.Equal(body, large) {
		t.Fatalf("diagnosis deleted or changed body: %d bytes err=%v", len(body), err)
	}
}

func TestActorPersistenceEventFailuresRollBack(t *testing.T) {
	t.Run("enqueue", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		exec(t, st, `CREATE TRIGGER reject_actor_event BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'event failure'); END`)
		_, err := st.EnqueueInbox(context.Background(), InboxEnqueue{
			RootID: rootID, AgentID: agentID, Kind: "command",
			Payload: RuntimePayload{Data: bytes.Repeat([]byte("large"), 4096)},
		})
		if err == nil {
			t.Fatal("event failure should abort enqueue")
		}
		assertActorRows(t, st, rootID, 0, 0)
	})

	t.Run("consume", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		pair, err := st.EnqueueInbox(context.Background(), InboxEnqueue{RootID: rootID, AgentID: agentID, Kind: "command"})
		if err != nil {
			t.Fatal(err)
		}
		exec(t, st, `CREATE TRIGGER reject_actor_event BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'event failure'); END`)
		if _, err := st.ConsumeInbox(context.Background(), rootID, agentID, pair.InboxSeq); err == nil {
			t.Fatal("event failure should abort consume")
		}
		var status string
		if err := st.db.QueryRow(`SELECT status FROM inbox WHERE root_id=? AND agent_id=? AND seq=?`, rootID, agentID, pair.InboxSeq).Scan(&status); err != nil || status != "queued" {
			t.Fatalf("failed consume status=%q err=%v", status, err)
		}
		assertActorRows(t, st, rootID, 1, 1)
	})

	t.Run("schedule", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		anchor := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
		id, err := st.AddSchedule(rootID, "@every 10m", "work", anchor)
		if err != nil {
			t.Fatal(err)
		}
		exec(t, st, `CREATE TRIGGER reject_actor_event BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'event failure'); END`)
		if _, err := st.ClaimScheduleFire(context.Background(), ScheduleFireClaim{RootID: rootID, AgentID: agentID, ScheduleID: id, Slot: anchor}); err == nil {
			t.Fatal("event failure should abort schedule claim")
		}
		if got := st.Schedules(rootID); len(got) != 1 || !got[0].LastFire.IsZero() {
			t.Fatalf("failed schedule claim stamped row: %+v", got)
		}
		assertActorRows(t, st, rootID, 0, 0)
	})

	t.Run("root failure", func(t *testing.T) {
		st, rootID, agentID := actorFailureFixture(t)
		exec(t, st, `INSERT INTO commands(client_id,command_id,scope,root_id,request_digest,status,created_at,updated_at) VALUES('c','cmd','root',?,'d','running',?,?)`, rootID, now(), now())
		exec(t, st, `CREATE TRIGGER reject_actor_event BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'event failure'); END`)
		if _, err := st.FailClassicRoot(context.Background(), rootID, "actor panic"); err == nil {
			t.Fatal("event failure should abort root terminalization")
		}
		var statuses string
		if err := st.db.QueryRow(`SELECT a.status || ':' || c.status FROM agents a JOIN commands c ON c.root_id=a.root_id WHERE a.id=? AND c.command_id='cmd'`, agentID).Scan(&statuses); err != nil || statuses != "idle:running" {
			t.Fatalf("failed terminalization statuses=%q err=%v", statuses, err)
		}
		assertActorRows(t, st, rootID, 0, 0)
	})
}

func actorFailureFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(t.TempDir(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	return st, rootID, authority.AgentID
}

func assertActorRows(t *testing.T, st *Store, rootID string, inbox, events int) {
	t.Helper()
	for table, want := range map[string]int{"inbox": inbox, "events": events} {
		var got int
		if err := st.db.QueryRow(`SELECT count(*) FROM `+table+` WHERE root_id=?`, rootID).Scan(&got); err != nil || got != want {
			t.Fatalf("%s rows=%d want=%d err=%v", table, got, want, err)
		}
	}
}

func TestRecoveryFailureRollsBackEveryStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rootID, _ := st.Create("/workspace", "m", "p")
	exec(t, st, `INSERT INTO agents(id,root_id,parent_id,status,created_at,updated_at) VALUES('a',?,NULL,'idle',?,?)`, rootID, now(), now())
	exec(t, st, `INSERT INTO commands(client_id,command_id,scope,root_id,request_digest,status,created_at,updated_at) VALUES('c','cmd','root',?,'d','queued',?,?)`, rootID, now(), now())
	exec(t, st, `INSERT INTO turns(id,root_id,agent_id,status,created_at,updated_at) VALUES('turn',?,'a','running',?,?)`, rootID, now(), now())
	exec(t, st, `INSERT INTO child_executions(id,root_id,parent_agent_id,child_agent_id,status,created_at,updated_at) VALUES('child',?,'a','a','running',?,?)`, rootID, now(), now())
	exec(t, st, `INSERT INTO operations(id,root_id,agent_id,status,created_at,updated_at) VALUES('op',?,'a','running',?,?)`, rootID, now(), now())
	exec(t, st, `INSERT INTO leases(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES('lease',?,'a','op','running',?,?)`, rootID, now(), now())
	exec(t, st, `CREATE TRIGGER reject_recovery BEFORE UPDATE ON operations BEGIN SELECT RAISE(ABORT,'recovery failure'); END`)
	st.Close()

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Recover(context.Background()); err == nil {
		t.Fatal("recovery should fail closed")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]string{"commands": "queued", "turns": "running", "child_executions": "running", "operations": "running", "leases": "running"} {
		var status string
		if err := db.QueryRow(`SELECT status FROM ` + table + ` LIMIT 1`).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != want {
			t.Errorf("partial recovery changed %s to %q", table, status)
		}
	}
	if _, err := db.Exec(`DROP TRIGGER reject_recovery`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"commands", "turns", "child_executions", "operations", "leases"} {
		var status string
		if err := st.db.QueryRow(`SELECT status FROM ` + table + ` LIMIT 1`).Scan(&status); err != nil || status != "interrupted" {
			t.Errorf("%s status=%q err=%v", table, status, err)
		}
	}
}

func TestOpenRejectsInvalidDefaultMode(t *testing.T) {
	if _, err := OpenWithDefaultMode(filepath.Join(t.TempDir(), "sessions.db"), Mode("future")); err == nil {
		t.Fatal("invalid configured mode should fail before opening")
	}
}

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
	if err := st.RecordCompaction(id, 3, "second gen"); err != nil {
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
