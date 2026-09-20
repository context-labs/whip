package session

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/llm"
)

func TestV20UpgradeRejectsUnsupportedSchemaWithoutMutation(t *testing.T) {
	for _, schema := range []struct {
		version  int
		identity string
	}{
		{19, "whip-recursive-runtime-v19"},
		{20, "foreign"},
		{20, "whip-recursive-runtime-v21"},
		{21, "foreign"},
		{21, "whip-recursive-runtime-v20"},
		{22, "whip-recursive-runtime-v22"},
		{22, schemaIdentity},
	} {
		for _, upgrade := range []struct {
			name string
			run  func(context.Context, *sql.Conn) error
		}{
			{"direct", upgradeV20},
			{"stale20", func(ctx context.Context, conn *sql.Conn) error {
				return migrateExisting(ctx, conn, "unused.db", 20, "whip-recursive-runtime-v20", nil)
			}},
		} {
			t.Run(fmt.Sprintf("%s/%d/%s", upgrade.name, schema.version, schema.identity), func(t *testing.T) {
				store, _, _ := newSwarmFixture(t)
				conn, err := store.db.Conn(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if schema.version == 20 {
					if _, err := conn.ExecContext(t.Context(), `ALTER TABLE compactions DROP COLUMN pinned`); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := conn.ExecContext(t.Context(), `UPDATE runtime_schema SET identity=?`, schema.identity); err != nil {
					t.Fatal(err)
				}
				if _, err := conn.ExecContext(t.Context(), fmt.Sprintf(`PRAGMA user_version=%d`, schema.version)); err != nil {
					t.Fatal(err)
				}
				// A rejected source must not run timestamp normalization either.
				if _, err := conn.ExecContext(t.Context(), `UPDATE sessions SET updated_at='2025-01-02T03:04:05Z'`); err != nil {
					t.Fatal(err)
				}
				type databaseState struct {
					version, schemaVersion, changes int
					identity                        string
				}
				readState := func() databaseState {
					t.Helper()
					var state databaseState
					err := conn.QueryRowContext(t.Context(), `SELECT user_version,schema_version,total_changes(),identity
FROM pragma_user_version,pragma_schema_version,runtime_schema WHERE id=1`).Scan(
						&state.version, &state.schemaVersion, &state.changes, &state.identity)
					if err != nil {
						t.Fatal(err)
					}
					return state
				}
				before := readState()
				if err := upgrade.run(t.Context(), conn); err == nil {
					t.Error("accepted unsupported schema")
				}
				if after := readState(); after != before {
					t.Fatalf("rejected upgrade changed database: before=%+v after=%+v", before, after)
				}
			})
		}
	}
}

func TestCompactionPinSurvivesReopenAndFork(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(fmt.Sprintf("pinned=%t", pinned), func(t *testing.T) {
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
			raw := []llm.Message{
				{Role: "user", Content: "exact original instructions", Authored: true},
				{Role: "assistant", Content: "retained answer"},
			}
			cutoff := 1
			transcriptCommit(t, store, root, root, 1, raw,
				RootCompaction{Summary: "folded instructions", RawCutoff: &cutoff, Pinned: pinned})
			_, before, err := store.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			wantLen := 2
			if pinned {
				wantLen++
			}
			if len(before) != wantLen || before[len(before)-1].Content != "retained answer" {
				t.Fatalf("derived view = %+v", before)
			}
			if pinned && (before[1].Content != raw[0].Content || !before[1].Authored) {
				t.Fatalf("pinned message lost original data: %+v", before)
			}
			fork, err := store.Fork(root, len(raw), "fork")
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
			for _, id := range []string{root, fork} {
				_, after, err := store.Load(id)
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("reopened view = %+v, err = %v; want %+v", after, err, before)
				}
				if got := store.RawMessages(id); len(got) != len(raw) || got[0].Content != raw[0].Content {
					t.Fatalf("raw history changed: %+v", got)
				}
			}
		})
	}
}

func TestVersionTwentyCompactionsDoNotInferPins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	root, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	raw := []llm.Message{{Role: "user", Content: "legacy question"}, {Role: "assistant", Content: "legacy answer"}}
	if err := store.Save(root, 0, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordCompaction(root, 1, "legacy summary"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `ALTER TABLE compactions DROP COLUMN pinned;
UPDATE runtime_schema SET identity='whip-recursive-runtime-v20'; PRAGMA user_version=20;`); err != nil {
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
	_, view, err := store.Load(root)
	if err != nil || len(view) != 2 || view[1].Content != "legacy answer" {
		t.Fatalf("legacy view = %+v, err = %v", view, err)
	}
	if got := store.RawMessages(root); len(got) != 2 || got[0].Content != "legacy question" {
		t.Fatalf("migration changed raw history: %+v", got)
	}
	var pinned bool
	if err := store.db.QueryRowContext(t.Context(), `SELECT pinned FROM compactions WHERE session_id=?`, root).Scan(&pinned); err != nil || pinned {
		t.Fatalf("legacy pin = %t, err = %v", pinned, err)
	}
}
