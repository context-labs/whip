package session

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestUpcomingSchedulesIndependentOfHistoryAndClaims(t *testing.T) {
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
	anchor := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	add := func(expression, prompt string) int {
		t.Helper()
		id, err := store.AddSchedule(root, expression, prompt, anchor)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	view := func(limit, budget int) RootSnapshot {
		t.Helper()
		got, err := store.SnapshotRootView(t.Context(), root, SnapshotViewOptions{RecentMessages: 1, CollectionLimit: limit, MaxBytes: budget})
		if err != nil {
			t.Fatal(err)
		}
		if got.UpcomingSchedules == nil || got.UpcomingScheduleCount == nil {
			t.Fatal("missing projection support")
		}
		encoded, err := json.Marshal(got)
		if err != nil || len(encoded) > budget {
			t.Fatalf("size=%d err=%v", len(encoded), err)
		}
		return got
	}
	empty := view(2, 4096)
	encoded, _ := json.Marshal(empty)
	if *empty.UpcomingScheduleCount != 0 || !strings.Contains(string(encoded), `"upcoming_schedules":[]`) {
		t.Fatalf("empty projection %s", encoded)
	}
	// Old fired rows exceed the historical collection page.
	for range 12 {
		id := add("@at "+anchor.Format(time.RFC3339), "past")
		if err := store.MarkFired(root, id, anchor); err != nil {
			t.Fatal(err)
		}
	}
	add("not a schedule", "invalid")
	late := add("@at "+anchor.Add(time.Hour).Format(time.RFC3339), "later")
	overdue := add("@at "+anchor.Format(time.RFC3339), "overdue")
	recurring := add("@every 10m", "recurring")
	cancelled := add("@every 1h", "cancelled")
	if err := store.DeleteSchedule(root, cancelled); err != nil {
		t.Fatal(err)
	}
	got := view(2, 32768)
	rows := *got.UpcomingSchedules
	if *got.UpcomingScheduleCount != 3 || len(rows) != 2 || rows[0].ID != overdue || rows[1].ID != recurring || !got.Omitted["upcoming_schedules"] {
		t.Fatalf("pending=%+v count=%d omitted=%v", rows, *got.UpcomingScheduleCount, got.Omitted)
	}
	if len(got.Schedules) != 2 || got.Schedules[0].ID != 1 {
		t.Fatalf("historical page changed: %+v", got.Schedules)
	}
	// Admission is the disappearance boundary: no turn has started or completed.
	for _, id := range []int{overdue, recurring} {
		if _, err := store.ClaimScheduleFire(t.Context(), ScheduleFireClaim{RootID: root, AgentID: authority.AgentID, ScheduleID: id, Slot: anchor}); err != nil {
			t.Fatal(err)
		}
	}
	got = view(4, 32768)
	rows = *got.UpcomingSchedules
	if *got.UpcomingScheduleCount != 2 || len(rows) != 2 || rows[0].ID != recurring || rows[0].NextFire != formatStamp(anchor.Add(10*time.Minute)) || rows[1].ID != late || got.Omitted["upcoming_schedules"] {
		t.Fatalf("claimed projection=%+v count=%d", rows, *got.UpcomingScheduleCount)
	}
	queued, err := store.LoadQueuedInbox(t.Context(), root, authority.AgentID, 0, 10)
	if err != nil || len(queued) != 2 {
		t.Fatalf("queued=%v err=%v", queued, err)
	}
}

func TestUpcomingSchedulesPromptAndAggregateBounds(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "runtime.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	root, err := store.Create(SessionKindAgent, t.TempDir(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	prompt := strings.Repeat("界", 4000)
	for range 40 {
		if _, err := store.AddSchedule(root, "@every 1h", prompt, anchor); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.RootCollectionPage(t.Context(), root, "schedules", CollectionPageOptions{Limit: 1, MaxBytes: 4096})
	if err != nil || len(page.Items) != 1 || page.Items[0].Body == nil {
		t.Fatalf("full prompt content reference: page=%+v err=%v", page, err)
	}
	body, _, err := store.ReadContent(t.Context(), page.Items[0].Body.ReferenceID, root, "", 0, MaxContentRead)
	if err != nil {
		t.Fatal(err)
	}
	var entry CollectionEntry
	if err := json.Unmarshal(body, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Schedule == nil || entry.Schedule.Prompt != prompt {
		t.Fatal("collection content did not recover the exact full prompt")
	}
	for _, budget := range []int{4096, 32768} {
		got, err := store.SnapshotRootView(t.Context(), root, SnapshotViewOptions{RecentMessages: 1, CollectionLimit: 40, MaxBytes: budget})
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(got)
		rows := *got.UpcomingSchedules
		if len(encoded) > budget || *got.UpcomingScheduleCount != 40 || len(rows) == 0 || rows[0].ID != 1 {
			t.Fatalf("budget=%d size=%d rows=%d", budget, len(encoded), len(rows))
		}
		if got.Omitted["upcoming_schedules"] != (len(rows) < 40) {
			t.Fatalf("inaccurate omission: rows=%d omitted=%v", len(rows), got.Omitted)
		}
		for _, row := range rows {
			if !row.PromptTruncated || len(row.Prompt) > upcomingPromptBytes || !utf8.ValidString(row.Prompt) || !strings.HasPrefix(prompt, row.Prompt) || row.NextFire != formatStamp(anchor) {
				t.Fatalf("invalid preview: %+v", row)
			}
		}
	}
}
