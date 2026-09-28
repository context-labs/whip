package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func capacityRuntime(t *testing.T, provider providerFunc, maxActive int) *Runtime {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := Open(t.Context(), directory, provider, Options{Workers: 1, MaxActiveTurns: maxActive, PollInterval: time.Millisecond})
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

func capacitySignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worker capacity event")
	}
}

func capacityWait(ctx context.Context, signal <-chan struct{}) error {
	select {
	case <-signal:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func capacityState(t *testing.T, r *Runtime, active, runnable, waiting, resumptions int) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		r.mu.Lock()
		a, n, w, q := len(r.active), r.runnable, r.waiting, len(r.resumptions)
		r.mu.Unlock()
		if a == active && n == runnable && w == waiting && q == resumptions {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("capacity=(%d,%d,%d,%d), want (%d,%d,%d,%d), runtime=%v", a, n, w, q, active, runnable, waiting, resumptions, r.Err())
		case <-ticker.C:
		}
	}
}

func TestWorkerCapacityYieldPreservesOwnershipAndAlternatesAdmission(t *testing.T) {
	waiting := make(chan struct{})
	finishWait := make(chan struct{})
	childStarted, releaseChild := make(chan struct{}), make(chan struct{})
	firstResume, yieldAgain := make(chan struct{}), make(chan struct{})
	freshStarted, releaseFresh := make(chan struct{}), make(chan struct{})
	secondResume := make(chan struct{})
	waitFailure := errors.New("wait completed with a domain error")
	var r *Runtime
	var parentID, childID session.SessionID
	r = capacityRuntime(t, providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
		switch request.SessionID {
		case parentID:
			err := r.withReleasedWorker(ctx, request.TurnID, func(ctx context.Context) error {
				close(waiting)
				if err := capacityWait(ctx, finishWait); err != nil {
					return err
				}
				return waitFailure
			})
			if !errors.Is(err, waitFailure) {
				return model.Response{}, fmt.Errorf("wait result: %w", err)
			}
			close(firstResume)
			if err := capacityWait(ctx, yieldAgain); err != nil {
				return model.Response{}, err
			}
			if err := r.withReleasedWorker(ctx, request.TurnID, func(context.Context) error { return nil }); err != nil {
				return model.Response{}, err
			}
			close(secondResume)
		case childID:
			close(childStarted)
			if err := capacityWait(ctx, releaseChild); err != nil {
				return model.Response{}, err
			}
		default:
			close(freshStarted)
			if err := capacityWait(ctx, releaseFresh); err != nil {
				return model.Response{}, err
			}
		}
		return (model.Scripted{}).Complete(ctx, request)
	}), 4)
	parent := createTest(t, r)
	parentID = parent.ID
	submitTest(t, r, parentID, "parent")
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, store.ChildRequest{
		ParentID: parentID, Parts: []session.Part{{Type: "text", Text: "child"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	childID = child.Session.ID
	fresh := createTest(t, r)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	capacitySignal(t, waiting)
	capacitySignal(t, childStarted)
	submitTest(t, r, fresh.ID, "fresh")
	followup := submitTest(t, r, parentID, "followup")
	if followup.Turn != nil {
		t.Fatal("yielding released the parent's durable turn ownership")
	}
	if _, err := r.CancelInput(t.Context(), followup.Input.ID); err != nil {
		t.Fatal(err)
	}
	close(finishWait)
	capacityState(t, r, 2, 1, 1, 1)
	close(releaseChild)
	capacitySignal(t, firstResume)
	capacityState(t, r, 1, 1, 0, 0)
	select {
	case <-freshStarted:
		t.Fatal("fresh admission bypassed an already waiting resumption")
	default:
	}
	close(yieldAgain)
	capacitySignal(t, freshStarted)
	capacityState(t, r, 2, 1, 1, 1)
	select {
	case <-secondResume:
		t.Fatal("repeated parent resumption starved fresh queued work")
	default:
	}
	close(releaseFresh)
	capacitySignal(t, secondResume)
	for _, key := range []string{"parent", "child", "fresh"} {
		if a := waitTest(t, r, key, terminal); a.Turn.State != session.Succeeded {
			t.Fatalf("%s ended with %+v", key, a.Turn)
		}
	}
	capacityState(t, r, 0, 0, 0, 0)
}

func TestWorkerCapacityCancellationDuringWaitAndResumption(t *testing.T) {
	for _, reacquiring := range []bool{false, true} {
		t.Run(fmt.Sprintf("reacquiring=%t", reacquiring), func(t *testing.T) {
			waiting, finishWait := make(chan struct{}), make(chan struct{})
			childStarted, releaseChild := make(chan struct{}), make(chan struct{})
			returned := make(chan error, 1)
			var r *Runtime
			var parentID session.SessionID
			r = capacityRuntime(t, providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.SessionID == parentID {
					err := r.withReleasedWorker(ctx, request.TurnID, func(ctx context.Context) error {
						close(waiting)
						return capacityWait(ctx, finishWait)
					})
					returned <- err
					return model.Response{}, err
				}
				close(childStarted)
				if err := capacityWait(ctx, releaseChild); err != nil {
					return model.Response{}, err
				}
				return (model.Scripted{}).Complete(ctx, request)
			}), 3)
			parent, child := createTest(t, r), createTest(t, r)
			parentID = parent.ID
			submitTest(t, r, parent.ID, "parent")
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			capacitySignal(t, waiting)
			submitTest(t, r, child.ID, "child")
			capacitySignal(t, childStarted)
			if reacquiring {
				close(finishWait)
				capacityState(t, r, 2, 1, 1, 1)
			}
			a := waitTest(t, r, "parent", func(a store.Admission) bool { return a.Turn != nil })
			if _, err := r.CancelTurn(t.Context(), a.Turn.ID); err != nil {
				t.Fatal(err)
			}
			if a := waitTest(t, r, "parent", terminal); a.Turn.State != session.Cancelled {
				t.Fatalf("parent state=%+v", a.Turn)
			}
			if err := <-returned; !errors.Is(err, context.Canceled) {
				t.Fatalf("wait cancellation=%v", err)
			}
			capacityState(t, r, 1, 1, 0, 0)
			close(releaseChild)
			if a := waitTest(t, r, "child", terminal); a.Turn.State != session.Succeeded {
				t.Fatalf("child state=%+v", a.Turn)
			}
			capacityState(t, r, 0, 0, 0, 0)
		})
	}
}

