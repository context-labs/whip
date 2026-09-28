package runtime

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func lifecycleTarget(t *testing.T, r *Runtime, kind string) (session.Session, store.Admission) {
	t.Helper()
	root := createTest(t, r)
	if kind == "root" {
		return root, submitTest(t, r, root.ID, "initial")
	}
	child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "initial"}, store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "initial"}}})
	if err != nil {
		t.Fatal(err)
	}
	return *child.Session, child.Admission
}

func TestLifecycleDelayedStopCannotCancelNewExecution(t *testing.T) {
	for _, kind := range []string{"root", "child"} {
		t.Run(kind, func(t *testing.T) {
			entered := make(chan context.Context, 2)
			release := make(chan struct{}, 1)
			provider := providerFunc(func(ctx context.Context, _ model.Request) (model.Response, error) {
				entered <- ctx
				select {
				case <-ctx.Done():
					return model.Response{}, ctx.Err()
				case <-release:
					return model.Response{Parts: []session.Part{{Type: "text", Text: "completed"}}}, nil
				}
			})
			r := openTest(t, t.TempDir(), provider)
			target, _ := lifecycleTarget(t, r, kind)
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("initial provider did not start")
			}
			// Hold only the post-commit notification: old execution can settle and
			// the host can reactivate before this result reaches the runtime helper.
			oldStop, err := r.store.SetLifecycle(t.Context(), target.ID, session.Stopped)
			if err != nil || oldStop.CancelTurnID == nil {
				t.Fatalf("missing old cancellation: %+v %v", oldStop, err)
			}
			if _, err := r.SetLifecycle(t.Context(), target.ID, session.Active); err != nil {
				t.Fatal(err)
			}
			release <- struct{}{}
			if old := waitTest(t, r, "initial", terminal); old.Turn.State != session.Cancelled {
				t.Fatalf("old execution ignored committed stop: %+v", old.Turn)
			}
			idleStop, err := r.store.SetLifecycle(t.Context(), target.ID, session.Stopped)
			if err != nil || idleStop.CancelTurnID != nil {
				t.Fatalf("idle stop captured a turn: %+v %v", idleStop, err)
			}
			if _, err := r.SetLifecycle(t.Context(), target.ID, session.Active); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, target.ID, "new")
			var next context.Context
			select {
			case next = <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("new provider did not start")
			}
			for range 2 {
				r.applyLifecycleChange(oldStop)
				r.applyLifecycleChange(idleStop)
			}
			if err := next.Err(); err != nil {
				t.Fatalf("delayed stop cancelled newer execution: %v", err)
			}
			release <- struct{}{}
			if next := waitTest(t, r, "new", terminal); next.Turn.State != session.Succeeded {
				t.Fatalf("new execution failed: %+v", next.Turn)
			}
		})
	}
}

func TestLifecycleRootAndChildStopResumePreservesQueuedWork(t *testing.T) {
	for _, kind := range []string{"root", "child"} {
		t.Run(kind, func(t *testing.T) {
			entered, cancelled, settle := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var release sync.Once
			var calls atomic.Int32
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				calls.Add(1)
				key := request.Messages[len(request.Messages)-1].Parts[0].Text
				if key == "initial" {
					close(entered)
					<-ctx.Done()
					close(cancelled)
					<-settle
					return model.Response{}, ctx.Err()
				}
				return model.Response{Parts: []session.Part{{Type: "text", Text: "reply " + key}}}, nil
			})
			r := openTest(t, t.TempDir(), provider)
			t.Cleanup(func() { release.Do(func() { close(settle) }) })
			target, original := lifecycleTarget(t, r, kind)
			queued := submitTest(t, r, target.ID, "queued")
			discarded := submitTest(t, r, target.ID, "discarded")
			if input, err := r.CancelInput(t.Context(), discarded.Input.ID); err != nil || input.State != session.InputCancelled {
				t.Fatalf("queued cancellation: %+v %v", input, err)
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			capacitySignal(t, entered)
			requestCtx, cancelRequest := context.WithCancel(t.Context())
			cancelRequest()
			if _, err := r.SetLifecycle(requestCtx, target.ID, session.Stopped); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled lifecycle request: %v", err)
			}
			select {
			case <-cancelled:
				t.Fatal("failed lifecycle transaction cancelled live work")
			default:
			}
			for range 2 {
				if stopped, err := r.SetLifecycle(t.Context(), target.ID, session.Stopped); err != nil || stopped.Lifecycle != session.Stopped {
					t.Fatalf("stop: %+v %v", stopped, err)
				}
			}
			capacitySignal(t, cancelled)
			if _, err := r.Admit(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "stopped"}, store.Submission{SessionID: target.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "rejected"}}}); !errors.Is(err, store.ErrStopped) {
				t.Fatalf("stopped admission: %v", err)
			}
			if _, err := r.store.Claim(t.Context(), target.ID); !errors.Is(err, store.ErrStopped) {
				t.Fatalf("stopped claim: %v", err)
			}
			if err := r.DeleteSubtree(t.Context(), target.ID); !errors.Is(err, store.ErrBusy) {
				t.Fatalf("cancelling deletion: %v", err)
			}
			if _, err := r.SetLifecycle(t.Context(), target.ID, session.Active); err != nil {
				t.Fatal(err)
			}
			if _, err := r.store.Claim(t.Context(), target.ID); !errors.Is(err, store.ErrBusy) {
				t.Fatalf("reactivation overlapped cancelling execution: %v", err)
			}
			pending, err := r.Admission(t.Context(), queued.Receipt.RequestIdentity)
			if err != nil || pending.Input.State != session.Queued || pending.Turn != nil {
				t.Fatalf("stop changed queued work: %+v %v", pending, err)
			}
			release.Do(func() { close(settle) })
			old := waitTest(t, r, "initial", terminal)
			if old.Turn.State != session.Cancelled || old.Input.ID != original.Input.ID {
				t.Fatalf("wrong cancelled outcome: %+v", old)
			}
			if next := waitTest(t, r, "queued", terminal); next.Turn.State != session.Succeeded || calls.Load() != 2 {
				t.Fatalf("queue did not resume exactly once: %+v calls=%d", next.Turn, calls.Load())
			}
			history, err := r.History(t.Context(), target.ID, 0, 100)
			if err != nil || len(history) != 3 || history[0].InputID == nil || *history[0].InputID != original.Input.ID || history[1].InputID == nil || *history[1].InputID != queued.Input.ID || history[2].Parts[0].Text != "reply queued" {
				t.Fatalf("history duplicated or discarded work: %+v %v", history, err)
			}
		})
	}
}

