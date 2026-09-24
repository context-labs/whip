package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/session"
)

func receiveActorValue[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for actor result")
		var zero T
		return zero
	}
}

func newIdleActorSession(t *testing.T, runner Runner) *Session {
	t.Helper()
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureAuthority(t.Context(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	root := newSession(store, meta, authority, Components{Runner: runner})
	t.Cleanup(root.supervisor.stop)
	return root
}

func TestRouteControlSkipsCancelledQueuedRequest(t *testing.T) {
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "claim"}[owned], func(t *testing.T) {
			root := &Session{supervisor: newSupervisor()}
			t.Cleanup(root.supervisor.stop)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			finished := make(chan error, 1)
			ran := false
			go func() {
				if owned {
					_, err := routeControlOwnedValue(root, ctx, func(context.Context) (int, error) { ran = true; return 1, nil })
					finished <- err
					return
				}
				finished <- root.routeControl(ctx, func(context.Context) error { ran = true; return nil })
			}()
			receiveActorValue(t, root.supervisor.wake)
			cancel()
			if err := receiveActorValue(t, finished); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancelled queued control = %v", err)
			}
			if err := root.processWorkerBatch(root.supervisor.take()); err != nil {
				t.Fatal(err)
			}
			if ran {
				t.Fatal("cancelled control executed after its caller returned")
			}
		})
	}
}

