package store

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

func scheduleSpec(expression string) session.ScheduleSpec {
	return session.ScheduleSpec{Expression: expression, Parts: []session.Part{{Type: "text", Text: "scheduled work"}}}
}

func makeSchedule(t *testing.T, s *Store, owner session.SessionID, id, expression string) session.ScheduleMetadata {
	t.Helper()
	result, err := s.CreateSchedule(t.Context(), owner, session.ScheduleID(id), scheduleSpec(expression))
	if err != nil {
		t.Fatal(err)
	}
	return *result.Schedule
}

func TestScheduleExactOccurrenceConcurrentRetryAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	other := openTest(t, path)
	_, owner := create(t, s, nil)
	spec := scheduleSpec("@at 2020-01-02T03:04:05.123456789-07:00")
	created, err := s.CreateSchedule(t.Context(), owner.ID, "once", spec)
	if err != nil {
		t.Fatal(err)
	}
	due := *created.Schedule.NextDue
	results := make([]Admission, 2)
	failures := make([]error, 2)
	var wg sync.WaitGroup
	for i, db := range []*Store{s, other} {
		wg.Go(func() { results[i], failures[i] = db.FireSchedule(t.Context(), "once", due) })
	}
	wg.Wait()
	if failures[0] != nil || failures[1] != nil || results[0].Input.ID != results[1].Input.ID {
		t.Fatalf("double fire %+v %v", results, failures)
	}
	input := results[0].Input
	if input.Source != session.ScheduledInput || input.Kind != session.PromptInput || input.Schedule == nil || !input.Schedule.ScheduledFor.Equal(due) || due.Nanosecond() != 123456789 {
		t.Fatalf("lost provenance %+v", input)
	}
	if _, err := s.FireSchedule(t.Context(), "once", due.Add(time.Nanosecond)); !errors.Is(err, ErrConflict) {
		t.Fatalf("off grid %v", err)
	}
	read, err := s.Schedule(t.Context(), owner.ID, "once")
	if err != nil || read.NextDue != nil || read.Latest.InputID != input.ID || !reflect.DeepEqual(read.Parts, spec.Parts) {
		t.Fatalf("read %+v %v", read, err)
	}
	if used := resourceState(t, s, owner.ID, session.ResourceSchedules).Used; used != 0 {
		t.Fatal(used)
	}
	turn := claim(t, s, owner.ID)
	if _, err := s.Finish(t.Context(), turn.Turn.ID, session.Failed, new("provider failed"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	retry, err := reopened.CreateSchedule(t.Context(), owner.ID, "once", spec)
	if err != nil || retry.DeletedAt == nil || retry.Schedule != nil {
		t.Fatalf("tombstone %+v %v", retry, err)
	}
	fire, err := reopened.FireSchedule(t.Context(), "once", due)
	if err != nil || fire.Input != nil || fire.Receipt.DeletedAt == nil {
		t.Fatalf("receipt tombstone %+v %v", fire, err)
	}
	changed := spec
	changed.Parts = []session.Part{{Type: "text", Text: "changed"}}
	if _, err := s.CreateSchedule(t.Context(), owner.ID, "once", changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestScheduleAtomicFailuresBackpressureAndLifecycle(t *testing.T) {
	for _, fault := range []struct{ name, sql string }{{"input", "CREATE TRIGGER fail BEFORE INSERT ON inputs BEGIN SELECT RAISE(ABORT,'fault'); END"}, {"receipt", "CREATE TRIGGER fail BEFORE INSERT ON receipts BEGIN SELECT RAISE(ABORT,'fault'); END"}, {"charge", "CREATE TRIGGER fail BEFORE INSERT ON logical_writes WHEN NEW.source_kind='input' BEGIN SELECT RAISE(ABORT,'fault'); END"}, {"cursor", "CREATE TRIGGER fail BEFORE UPDATE ON schedules BEGIN SELECT RAISE(ABORT,'fault'); END"}} {
		t.Run(fault.name, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			value := makeSchedule(t, s, owner.ID, "repeat", "@every 0.000000001s")
			due := *value.NextDue
			execTest(t, s, fault.sql)
			if _, err := s.FireSchedule(t.Context(), value.ID, due); err == nil {
				t.Fatal("fault ignored")
			}
			if count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 || count(t, s, "logical_writes") != 1 {
				t.Fatal("partial fire")
			}
			after, _ := s.Schedule(t.Context(), owner.ID, value.ID)
			if !after.NextDue.Equal(due) {
				t.Fatal("slot consumed")
			}
			execTest(t, s, "DROP TRIGGER fail")
			admission, err := s.FireSchedule(t.Context(), value.ID, due)
			if err != nil {
				t.Fatal(err)
			}
			next := due.Add(time.Nanosecond)
			if _, err := s.FireSchedule(t.Context(), value.ID, next); !errors.Is(err, ErrBusy) {
				t.Fatalf("queued backpressure %v", err)
			}
			turn := claim(t, s, owner.ID)
			if _, err := s.FireSchedule(t.Context(), value.ID, next); !errors.Is(err, ErrBusy) {
				t.Fatalf("running backpressure %v", err)
			}
			if _, err := s.Finish(t.Context(), turn.Turn.ID, session.Failed, new("failed"), nil); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
				t.Fatal(err)
			}
			if _, err := s.FireSchedule(t.Context(), value.ID, next); !errors.Is(err, ErrStopped) {
				t.Fatal(err)
			}
			if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Active); err != nil {
				t.Fatal(err)
			}
			second, err := s.FireSchedule(t.Context(), value.ID, next)
			if err != nil || second.Input.ID == admission.Input.ID {
				t.Fatalf("catchup %+v %v", second, err)
			}
			if _, err := s.CancelSchedule(t.Context(), owner.ID, value.ID); err != nil {
				t.Fatal(err)
			}
			input, _ := s.Input(t.Context(), second.Input.ID)
			if input.State != session.Queued {
				t.Fatal("cancel discarded accepted work")
			}
			if _, err := s.FireSchedule(t.Context(), value.ID, next.Add(time.Nanosecond)); !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
		})
	}
}