func TestWorkerCapacityBoundLeavesRunnableProgress(t *testing.T) {
	waiting := make(chan struct{})
	results := make(chan error, 2)
	var r *Runtime
	var parentID session.SessionID
	r = capacityRuntime(t, providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
		if request.SessionID == parentID {
			err := r.withReleasedWorker(ctx, request.TurnID, func(ctx context.Context) error {
				results <- r.withReleasedWorker(ctx, request.TurnID, func(context.Context) error {
					return errors.New("nested wait callback must not run")
				})
				close(waiting)
				<-ctx.Done()
				return ctx.Err()
			})
			return model.Response{}, err
		}
		results <- r.withReleasedWorker(ctx, request.TurnID, func(context.Context) error {
			return errors.New("capacity-rejected callback must not run")
		})
		return (model.Scripted{}).Complete(ctx, request)
	}), 2)
	parent, child := createTest(t, r), createTest(t, r)
	parentID = parent.ID
	submitTest(t, r, parent.ID, "parent")
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	capacitySignal(t, waiting)
	if err := <-results; !errors.Is(err, store.ErrBusy) {
		t.Fatalf("nested wait=%v", err)
	}
	submitTest(t, r, child.ID, "child")
	if a := waitTest(t, r, "child", terminal); a.Turn.State != session.Succeeded {
		t.Fatalf("capacity limit prevented child progress: %+v", a.Turn)
	}
	if err := <-results; !errors.Is(err, store.ErrLimit) {
		t.Fatalf("waiting capacity=%v", err)
	}
	capacityState(t, r, 1, 0, 1, 0)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	capacityState(t, r, 0, 0, 0, 0)
}

func TestWorkerCapacityCloseJoinsMoreWaitersThanWorkers(t *testing.T) {
	const waiters = 4
	started := make(chan struct{}, waiters)
	var r *Runtime
	r = capacityRuntime(t, providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
		err := r.withReleasedWorker(ctx, request.TurnID, func(ctx context.Context) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
		return model.Response{}, err
	}), waiters+1)
	for i := range waiters {
		s := createTest(t, r)
		submitTest(t, r, s.ID, fmt.Sprintf("wait%d", i))
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range waiters {
		capacitySignal(t, started)
	}
	capacityState(t, r, waiters, 0, waiters, 0)
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		if err := r.Close(); err != nil {
			t.Error(err)
		}
	}()
	capacitySignal(t, closed)
	capacityState(t, r, 0, 0, 0, 0)
}
