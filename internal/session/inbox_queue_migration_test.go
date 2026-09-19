package session

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestMigrationsSkipPeerCompletedVersionTwenty(t *testing.T) {
	store, root, agent := newSwarmFixture(t)
	inputTestCommand(t, store, root, agent, "original", "submit")
	exec(t, store, dropQueueSchema+`UPDATE runtime_schema SET identity='whip-recursive-runtime-v19'; PRAGMA user_version=19`)
	conn, err := store.db.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := upgradeV19(t.Context(), conn); err != nil {
		t.Fatal(err)
	}
	// A second opener observed v19 before the peer took the write lock. It
	// now enters upgradeV19 after that peer committed the queue migration.
	// Reject any attempt to repeat its backfill, including after v21.
	if _, err := conn.ExecContext(t.Context(), `CREATE TRIGGER no_repeat_queue_backfill BEFORE UPDATE OF origin ON inbox
BEGIN SELECT RAISE(ABORT,'queue backfill repeated'); END;`); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{20, 21} {
		for _, upgrade := range []struct {
			name string
			run  func(context.Context, *sql.Conn) error
		}{
			{"v10", upgradeV10},
			{"v12", upgradeV12},
			{"v13", upgradeV13},
			{"v19", upgradeV19},
		} {
			if err := upgrade.run(t.Context(), conn); err != nil {
				t.Errorf("stale %s opener at version %d: %v", upgrade.name, version, err)
			}
		}
		// migrate's stale local snapshot still continues through upgradeV20.
		if err := upgradeV20(t.Context(), conn); err != nil {
			t.Fatalf("continue after peer upgrade: %v", err)
		}
	}
	var version int
	var identity string
	if err := conn.QueryRowContext(t.Context(), `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(t.Context(), `SELECT identity FROM runtime_schema WHERE id=1`).Scan(&identity); err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion || identity != schemaIdentity {
		t.Fatalf("final schema = %d %s", version, identity)
	}
	var origin, commandID string
	if err := conn.QueryRowContext(t.Context(), `SELECT origin,command_id FROM inbox WHERE root_id=?`, root).Scan(&origin, &commandID); err != nil {
		t.Fatal(err)
	}
	if origin != "client" || commandID != "original" {
		t.Fatalf("queue provenance changed: origin=%q command=%q", origin, commandID)
	}
}

func TestMigrationsRejectUnsupportedPeerSchema(t *testing.T) {
	for _, schema := range []struct {
		version  int
		identity string
	}{
		{20, "other-runtime-v20"},
		{20, "whip-recursive-runtime-v19"},
		{21, "other-runtime-v21"},
		{22, "whip-recursive-runtime-v22"},
	} {
		t.Run(fmt.Sprintf("%d-%s", schema.version, schema.identity), func(t *testing.T) {
			store, _, _ := newSwarmFixture(t)
			conn, err := store.db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			if _, err := conn.ExecContext(t.Context(), fmt.Sprintf(`PRAGMA user_version=%d`, schema.version)); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.ExecContext(t.Context(), `UPDATE runtime_schema SET identity=?`, schema.identity); err != nil {
				t.Fatal(err)
			}
			for _, upgrade := range []struct {
				name string
				run  func(context.Context, *sql.Conn) error
			}{
				{"v10", upgradeV10},
				{"v12", upgradeV12},
				{"v13", upgradeV13},
				{"v19", upgradeV19},
			} {
				if err := upgrade.run(t.Context(), conn); err == nil {
					t.Errorf("%s accepted unsupported peer schema", upgrade.name)
				}
			}
		})
	}
}