func TestScheduleCapacityAndOwnershipRollback(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, []session.ResourceLimit{{Kind: session.ResourceSchedules, Limit: new(int64(1))}})
	value := makeSchedule(t, s, owner.ID, "first", "@at 2500-01-01T00:00:00.000000001Z")
	if _, err := s.CreateSchedule(t.Context(), owner.ID, "second", scheduleSpec("@every 1m")); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if count(t, s, "schedules") != 1 || count(t, s, "logical_writes") != 1 {
		t.Fatal("creation leaked")
	}
	_, foreign := create(t, s, nil)
	if _, err := s.CancelSchedule(t.Context(), foreign.ID, value.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Schedule(t.Context(), foreign.ID, value.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "user", RequestID: "forged"}, Submission{SessionID: owner.ID, Source: session.ScheduledInput, Parts: scheduleSpec("@every 1m").Parts}); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("forged source %v", err)
	}
	if _, err := s.CancelSchedule(t.Context(), owner.ID, value.ID); err != nil {
		t.Fatal(err)
	}
	makeSchedule(t, s, owner.ID, "second", "@every 1m")
	resourceLimit(t, s, owner.ID, session.ResourceQueuedInputs, 0)
	second, _ := s.Schedule(t.Context(), owner.ID, "second")
	if _, err := s.FireSchedule(t.Context(), "second", *second.NextDue); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	if count(t, s, "inputs") != 0 || count(t, s, "logical_writes") != 2 {
		t.Fatal("pressure leaked charge/input")
	}
}

func TestScheduleOverflowVisibleAndDoesNotSkip(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	value := makeSchedule(t, s, owner.ID, "overflow", "@every 1s")
	end := time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
	execTest(t, s, "UPDATE schedules SET next_due='9999-12-31T23:59:59.999999999Z' WHERE id='overflow'")
	if _, err := s.fireSchedule(t.Context(), value.ID, end, end); !errors.Is(err, ErrScheduleBlocked) {
		t.Fatal(err)
	}
	read, err := s.Schedule(t.Context(), owner.ID, value.ID)
	if err != nil || read.Failure == nil || !read.NextDue.Equal(end) {
		t.Fatalf("overflow %+v %v", read, err)
	}
	if count(t, s, "inputs") != 0 || count(t, s, "logical_writes") != 1 {
		t.Fatal("overflow admitted work")
	}
	if _, err := s.CancelSchedule(t.Context(), owner.ID, value.ID); err != nil {
		t.Fatal(err)
	}
	if resourceState(t, s, owner.ID, session.ResourceSchedules).Used != 0 {
		t.Fatal("cancel did not release")
	}
}