func TestRouteControlValueOutlivesCancelledWaiter(t *testing.T) {
	for _, stopActor := range []bool{false, true} {
		t.Run(map[bool]string{false: "caller cancellation", true: "actor shutdown"}[stopActor], func(t *testing.T) {
			root := &Session{supervisor: newSupervisor()}
			t.Cleanup(root.supervisor.stop)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started, release := make(chan struct{}), make(chan struct{})
			finished := make(chan error, 1)
			go func() {
				_, err := routeControlValue(root, ctx, func(actorCtx context.Context) ([]string, error) {
					close(started)
					<-release // Simulate an operation that finishes after cancellation.
					return []string{"committed"}, nil
				})
				finished <- err
			}()
			receiveActorValue(t, root.supervisor.wake)
			processed := make(chan error, 1)
			go func() { processed <- root.processWorkerBatch(root.supervisor.take()) }()
			receiveActorValue(t, started)
			want := context.Canceled
			if stopActor {
				want = ErrStopped
				root.supervisor.stop()
			} else {
				cancel()
			}
			if err := receiveActorValue(t, finished); !errors.Is(err, want) {
				t.Fatalf("cancelled control = %v, want %v", err, want)
			}
			close(release)
			if err := receiveActorValue(t, processed); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRouteControlOwnedValuePreservesCompletedClaimAfterCancellation(t *testing.T) {
	root := &Session{supervisor: newSupervisor()}
	t.Cleanup(root.supervisor.stop)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, release := make(chan struct{}), make(chan struct{})
	type result struct {
		claim string
		err   error
	}
	finished := make(chan result, 1)
	go func() {
		claim, err := routeControlOwnedValue(root, ctx, func(context.Context) (string, error) {
			close(started)
			<-release
			return "durable claim", nil
		})
		finished <- result{claim, err}
	}()
	receiveActorValue(t, root.supervisor.wake)
	processed := make(chan error, 1)
	go func() { processed <- root.processWorkerBatch(root.supervisor.take()) }()
	receiveActorValue(t, started)
	cancel()
	close(release)
	if got := receiveActorValue(t, finished); got.err != nil || got.claim != "durable claim" {
		t.Fatalf("lost completed claim after cancellation: %+v", got)
	}
	if err := receiveActorValue(t, processed); err != nil {
		t.Fatal(err)
	}
}

type actorCloseFunc func()

func (closeFn actorCloseFunc) Close() { closeFn() }

func TestActorFailureClosesRuntimeWithWorkerAwaitingControl(t *testing.T) {
	root := newIdleActorSession(t, &fakeRunner{})
	root.supervisor.post(workerEnvelope{kind: workerStream, stream: &streamEnvelope{}})
	receiveActorValue(t, root.supervisor.wake)
	workerDone := make(chan struct{})
	workerResult := make(chan error, 1)
	if !root.supervisor.launchWorker("child waiting for root control", func() {
		defer close(workerDone)
		workerResult <- root.routeControl(context.Background(), func(context.Context) error {
			return errors.New("control should never execute")
		})
	}) {
		t.Fatal("worker was not launched")
	}
	receiveActorValue(t, root.supervisor.wake)
	root.runtime = actorCloseFunc(func() { <-workerDone })
	// The events are already queued; supply the wake consumed by the test.
	root.supervisor.wake <- struct{}{}
	go root.run()
	receiveActorValue(t, root.done)
	if err := receiveActorValue(t, workerResult); !errors.Is(err, ErrStopped) {
		t.Fatalf("worker's control result = %v", err)
	}
	if root.Err() == nil {
		t.Fatal("actor failure was not retained")
	}
}

type cancelledClientShellRunner struct {
	*fakeRunner
	started, release chan struct{}
}

func (r *cancelledClientShellRunner) RunShell(context.Context, string) (string, error) {
	close(r.started)
	<-r.release
	return "shell completed", nil
}

func TestClientCommandCancellationDoesNotBlockCompletion(t *testing.T) {
	runner := &cancelledClientShellRunner{fakeRunner: &fakeRunner{}, started: make(chan struct{}), release: make(chan struct{})}
	root := newIdleActorSession(t, runner)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		_, err := root.ClientCommand(ctx, session.CommandAdmission{ClientID: "client", CommandID: "shell", RequestDigest: "shell-request"}, "shell.run", json.RawMessage(`{"command":"echo test"}`))
		returned <- err
	}()
	receiveActorValue(t, root.supervisor.wake)
	if err := root.processWorkerBatch(root.supervisor.take()); err != nil {
		t.Fatal(err)
	}
	// Admission and execution are deliberately separate actor transitions.
	receiveActorValue(t, root.supervisor.wake)
	if err := root.processWorkerBatch(root.supervisor.take()); err != nil {
		t.Fatal(err)
	}
	receiveActorValue(t, runner.started)
	cancel()
	if err := receiveActorValue(t, returned); !errors.Is(err, context.Canceled) {
		t.Fatalf("client wait = %v", err)
	}
	close(runner.release)
	receiveActorValue(t, root.supervisor.wake)
	if err := root.processWorkerBatch(root.supervisor.take()); err != nil {
		t.Fatal(err)
	}
	root.supervisor.wait()
	command, err := root.store.LoadCommand(t.Context(), "client", "shell")
	if err != nil || command.Status != "succeeded" {
		t.Fatalf("completed command after caller cancellation: %+v, %v", command, err)
	}
}

func TestRouteControlOwnedValuePanicDoesNotReturnSuccess(t *testing.T) {
	root := &Session{supervisor: newSupervisor()}
	t.Cleanup(root.supervisor.stop)
	finished := make(chan error, 1)
	go func() {
		_, err := routeControlOwnedValue(root, t.Context(), func(context.Context) (string, error) { panic("claim failed") })
		finished <- err
	}()
	receiveActorValue(t, root.supervisor.wake)
	if err := root.processWorkerBatch(root.supervisor.take()); err == nil {
		t.Fatal("panic did not fail the actor batch")
	}
	if err := receiveActorValue(t, finished); err == nil {
		t.Fatal("panicking claim returned success")
	}
}

func TestClientCommandCancelledBeforeAdmissionHasNoEffect(t *testing.T) {
	root := newIdleActorSession(t, &fakeRunner{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	returned := make(chan error, 1)
	go func() {
		_, err := root.ClientCommand(ctx, session.CommandAdmission{ClientID: "client", CommandID: "rename", RequestDigest: "rename-request"}, "session.rename", json.RawMessage(`{"title":"should not apply"}`))
		returned <- err
	}()
	receiveActorValue(t, root.supervisor.wake)
	cancel()
	if err := receiveActorValue(t, returned); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission = %v", err)
	}
	if err := root.processWorkerBatch(root.supervisor.take()); err != nil {
		t.Fatal(err)
	}
	if _, err := root.store.LoadCommand(t.Context(), "client", "rename"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("cancelled request was durably admitted: %v", err)
	}
}

func TestClientCommandFinishesAfterActionCancellation(t *testing.T) {
	root := newIdleActorSession(t, &fakeRunner{})
	admission := session.CommandAdmission{
		ClientID: "client", CommandID: "cancelled", RequestDigest: "cancelled-request", Scope: session.CommandScopeRoot,
		RootID: root.ID(), AgentID: root.AgentID(), Kind: "shell.run",
	}
	if _, err := root.store.AdmitControlCommand(t.Context(), admission); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var result CommandResult
	if err := root.finishClientCommandInline(ctx, admission, "shell.run", "", context.Canceled, &result); err != nil {
		t.Fatal(err)
	}
	command, err := root.store.LoadCommand(t.Context(), "client", "cancelled")
	if err != nil || command.Status != "failed" {
		t.Fatalf("cancelled action left command uncommitted: %+v, %v", command, err)
	}
}

func TestStateBudgetSettlementSurvivesCancellation(t *testing.T) {
	for _, failAction := range []bool{false, true} {
		t.Run(map[bool]string{false: "committed mutation", true: "failed mutation"}[failAction], func(t *testing.T) {
			root := newIdleActorSession(t, &fakeRunner{})
			readBudget := func() session.BudgetState {
				t.Helper()
				states, err := root.store.InspectBudgetsFor(t.Context(), root.ID(), root.AgentID(), root.AgentID())
				if err != nil {
					t.Fatal(err)
				}
				for _, state := range states {
					if state.Kind == session.BudgetRecordCount {
						return state
					}
				}
				t.Fatal("record budget is missing")
				return session.BudgetState{}
			}
			before := readBudget()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			actionErr := errors.New("mutation failed")
			err := root.consumeBudgets(ctx, root.AgentID(), []capability.Reservation{{Kind: string(session.BudgetRecordCount), Amount: 1, Consume: true}}, func() error {
				cancel()
				if failAction {
					return actionErr
				}
				return nil
			})
			if failAction && !errors.Is(err, actionErr) || !failAction && err != nil {
				t.Fatalf("budget settlement error = %v", err)
			}
			after := readBudget()
			wantUsed := before.Used
			if !failAction {
				wantUsed++
			}
			if after.Reserved != before.Reserved || after.Used != wantUsed {
				t.Fatalf("cancelled action accounting: before=%+v, after=%+v", before, after)
			}
		})
	}
}

func TestStateMutationOwnsPayloadAfterCallerCancellation(t *testing.T) {
	root := newIdleActorSession(t, &fakeRunner{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	payload := session.RuntimePayload{Data: []byte("original")}
	started, release := make(chan struct{}), make(chan struct{})
	observed := make(chan string, 1)
	returned := make(chan error, 1)
	go func() {
		_, err := root.mutateState(ctx, root.AgentID(), payload, func(_ context.Context, owned session.RuntimePayload) (session.StateValue, error) {
			close(started)
			<-release
			observed <- string(owned.Data)
			return session.StateValue{}, nil
		})
		returned <- err
	}()
	receiveActorValue(t, root.supervisor.wake)
	processed := make(chan error, 1)
	go func() { processed <- root.processWorkerBatch(root.supervisor.take()) }()
	receiveActorValue(t, started)
	cancel()
	if err := receiveActorValue(t, returned); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mutation = %v", err)
	}
	copy(payload.Data, "modified")
	close(release)
	if got := receiveActorValue(t, observed); got != "original" {
		t.Fatalf("actor observed caller's reused buffer: %q", got)
	}
	if err := receiveActorValue(t, processed); err != nil {
		t.Fatal(err)
	}
}