func TestLifecycleRootAndChildFailureRetainsExactInput(t *testing.T) {
	for _, kind := range []string{"root", "child"} {
		t.Run(kind, func(t *testing.T) {
			r := openTest(t, t.TempDir(), providerFunc(func(context.Context, model.Request) (model.Response, error) {
				return model.Response{}, errors.New("controlled provider failure")
			}))
			target, original := lifecycleTarget(t, r, kind)
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			failed := waitTest(t, r, "initial", terminal)
			if failed.Turn.State != session.Failed || failed.Turn.Failure == nil || failed.Input.ID != original.Input.ID || r.Err() != nil {
				t.Fatalf("failure lost canonical outcome: %+v runtime=%v", failed, r.Err())
			}
			history, err := r.History(t.Context(), target.ID, 0, 100)
			if err != nil || len(history) != 1 || history[0].InputID == nil || *history[0].InputID != original.Input.ID {
				t.Fatalf("failed execution invented successful output: %+v %v", history, err)
			}
			current, err := r.Session(t.Context(), target.ID)
			if err != nil || current.Lifecycle != session.Active {
				t.Fatalf("one failed turn changed retained lifecycle: %+v %v", current, err)
			}
		})
	}
}

func TestLifecycleRootAndChildRecoveryAndDeletion(t *testing.T) {
	for _, kind := range []string{"root", "child"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			r := openTest(t, directory, model.Scripted{})
			target, original := lifecycleTarget(t, r, kind)
			submitTest(t, r, target.ID, "queued")
			claimed, err := r.store.Claim(t.Context(), target.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.store.AppendMessage(t.Context(), claimed.Turn.ID, session.MessageDraft{ID: "partial", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "committed before restart"}}}); err != nil {
				t.Fatal(err)
			}
			// No worker is started: closing the owner leaves the durable running
			// turn for the next exclusive owner's recovery, as after a crash.
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			r = openTest(t, directory, providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				calls.Add(1)
				return (model.Scripted{}).Complete(ctx, request)
			}))
			before, err := r.Turn(t.Context(), claimed.Turn.ID)
			if err != nil || before.State != session.Running {
				t.Fatalf("opening replayed or recovered execution: %+v %v", before, err)
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			if old := waitTest(t, r, "initial", terminal); old.Turn.State != session.Interrupted || old.Input.ID != original.Input.ID {
				t.Fatalf("claimed work was replayed: %+v", old)
			}
			if queued := waitTest(t, r, "queued", terminal); queued.Turn.State != session.Succeeded || calls.Load() != 1 {
				t.Fatalf("queued recovery: %+v calls=%d", queued, calls.Load())
			}
			history, err := r.History(t.Context(), target.ID, 0, 100)
			if err != nil || len(history) != 4 || history[1].Parts[0].Text != "committed before restart" {
				t.Fatalf("recovery changed committed history: %+v %v", history, err)
			}
			if err := r.DeleteSubtree(t.Context(), target.ID); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"initial", "queued"} {
				tombstone, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key})
				if err != nil || tombstone.Input != nil || tombstone.Receipt.DeletedAt == nil {
					t.Fatalf("receipt lost deletion evidence: %+v %v", tombstone, err)
				}
			}
			_, err = r.Tree(t.Context(), target.TreeID)
			if kind == "root" && !errors.Is(err, store.ErrNotFound) || kind == "child" && err != nil {
				t.Fatalf("wrong deletion scope for %s: %v", kind, err)
			}
		})
	}
}