func TestScheduleListKeysetsAndOperationAtomicAuthority(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	for _, id := range []string{"a", "b", "c"} {
		makeSchedule(t, s, owner.ID, id, "@at 2000-01-01T00:00:00.123456789Z")
	}
	page, err := s.Schedules(t.Context(), owner.ID, session.ScheduleList{Upcoming: true, Limit: 2})
	if err != nil || len(page.Items) != 2 || page.NextCursor == nil {
		t.Fatalf("page %+v %v", page, err)
	}
	next, err := s.Schedules(t.Context(), owner.ID, session.ScheduleList{Upcoming: true, Cursor: page.NextCursor, Limit: 2})
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != "c" || next.NextCursor != nil {
		t.Fatalf("next %+v %v", next, err)
	}
	spec := operationSpec(cell, "create_schedule")
	spec.Capability = "schedules.create"
	spec.Resource = string(owner.TreeID)
	spec.Arguments = []byte(`{"expression":"@every 10m","parts":[{"type":"text","text":"guest"}]}`)
	grant, err := s.CreateGrant(t.Context(), session.Grant{ID: "schedule_grant", SessionID: owner.ID, Capability: spec.Capability, Resource: spec.Resource})
	if err != nil {
		t.Fatal(err)
	}
	operation := admitOperation(t, s, spec)
	execTest(t, s, "CREATE TRIGGER fail_schedule_settle BEFORE UPDATE ON operations WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'fault'); END")
	if _, err := s.ApplyScheduleOperation(t.Context(), operation.ID); err == nil {
		t.Fatal("settlement fault ignored")
	}
	if count(t, s, "schedules") != 3 || count(t, s, "logical_writes") != 3 {
		t.Fatal("operation partial commit")
	}
	execTest(t, s, "DROP TRIGGER fail_schedule_settle")
	result, err := s.ApplyScheduleOperation(t.Context(), operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.ApplyScheduleOperation(t.Context(), operation.ID)
	if err != nil || !reflect.DeepEqual(result, replay) || count(t, s, "schedules") != 4 {
		t.Fatalf("operation retry %s %s %v", result, replay, err)
	}
	spec.ID = "revoked_schedule"
	spec.RequestID = "revoked_schedule"
	operation = admitOperation(t, s, spec)
	if _, err := s.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyScheduleOperation(t.Context(), operation.ID); err == nil {
		t.Fatal("revoked authority admitted schedule")
	}
	if count(t, s, "schedules") != 4 {
		t.Fatal("revocation leak")
	}
}

func TestScheduleSweepVisitsEachRecurrenceOnce(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	for i := range 9 {
		makeSchedule(t, s, owner.ID, fmt.Sprintf("recurring_%02d", i), "@every 0.000000001s")
	}
	makeSchedule(t, s, owner.ID, "later", "@at 2025-01-01T00:00:00Z")
	// Put recurring cursors before the later one. This simulates a long outage;
	// subsequent inputs still pass through the actual transaction and queue.
	execTest(t, s, "UPDATE schedules SET next_due='2000-01-01T00:00:00.000000000Z' WHERE id LIKE 'recurring_%'")
	through, err := s.ScheduleSweep(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var cursor *session.ScheduleCursor
	seen := map[session.ScheduleID]bool{}
	for pass := range 3 {
		page, err := s.DueSchedules(t.Context(), cursor, through, 4)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page {
			if seen[item.ID] {
				t.Fatalf("schedule repeated within bounded sweep: %s", item.ID)
			}
			seen[item.ID] = true
			admission, err := s.FireSchedule(t.Context(), item.ID, *item.NextDue)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.CancelInput(t.Context(), admission.Input.ID); err != nil {
				t.Fatal(err)
			}
			cursor = &session.ScheduleCursor{Due: *item.NextDue, ID: item.ID}
		}
		if pass == 2 && len(page) != 2 {
			t.Fatalf("expected sweep end: %+v", page)
		}
	}
	if !seen["later"] || len(seen) != 10 {
		t.Fatal("starved later schedule", seen)
	}
	next, err := s.DueSchedules(t.Context(), nil, through, 100)
	if err != nil || len(next) != 0 {
		t.Fatalf("admitted rows revisited %+v %v", next, err)
	}
	newThrough, err := s.ScheduleSweep(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.DueSchedules(t.Context(), nil, newThrough, 100)
	if err != nil || len(again) != 9 {
		t.Fatalf("next sweep lost recurrences %+v %v", again, err)
	}
}

func TestScheduleAnchorContentAndPreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	_, foreign := create(t, s, nil)
	ref, err := s.RegisterContent(t.Context(), contentReference(owner.ID, "attachment", "body"))
	if err != nil {
		t.Fatal(err)
	}
	template := session.ScheduleSpec{Expression: "@every 10m", Parts: []session.Part{{Type: "text", Text: strings.Repeat("🌊", 1000)}, {Type: "content", ReferenceID: ref.ID}}}
	if _, err := s.CreateSchedule(t.Context(), foreign.ID, "foreign", template); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign content %v", err)
	}
	if count(t, s, "schedules") != 0 {
		t.Fatal("foreign template leaked")
	}
	first, err := s.CreateSchedule(t.Context(), owner.ID, "anchor", template)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.FireSchedule(t.Context(), first.ID, *first.Schedule.NextDue); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	template.Expression = "@every 600s"
	replay, err := reopened.CreateSchedule(t.Context(), owner.ID, "anchor", template)
	if err != nil || !replay.Schedule.FirstDue.Equal(first.Schedule.FirstDue) || !replay.Schedule.NextDue.Equal(first.Schedule.FirstDue.Add(10*time.Minute)) {
		t.Fatalf("anchor moved %+v %v", replay, err)
	}
	if !replay.Schedule.PreviewTruncated || !utf8.ValidString(replay.Schedule.Preview) || len(replay.Schedule.Preview) > 2048 {
		t.Fatalf("unbounded preview %+v", replay)
	}
	full, err := reopened.Schedule(t.Context(), owner.ID, first.ID)
	if err != nil || !reflect.DeepEqual(full.Parts, template.Parts) {
		t.Fatalf("template hydration %+v %v", full, err)
	}
	before := count(t, s, "inputs")
	due := *replay.Schedule.NextDue
	if _, err := s.fireSchedule(t.Context(), first.ID, due, due.Add(-time.Nanosecond)); !errors.Is(err, ErrNoWork) {
		t.Fatalf("early rounded admission %v", err)
	}
	if count(t, s, "inputs") != before {
		t.Fatal("future admitted")
	}
}

func TestScheduleReceiptNamespaceCannotBeOccupiedByPublicAdmissions(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	value := makeSchedule(t, s, owner.ID, "protected", "@at 2000-01-01T00:00:00.123456789Z")
	slot := "2000-01-01T00:00:00.123456789Z"
	identity := session.RequestIdentity{ClientID: "schedule", RequestID: fmt.Sprintf("slot_%x", sha256.Sum256([]byte("protected\x00"+slot)))}
	for _, clientID := range []string{"schedule", "operation"} {
		requestID := identity
		requestID.ClientID = clientID
		for _, kind := range []session.InputKind{session.PromptInput, session.CompactInput} {
			request := Submission{SessionID: owner.ID, Source: session.UserInput, Kind: kind}
			if kind == session.PromptInput {
				request.Parts = []session.Part{{Type: "text", Text: "collision"}}
			}
			if _, err := s.Admit(t.Context(), requestID, request); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("reserved %s/%s admitted: %v", clientID, kind, err)
			}
		}
		if _, err := s.SpawnChild(t.Context(), requestID, childRequest(owner.ID)); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("reserved child receipt admitted: %v", err)
		}
	}
	if count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 || count(t, s, "sessions") != 1 {
		t.Fatal("reserved identity left rows")
	}
	fired, err := s.FireSchedule(t.Context(), value.ID, *value.NextDue)
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.Admission(t.Context(), identity)
	if err != nil || read.Input.ID != fired.Input.ID {
		t.Fatalf("internal receipt unreadable %+v %v", read, err)
	}
}
