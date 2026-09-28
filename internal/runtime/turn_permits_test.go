package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func permitRuntime(t *testing.T, provider capacityProvider, workers int) *Runtime {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	r, err := Open(t.Context(), directory, provider, Options{Workers: workers, KernelWorkers: 1, MaxActiveTurns: 16, PollInterval: time.Millisecond})
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

func runnableLimit(t *testing.T, r *Runtime, owner session.SessionID, limit int64) {
	t.Helper()
	values, err := r.Resources(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range values {
		if value.SessionID == owner && value.Kind == session.ResourceRunnableDescendants {
			if _, err := r.SetResource(t.Context(), owner, value.Revision, session.ResourceLimit{Kind: value.Kind, Limit: &limit}); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("missing runnable limit")
}

func permitChild(t *testing.T, r *Runtime, parent session.SessionID, key string) store.ChildAdmission {
	t.Helper()
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, store.ChildRequest{
		ParentID: parent, Parts: []session.Part{{Type: "text", Text: key}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return child
}

func TestTurnPermitsScopeConcurrencyIndependentlyOfHostWorkers(t *testing.T) {
	firstStarted, releaseFirst := make(chan struct{}), make(chan struct{})
	secondStarted := make(chan struct{}, 1)
	provider := capacityProvider(func(ctx context.Context, request model.Request) (model.Response, error) {
		switch request.Messages[0].Parts[0].Text {
		case "first":
			close(firstStarted)
			if err := capacityWait(ctx, releaseFirst); err != nil {
				return model.Response{}, err
			}
		case "second":
			secondStarted <- struct{}{}
		}
		return (model.Scripted{}).Complete(ctx, request)
	})
	r := permitRuntime(t, provider, 4)
	root := createTest(t, r)
	runnableLimit(t, r, root.ID, 1)
	permitChild(t, r, root.ID, "first")
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	capacitySignal(t, firstStarted)
	second := permitChild(t, r, root.ID, "second")
	unrelated := createTest(t, r)
	submitTest(t, r, unrelated.ID, "unrelated")
	if result := waitTest(t, r, "unrelated", terminal); result.Turn.State != session.Succeeded {
		t.Fatalf("blocked subtree prevented unrelated progress: %+v", result)
	}
	select {
	case <-secondStarted:
		t.Fatal("host worker availability bypassed subtree cap")
	default:
	}
	queued, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "second"})
	if err != nil || queued.Turn != nil || queued.Input.ID != second.Admission.Input.ID || queued.Input.State != session.Queued {
		t.Fatalf("capacity pressure consumed queued work: %+v %v", queued, err)
	}
	close(releaseFirst)
	capacitySignal(t, secondStarted)
	for _, key := range []string{"first", "second"} {
		if result := waitTest(t, r, key, terminal); result.Turn.State != session.Succeeded {
			t.Fatalf("%s did not finish: %+v", key, result)
		}
	}
}

func TestTurnPermitsBlockedQueuePrefixDoesNotStarveUnrelatedWork(t *testing.T) {
	r := permitRuntime(t, capacityProvider((model.Scripted{}).Complete), 4)
	oldest := createTest(t, r)
	runnableLimit(t, r, oldest.ID, 0)
	permitChild(t, r, oldest.ID, "oldest")
	blocked := createTest(t, r)
	runnableLimit(t, r, blocked.ID, 0)
	// More than both Workers and one ordinary 100-row page must be skipped.
	for i := range 105 {
		permitChild(t, r, blocked.ID, fmt.Sprintf("blocked-%d", i))
	}
	unrelated := createTest(t, r)
	submitTest(t, r, unrelated.ID, "unrelated")
	// Assert fairness within bounded scheduler passes. Wall-clock polling also
	// measures the cost of deliberately rejected claims under race instrumentation.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	var workers sync.WaitGroup
	r.cancel = cancel
	t.Cleanup(func() { cancel(); workers.Wait() })
	step := func() {
		t.Helper()
		if err := r.schedule(ctx, &workers); err != nil {
			t.Fatal(err)
		}
		workers.Wait()
		if err := r.Err(); err != nil {
			t.Fatal(err)
		}
	}
	admission := func(key string) store.Admission {
		t.Helper()
		value, err := r.Admission(ctx, session.RequestIdentity{ClientID: "test", RequestID: key})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	step()
	if value := admission("unrelated"); value.Turn != nil || value.Input.State != session.Queued || r.queueCursor == (store.QueueCursor{}) {
		t.Fatalf("first bounded page did not retain its cursor: %+v cursor=%+v", value, r.queueCursor)
	}
	step()
	if result := admission("unrelated"); result.Turn == nil || result.Turn.State != session.Succeeded {
		t.Fatalf("blocked prefix starved the second page: %+v", result)
	}
	if r.queueCursor != (store.QueueCursor{}) {
		t.Fatalf("completed sweep did not wrap its cursor: %+v", r.queueCursor)
	}
	for _, key := range []string{"blocked-0", "blocked-104"} {
		if value := admission(key); value.Turn != nil || value.Input.State != session.Queued {
			t.Fatalf("blocked input changed: %+v", value)
		}
	}
	runnableLimit(t, r, oldest.ID, 1)
	step()
	if result := admission("oldest"); result.Turn == nil || result.Turn.State != session.Succeeded {
		t.Fatalf("next sweep did not revisit newly eligible earlier work: %+v", result)
	}
}

func TestTurnPermitsBlockedResumptionAllowsFreshWorkAndEndsCleanly(t *testing.T) {
	for _, action := range []string{"increase", "cancel", "deadline"} {
		t.Run(action, func(t *testing.T) {
			waiting, finishWait := make(chan struct{}), make(chan struct{})
			resumed := make(chan struct{}, 1)
			returned := make(chan error, 1)
			var r *Runtime
			provider := capacityProvider(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.Messages[0].Parts[0].Text == "parent" {
					waitCtx := ctx
					if action == "deadline" {
						var cancel context.CancelFunc
						waitCtx, cancel = context.WithTimeout(ctx, 2*time.Second)
						defer cancel()
					}
					err := r.withReleasedWorker(waitCtx, request.TurnID, func(ctx context.Context) error {
						close(waiting)
						return capacityWait(ctx, finishWait)
					})
					returned <- err
					if err != nil {
						return model.Response{}, err
					}
					resumed <- struct{}{}
				}
				return (model.Scripted{}).Complete(ctx, request)
			})
			r = permitRuntime(t, provider, 2)
			root := createTest(t, r)
			runnableLimit(t, r, root.ID, 1)
			permitChild(t, r, root.ID, "parent")
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			capacitySignal(t, waiting)
			runnableLimit(t, r, root.ID, 0)
			close(finishWait)
			capacityState(t, r, 1, 0, 1, 1)
			unrelated := createTest(t, r)
			submitTest(t, r, unrelated.ID, "unrelated")
			if result := waitTest(t, r, "unrelated", terminal); result.Turn.State != session.Succeeded {
				t.Fatalf("blocked resumption starved fresh work: %+v", result)
			}
			select {
			case <-resumed:
				t.Fatal("resumed despite exhausted ancestor cap")
			default:
			}
			want := session.Succeeded
			switch action {
			case "increase":
				runnableLimit(t, r, root.ID, 1)
			case "cancel":
				current := waitTest(t, r, "parent", func(a store.Admission) bool { return a.Turn != nil })
				if _, err := r.CancelTurn(t.Context(), current.Turn.ID); err != nil {
					t.Fatal(err)
				}
				want = session.Cancelled
			case "deadline":
				want = session.Failed
			}
			result := waitTest(t, r, "parent", terminal)
			if result.Turn.State != want {
				t.Fatalf("parent after %s: %+v runtime=%v", action, result, r.Err())
			}
			err := <-returned
			if action == "increase" && err != nil || action == "cancel" && !errors.Is(err, context.Canceled) || action == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("wait result after %s: %v", action, err)
			}
			capacityState(t, r, 0, 0, 0, 0)
			values, err := r.Resources(t.Context(), root.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range values {
				if value.Kind == session.ResourceRunnableDescendants && value.Used != 0 {
					t.Fatalf("terminal waiter leaked SQL permit: %+v", value)
				}
			}
		})
	}
}

func TestTurnPermitsNestedWaitsProgressWithOneWorkerAndScopedSlot(t *testing.T) {
	var r *Runtime
	children := map[session.SessionID]session.InputID{}
	provider := capacityProvider(func(ctx context.Context, request model.Request) (model.Response, error) {
		if input, ok := children[request.SessionID]; ok {
			if err := r.withReleasedWorker(ctx, request.TurnID, func(ctx context.Context) error {
				ticker := time.NewTicker(time.Millisecond)
				defer ticker.Stop()
				for {
					done, err := r.store.ChildInputsComplete(ctx, request.SessionID, []session.InputID{input})
					if err != nil || done {
						return err
					}
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-ticker.C:
					}
				}
			}); err != nil {
				return model.Response{}, err
			}
		}
		return (model.Scripted{}).Complete(ctx, request)
	})
	r = permitRuntime(t, provider, 1)
	root := createTest(t, r)
	runnableLimit(t, r, root.ID, 1)
	submitTest(t, r, root.ID, "root")
	child := permitChild(t, r, root.ID, "child")
	runnableLimit(t, r, child.Session.ID, 1)
	leaf := permitChild(t, r, child.Session.ID, "leaf")
	children[root.ID], children[child.Session.ID] = child.Admission.Input.ID, leaf.Admission.Input.ID
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"root", "child", "leaf"} {
		if result := waitTest(t, r, key, terminal); result.Turn.State != session.Succeeded {
			t.Fatalf("nested %s did not complete: %+v runtime=%v", key, result, r.Err())
		}
	}
	capacityState(t, r, 0, 0, 0, 0)
}
