package session

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestUpcomingSchedulesOffsetAndFractionalOccurrence(t *testing.T) {
	for _, test := range []struct{ name, expression, wire string }{
		{"nanoseconds", "2026-09-20T17:00:00.123456789-06:00", "2026-09-20T23:00:00.123456789Z"},
		{"trailing zeroes", "2026-09-20T17:00:00.12-06:00", "2026-09-20T23:00:00.120000000Z"},
		{"whole second", "2026-09-20T17:00:00-06:00", "2026-09-20T23:00:00.000000000Z"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			root, err := store.Create(SessionKindAgent, t.TempDir(), "m", "p")
			if err != nil {
				t.Fatal(err)
			}
			authority, err := store.EnsureAuthority(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			slot, err := time.Parse(time.RFC3339Nano, test.expression)
			if err != nil {
				t.Fatal(err)
			}
			id, err := store.AddSchedule(root, "@at "+test.expression, "offset prompt", slot)
			if err != nil {
				t.Fatal(err)
			}
			before, err := store.SnapshotRoot(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			rows := *before.UpcomingSchedules
			if len(rows) != 1 || rows[0].ID != id {
				t.Fatalf("upcoming occurrence=%+v", rows)
			}
			raw, err := json.Marshal(rows[0])
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				NextFire string `json:"next_fire"`
			}
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.NextFire != test.wire {
				t.Fatalf("wire time=%q want %q", wire.NextFire, test.wire)
			}
			if _, err := store.ClaimScheduleFire(t.Context(), ScheduleFireClaim{RootID: root, AgentID: authority.AgentID, ScheduleID: id, Slot: slot}); err != nil {
				t.Fatal(err)
			}
			events, _, err := store.ReplayEvents(t.Context(), root, before.Cursor, 10)
			if err != nil || len(events) != 1 || events[0].Kind != "schedule.fired" {
				t.Fatalf("events=%+v err=%v", events, err)
			}
			var fired LifecycleEvent
			if err := json.Unmarshal(events[0].Payload.Inline, &fired); err != nil {
				t.Fatal(err)
			}
			if fired.ScheduleID != id || fired.Slot != wire.NextFire {
				t.Fatalf("fired slot %q != projected %q", fired.Slot, wire.NextFire)
			}
			after, err := store.SnapshotRoot(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(*after.UpcomingSchedules) != 0 || *after.UpcomingScheduleCount != 0 || after.Cursor <= before.Cursor {
				t.Fatalf("claimed snapshot=%+v", after)
			}
		})
	}
}
