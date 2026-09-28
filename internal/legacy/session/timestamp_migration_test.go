package session

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV20UpgradeNormalizesLegacyTimestampOrdering(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AdmitAgent(t.Context(), AgentAdmission{
		RootID: root, ParentAgentID: root, ChildAgentID: "child", Name: "child",
		Model: "model", Provider: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	laterRoot, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.September, 14, 19, 4, 5, 0, time.UTC)
	legacy, normalized := at.Format(time.RFC3339), formatStamp(at)
	precise := formatStamp(at.Add(123456789 * time.Nanosecond))
	message, err := store.SendMailboxMessage(t.Context(), root, root, "child", MailboxSend{
		Body: "legacy mail", AvailableAt: at,
	})
	if err != nil {
		t.Fatal(err)
	}
	scheduleID, err := store.AddSchedule(root, "@every 1h", "check", at)
	if err != nil {
		t.Fatal(err)
	}
	for _, update := range []struct {
		query string
		args  []any
	}{
		{`UPDATE sessions SET created_at=?,updated_at=? WHERE id=?`, []any{legacy, legacy, root}},
		{`UPDATE sessions SET updated_at=? WHERE id=?`, []any{precise, laterRoot}},
		{`UPDATE agent_messages SET available_at=?,created_at=? WHERE id=?`, []any{legacy, legacy, message.ID}},
		{`UPDATE schedules SET anchor=?,created_at=? WHERE session_id=? AND id=?`, []any{legacy, legacy, root, scheduleID}},
	} {
		if _, err := store.db.ExecContext(t.Context(), update.query, update.args...); err != nil {
			t.Fatal(err)
		}
	}
	// Recreate the supported v20 shape, including timestamps inherited from
	// v18. Both schema and timestamp repairs must happen through Open.
	if _, err := store.db.ExecContext(t.Context(), `
ALTER TABLE compactions DROP COLUMN pinned;
UPDATE runtime_schema SET identity='whip-recursive-runtime-v20' WHERE id=1;
PRAGMA user_version=20;`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	for range 2 { // Reopening an upgraded store must preserve the same values.
		store, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, instant := range []time.Time{at, at.Add(500 * time.Millisecond)} {
			work, err := store.AgentWorkStatus(t.Context(), root, "child", instant)
			if err != nil || !work.HasReadyMail || !work.NextDeferredAt.IsZero() {
				t.Fatalf("legacy mail not ready at %s: %+v, %v", instant, work, err)
			}
		}
		page, err := store.SessionCatalog(t.Context(), CatalogPageOptions{Limit: 2, MaxBytes: 4096})
		if err != nil || len(page.Items) != 2 || page.Items[0].ID != laterRoot || page.Items[1].ID != root {
			t.Fatalf("mixed-format catalog ordering: %+v, %v", page, err)
		}
		if page.Items[0].UpdatedAt != precise || page.Items[1].UpdatedAt != normalized {
			t.Fatalf("timestamp precision changed: %+v", page.Items)
		}
		var anchor, lastFire, created string
		if err := store.db.QueryRowContext(t.Context(),
			`SELECT anchor,last_fire,created_at FROM schedules WHERE session_id=? AND id=?`, root, scheduleID,
		).Scan(&anchor, &lastFire, &created); err != nil {
			t.Fatal(err)
		}
		if anchor != normalized || created != normalized || lastFire != "" {
			t.Fatalf("schedule stamps = %q, %q, %q", anchor, lastFire, created)
		}
		var available, delivered, done string
		if err := store.db.QueryRowContext(t.Context(),
			`SELECT available_at,delivered_at,done_at FROM agent_messages WHERE id=?`, message.ID,
		).Scan(&available, &delivered, &done); err != nil {
			t.Fatal(err)
		}
		if available != normalized || delivered != "" || done != "" {
			t.Fatalf("mail stamps = %q, %q, %q", available, delivered, done)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTimestampNormalizationFailureRollsBackUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	const legacy = "2026-09-14T19:04:05Z"
	if _, err := store.db.ExecContext(
		t.Context(), `UPDATE sessions SET updated_at=? WHERE id=?`, legacy, root,
	); err != nil {
		t.Fatal(err)
	}
	// Fail at the last normalized table, after earlier timestamps were rewritten.
	if _, err := store.db.ExecContext(t.Context(), `
ALTER TABLE compactions DROP COLUMN pinned;
DROP TABLE daemon_state;
UPDATE runtime_schema SET identity='whip-recursive-runtime-v20' WHERE id=1;
PRAGMA user_version=20;`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(path); err == nil {
		_ = reopened.Close()
		t.Fatal("incomplete schema unexpectedly upgraded")
	} else if !strings.Contains(err.Error(), "normalize legacy timestamps") {
		t.Fatalf("unexpected upgrade failure: %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var stamp string
	if err := db.QueryRowContext(
		t.Context(), `SELECT updated_at FROM sessions WHERE id=?`, root,
	).Scan(&stamp); err != nil || stamp != legacy {
		t.Fatalf("timestamp rewrite was not rolled back: %q, %v", stamp, err)
	}
	var version, pinned int
	if err := db.QueryRowContext(t.Context(), `PRAGMA user_version`).Scan(&version); err != nil || version != 20 {
		t.Fatalf("failed upgrade changed version: %d, %v", version, err)
	}
	if err := db.QueryRowContext(
		t.Context(), `SELECT count(*) FROM pragma_table_info('compactions') WHERE name='pinned'`,
	).Scan(&pinned); err != nil || pinned != 0 {
		t.Fatalf("failed upgrade changed compaction schema: %d, %v", pinned, err)
	}
}
