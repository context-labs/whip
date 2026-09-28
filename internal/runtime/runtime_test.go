package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/runner"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type providerFunc func(context.Context, model.Request) (model.Response, error)

func (f providerFunc) Prepare(ctx context.Context, request model.Request) (model.Prepared, error) {
	prepared, err := (model.Scripted{}).Prepare(ctx, request)
	prepared.Execute = func(ctx context.Context, _ func(model.Chunk)) (model.Response, error) { return f(ctx, request) }
	return prepared, err
}

func openTest(t *testing.T, path string, p runner.Provider) *Runtime {
	t.Helper()
	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := Open(t.Context(), path, p, Options{Workers: 2, PollInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	})
	return r
}

func createTest(t *testing.T, r *Runtime) session.Session {
	t.Helper()
	refs, err := r.Builtins()
	if err != nil {
		t.Fatal(err)
	}
	_, s, err := r.CreateTree(t.Context(), store.CreateTree{Engine: session.Starlark, Policy: session.TreePolicy{MaxDepth: 4, MaxSessions: 100, MaxQueuedInputsPerSession: 100}, Definition: refs[0], WorkingDirectory: t.TempDir(), Overrides: session.ConfigPatch{Model: &session.ModelSelection{Provider: "scripted", Name: "scripted"}}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func submitTest(t *testing.T, r *Runtime, s session.SessionID, key string) store.Admission {
	t.Helper()
	a, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, store.Submission{SessionID: s, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: key}}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func waitTest(t *testing.T, r *Runtime, key string, predicate func(store.Admission) bool) store.Admission {
	t.Helper()
	return waitTestWithin(t, r, key, predicate, 5*time.Second)
}

func waitTestWithin(t *testing.T, r *Runtime, key string, predicate func(store.Admission) bool, timeout time.Duration) store.Admission {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		a, err := r.Admission(ctx, session.RequestIdentity{ClientID: "test", RequestID: key})
		if err != nil {
			t.Fatal(err)
		}
		if predicate(a) {
			return a
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timeout waiting for %s: %+v runtime=%v", key, a, r.Err())
		case <-ticker.C:
		}
	}
}
func terminal(a store.Admission) bool { return a.Turn != nil && a.Turn.FinishedAt != nil }

func TestReadingQueuedHistoryDoesNotExecuteAndRestartResumes(t *testing.T) {
	path := t.TempDir()
	var calls atomic.Int32
	provider := providerFunc(func(ctx context.Context, r model.Request) (model.Response, error) {
		calls.Add(1)
		return (model.Scripted{}).Complete(ctx, r)
	})
	r := openTest(t, path, provider)
	s := createTest(t, r)
	original := submitTest(t, r, s.ID, "survives")
	history, err := r.History(t.Context(), s.ID, 0, 100)
	if err != nil || len(history) != 0 || calls.Load() != 0 {
		t.Fatalf("history=%v calls=%d err=%v", history, calls.Load(), err)
	}
	identity := r.Identity()
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path, provider)
	if r.Identity() != identity {
		t.Fatal("runtime identity changed")
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	a := waitTest(t, r, "survives", terminal)
	if a.Input.ID != original.Input.ID || a.Turn.State != session.Succeeded || calls.Load() != 1 {
		t.Fatalf("admission=%+v calls=%d", a, calls.Load())
	}
	history, err = r.History(t.Context(), s.ID, 0, 100)
	if err != nil || len(history) != 2 || history[0].Role != session.User || history[1].Parts[0].Text != "ack: survives" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
}

func TestExecutionOwnershipAndSingleTurnPerSession(t *testing.T) {
	path := t.TempDir()
	entered := make(chan session.SessionID, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	active := map[session.SessionID]int{}
	overlap := false
	r := openTest(t, path, providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
		mu.Lock()
		active[request.SessionID]++
		if active[request.SessionID] > 1 {
			overlap = true
		}
		mu.Unlock()
		defer func() { mu.Lock(); active[request.SessionID]--; mu.Unlock() }()
		select {
		case entered <- request.SessionID:
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
		select {
		case <-release:
		case <-ctx.Done():
			return model.Response{}, ctx.Err()
		}
		return (model.Scripted{}).Complete(ctx, request)
	}))
	if second, err := Open(t.Context(), path, model.Scripted{}, Options{}); !errors.Is(err, ErrOwned) {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second owner: %v", err)
	}
	first, second := createTest(t, r), createTest(t, r)
	for _, key := range []string{"a", "b"} {
		submitTest(t, r, first.ID, key)
	}
	submitTest(t, r, second.ID, "c")
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("independent session blocked")
		}
	}
	queued, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "b"})
	if err != nil || queued.Turn != nil {
		t.Fatalf("same session overlapped: %+v %v", queued, err)
	}
	close(release)
	for _, key := range []string{"a", "b", "c"} {
		if a := waitTest(t, r, key, terminal); a.Turn.State != session.Succeeded {
			t.Fatalf("%s: %+v", key, a.Turn)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if overlap {
		t.Fatal("provider requests overlapped in one session")
	}
}

func TestObserverCancellationAndExplicitCancellationHaveDifferentLifetimes(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{Delay: time.Second})
	s := createTest(t, r)
	ctx, cancel := context.WithCancel(t.Context())
	a, err := r.Admit(ctx, session.RequestIdentity{ClientID: "test", RequestID: "cancel"}, store.Submission{SessionID: s.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "cancel"}}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Turn != nil {
		t.Fatal("admission started execution before runtime Start")
	}
	cancel()
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	a = waitTest(t, r, "cancel", func(a store.Admission) bool { return a.Turn != nil })
	if a.Turn.State != session.Running {
		t.Fatalf("observer cancelled execution: %+v", a.Turn)
	}
	if _, err := r.CancelInput(t.Context(), a.Input.ID); err != nil {
		t.Fatal(err)
	}
	a = waitTest(t, r, "cancel", terminal)
	if a.Turn.State != session.Cancelled {
		t.Fatalf("explicit cancellation: %+v", a.Turn)
	}
}

