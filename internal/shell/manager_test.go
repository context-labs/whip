package shell

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/bashrun"
)

func capture(t *testing.T, m *Manager, id string) *Scope {
	t.Helper()
	s, err := m.Capture(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func start(t *testing.T, s *Scope, id, command string) *bashrun.Job {
	t.Helper()
	r, err := s.Reserve(true)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Release()
	job, err := r.Start(t.Context(), id, bashrun.Options{Command: command, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func wait(t *testing.T, job *bashrun.Job) {
	t.Helper()
	select {
	case <-job.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("job did not finish")
	}
}

func TestConcurrentReservationsRespectOwnerAndGlobalCapacity(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	s := capture(t, m, "one")
	var workers sync.WaitGroup
	reservations := make(chan *Reservation, 64)
	for range 64 {
		workers.Go(func() {
			r, err := s.Reserve(true)
			if err == nil {
				t.Cleanup(r.Release)
				reservations <- r
			} else if !errors.Is(err, ErrLimit) {
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if len(reservations) != MaxOwnerRunning {
		t.Fatalf("reserved %d", len(reservations))
	}
	for owner := 1; owner < MaxRunning/MaxOwnerRunning; owner++ {
		scope := capture(t, m, strconv.Itoa(owner))
		for range MaxOwnerRunning {
			r, err := scope.Reserve(false)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(r.Release)
			reservations <- r
		}
	}
	if _, err := capture(t, m, "overflow").Reserve(false); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	close(reservations)
	for r := range reservations {
		r.Release()
		r.Release()
	}
	// Abandoned background starts must release retained-output reservations too.
	for range MaxJobs + 1 {
		r, err := s.Reserve(true)
		if err != nil {
			t.Fatal(err)
		}
		r.Release()
	}
}

func TestRetirementJoinsOnlyOwnedJobsAndRejectsStaleGenerations(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	one, two := capture(t, m, "one"), capture(t, m, "two")
	first := start(t, one, "same-id", "echo one; sleep 30")
	second := start(t, two, "same-id", "echo two; sleep 30")
	m.Retire("one")
	if first.Running() || !second.Running() {
		t.Fatal("retirement crossed owner boundary or returned early")
	}
	if _, err := one.Reserve(false); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	if one.Context().Err() == nil {
		t.Fatal("prepared generation was not cancelled")
	}
	fresh := capture(t, m, "one")
	if fresh == one {
		t.Fatal("retired generation reused")
	}
	if _, err := fresh.Job("same-id"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := second.Kill(); err != nil {
		t.Fatal(err)
	}
	if second.Running() {
		t.Fatal("kill did not join")
	}
}

func TestCloseWaitsForUndispatchedReservationAndPreventsRestart(t *testing.T) {
	m := NewManager()
	s := capture(t, m, "owner")
	r, err := s.Reserve(false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Release)
	done := make(chan struct{})
	go func() { m.Close(); close(done) }()
	select {
	case <-s.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("close did not cancel generation")
	}
	select {
	case <-done:
		t.Fatal("close returned before reservation joined")
	default:
	}
	r.Release()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("close did not join")
	}
	if _, err := m.Capture("owner"); !errors.Is(err, ErrClosed) {
		t.Fatal(err)
	}
	m.Close()
}

func TestRetainedJobEvictionKeepsNewestAndRunningEvidence(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	s := capture(t, m, "owner")
	live := start(t, s, "live", "sleep 30")
	for i := range MaxOwnerJobs + 2 {
		job := start(t, s, fmt.Sprintf("done-%02d", i), "printf done")
		wait(t, job)
	}
	ids, err := s.JobIDs()
	if err != nil || len(ids) != MaxOwnerJobs {
		t.Fatal(ids, err)
	}
	if _, err := s.Job("done-00"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if job, err := s.Job("live"); err != nil || job != live || !job.Running() {
		t.Fatal(job, err)
	}
	if _, err := s.Job(fmt.Sprintf("done-%02d", MaxOwnerJobs+1)); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentDuplicateStartDoesNotOverwriteLiveJob(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	s := capture(t, m, "owner")
	var workers sync.WaitGroup
	results := make(chan error, 2)
	dir := t.TempDir()
	for range 2 {
		r, err := s.Reserve(true)
		if err != nil {
			t.Fatal(err)
		}
		workers.Go(func() {
			defer r.Release()
			_, err := r.Start(t.Context(), "duplicate", bashrun.Options{Command: "sleep 30", Cwd: dir})
			results <- err
		})
	}
	workers.Wait()
	close(results)
	var successes int
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrClosed) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("started %d jobs with one identity", successes)
	}
}

func TestReleaseDuringForegroundCallWaitsForCompletion(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	s := capture(t, m, "owner")
	r, err := s.Reserve(false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Release)
	entered, unblock := make(chan struct{}), make(chan struct{})
	release := sync.OnceFunc(func() { close(unblock) })
	t.Cleanup(release)
	signal := sync.OnceFunc(func() { close(entered) })
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		_, _ = r.Run(t.Context(), bashrun.Options{Command: "echo ready; sleep .2", Timeout: 5 * time.Second, OnUpdate: func(string) { signal(); <-unblock }})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("no callback")
	}
	released := make(chan struct{})
	go func() { r.Release(); close(released) }()
	select {
	case <-released:
		t.Fatal("released a still-running invocation")
	case <-time.After(300 * time.Millisecond):
	}
	// Its capacity is still held until the joined callback completes.
	held := make([]*Reservation, 0, MaxOwnerRunning-1)
	for range MaxOwnerRunning - 1 {
		reservation, err := s.Reserve(false)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(reservation.Release)
		held = append(held, reservation)
	}
	if _, err := s.Reserve(false); !errors.Is(err, ErrLimit) {
		t.Fatal(err)
	}
	release()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("release did not join")
	}
	<-runDone
	for _, reservation := range held {
		reservation.Release()
	}
}

func TestGlobalRetentionEvictsFinishedJobsForNewOwner(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Close)
	for owner := range MaxJobs / MaxOwnerJobs {
		s := capture(t, m, strconv.Itoa(owner))
		for i := range MaxOwnerJobs {
			wait(t, start(t, s, strconv.Itoa(i), "true"))
		}
	}
	s := capture(t, m, "new-owner")
	wait(t, start(t, s, "new", "true"))
	var records int
	for _, owner := range m.Owners() {
		ids, err := capture(t, m, owner).JobIDs()
		if err != nil {
			t.Fatal(err)
		}
		records += len(ids)
	}
	if records != MaxJobs {
		t.Fatalf("retained %d jobs", records)
	}
}
