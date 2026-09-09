package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
)

func TestPermissionModeCommitsSnapshotAndEventTogether(t *testing.T) {
	store, rootID, _ := actorFailureFixture(t)
	before, err := store.SnapshotRoot(t.Context(), rootID)
	if err != nil || before.PermissionMode != PermissionModePrompt {
		t.Fatalf("initial mode=%q, error=%v", before.PermissionMode, err)
	}
	// Failing the event insert must roll back the mode as well.
	exec(t, store, `CREATE TRIGGER reject_mode_event BEFORE INSERT ON events
		WHEN NEW.kind='session.permission_mode.updated' BEGIN SELECT RAISE(ABORT,'event failed'); END`)
	if err := store.SetPermissionMode(t.Context(), rootID, PermissionModeAutomatic); err == nil {
		t.Fatal("mode update succeeded without its event")
	}
	after, err := store.SnapshotRoot(t.Context(), rootID)
	if err != nil || after.PermissionMode != before.PermissionMode || after.Cursor != before.Cursor {
		t.Fatalf("failed write changed snapshot: mode=%q cursor=%d error=%v", after.PermissionMode, after.Cursor, err)
	}
	exec(t, store, `DROP TRIGGER reject_mode_event`)
	for _, mode := range []string{PermissionModeAutomatic, PermissionModePrompt} {
		if err := store.SetPermissionMode(t.Context(), rootID, mode); err != nil {
			t.Fatal(err)
		}
		snapshot, err := store.SnapshotRoot(t.Context(), rootID)
		if err != nil || snapshot.PermissionMode != mode {
			t.Fatalf("saved mode=%q error=%v", snapshot.PermissionMode, err)
		}
		events, _, err := store.ReplayEvents(t.Context(), rootID, snapshot.Cursor-1, 1)
		if err != nil || len(events) != 1 || events[0].Kind != "session.permission_mode.updated" {
			t.Fatalf("mode event=%+v error=%v", events, err)
		}
		var event struct {
			PermissionMode string `json:"permission_mode"`
		}
		if err := json.Unmarshal(events[0].Payload.Inline, &event); err != nil || event.PermissionMode != mode {
			t.Fatalf("event=%+v error=%v", event, err)
		}
	}
	if err := store.SetPermissionMode(t.Context(), rootID, "unknown"); err == nil {
		t.Fatal("unknown mode accepted")
	}
	if err := store.SetPermissionMode(t.Context(), "missing", PermissionModeAutomatic); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing root error=%v", err)
	}
}

func TestPermissionModeForkStartsWithDefault(t *testing.T) {
	store, rootID := seeded(t)
	if err := store.SetPermissionMode(t.Context(), rootID, PermissionModeAutomatic); err != nil {
		t.Fatal(err)
	}
	forkID, err := store.Fork(rootID, 1, "fork")
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := store.PermissionMode(t.Context(), forkID); err != nil || mode != PermissionModePrompt {
		t.Fatalf("fork mode=%q error=%v", mode, err)
	}
	if mode, err := store.PermissionMode(t.Context(), rootID); err != nil || mode != PermissionModeAutomatic {
		t.Fatalf("source mode=%q error=%v", mode, err)
	}
}