func TestShutdownInterruptsClaimedInputWithoutReplayingIt(t *testing.T) {
	path := t.TempDir()
	r := openTest(t, path, model.Scripted{Delay: time.Hour})
	s := createTest(t, r)
	submitTest(t, r, s.ID, "claimed")
	submitTest(t, r, s.ID, "queued")
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitTest(t, r, "claimed", func(a store.Admission) bool { return a.Turn != nil })
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, path, model.Scripted{})
	a := waitTest(t, r, "claimed", terminal)
	if a.Turn.State != session.Interrupted {
		t.Fatalf("shutdown outcome=%+v", a.Turn)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitTest(t, r, "queued", terminal)
	history, err := r.History(t.Context(), s.ID, 0, 100)
	if err != nil || len(history) != 3 {
		t.Fatalf("claimed input replayed: %+v %v", history, err)
	}
}

func TestDeletingQueuedSessionsDoesNotFaultOtherWorkers(t *testing.T) {
	r := openTest(t, t.TempDir(), model.Scripted{})
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i := range 30 {
		s := createTest(t, r)
		submitTest(t, r, s.ID, fmt.Sprintf("delete%d", i))
		err := r.DeleteSubtree(t.Context(), s.ID)
		if err != nil && !errors.Is(err, store.ErrBusy) {
			t.Fatal(err)
		}
	}
	survivor := createTest(t, r)
	submitTest(t, r, survivor.ID, "survivor")
	if a := waitTest(t, r, "survivor", terminal); a.Turn.State != session.Succeeded || r.Err() != nil {
		t.Fatalf("unrelated work failed: %+v %v", a.Turn, r.Err())
	}
}
