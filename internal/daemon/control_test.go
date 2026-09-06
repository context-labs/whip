package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/context-labs/whip/internal/protocol"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestControlSessionCreationIsIdempotent(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	admission := session.CommandAdmission{
		ClientID: "client", CommandID: "create", RequestDigest: "digest",
		Payload: session.RuntimePayload{Data: []byte(`{"cwd":"/tmp","model":"m","provider":"p"}`)},
	}
	create := CreateSession{Kind: session.SessionKindAgent, CWD: "/tmp", Model: "m", Provider: "p"}
	const retries = 12
	records := make(chan session.CommandRecord, retries)
	errs := make(chan error, retries)
	var wait sync.WaitGroup
	for range retries {
		wait.Go(func() {
			record, err := value.control.CreateSession(context.Background(), admission, create)
			records <- record
			errs <- err
		})
	}
	wait.Wait()
	close(records)
	close(errs)
	var rootID string
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for record := range records {
		if record.Status != "succeeded" || record.IngressSeq != 1 {
			t.Fatalf("record = %+v", record)
		}
		var result protocol.RootIDResult
		if err := json.Unmarshal(record.Outcome.Inline, &result); err != nil {
			t.Fatal(err)
		}
		if rootID == "" {
			rootID = result.RootID
		} else if result.RootID != rootID {
			t.Fatalf("retry root=%q want %q", result.RootID, rootID)
		}
	}
	if _, _, err := store.Load(rootID); err != nil {
		t.Fatalf("load created root: %v", err)
	}
	conflict := admission
	conflict.RequestDigest = "other"
	if _, err := value.control.CreateSession(context.Background(), conflict, create); !errors.Is(err, session.ErrCommandConflict) {
		t.Fatalf("conflicting retry = %v", err)
	}
}

func TestControlRouteHonorsCallerAndDaemonCancellation(t *testing.T) {
	caller, cancelCaller := context.WithCancel(context.Background())
	cancelCaller()
	idle := &Control{ctx: context.Background(), requests: make(chan controlRequest), done: make(chan struct{})}
	if err := idle.route(caller, func(context.Context) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("caller cancellation = %v", err)
	}
	daemonContext, cancelDaemon := context.WithCancel(context.Background())
	cancelDaemon()
	closed := &Control{ctx: daemonContext, requests: make(chan controlRequest), done: make(chan struct{})}
	if err := closed.route(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrClosed) {
		t.Fatalf("daemon cancellation = %v", err)
	}
}

func TestControlListsDeletesAndCheckpointsWithDurableOutcomes(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	rootID := createRoot(t, store)
	if err := store.Save(rootID, 1, []llm.Message{{Role: "user", Content: "listed"}, {Role: "assistant", Content: "yes"}}, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	admission := func(id string) session.CommandAdmission {
		return session.CommandAdmission{ClientID: "client", CommandID: id, RequestDigest: id}
	}
	listed, err := value.control.ListSessions(t.Context(), admission("list"), 10)
	if err != nil || listed.Status != "succeeded" || !strings.Contains(string(listed.Outcome.Inline), rootID) {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	if retry, err := value.control.ListSessions(t.Context(), admission("list"), 10); err != nil || retry.Status != "succeeded" {
		t.Fatalf("list retry = %+v, %v", retry, err)
	}

	removeErr := errors.New("session is busy")
	failed, err := value.control.DeleteSession(t.Context(), admission("delete-failed"), rootID, func(context.Context, string) error {
		return removeErr
	})
	var failure protocol.RPCError
	decodeErr := json.Unmarshal(failed.Outcome.Inline, &failure)
	if !errors.Is(err, removeErr) || failed.Status != "failed" || decodeErr != nil || failure.Message != removeErr.Error() {
		t.Fatalf("failed delete = %+v, %v", failed, err)
	}
	if retry, err := value.control.DeleteSession(t.Context(), admission("delete-failed"), rootID, func(context.Context, string) error {
		t.Fatal("terminal delete retry executed removal")
		return nil
	}); err != nil || retry.Status != "failed" {
		t.Fatalf("failed delete retry = %+v, %v", retry, err)
	}
	deleted, err := value.control.DeleteSession(t.Context(), admission("delete"), rootID, func(ctx context.Context, id string) error {
		return store.DeleteSession(ctx, id)
	})
	var deletedRoot protocol.RootIDResult
	decodeErr = json.Unmarshal(deleted.Outcome.Inline, &deletedRoot)
	if err != nil || deleted.Status != "succeeded" || decodeErr != nil || deletedRoot.RootID != rootID {
		t.Fatalf("delete = %+v, %v", deleted, err)
	}
	checkpoint, err := value.control.Checkpoint(t.Context(), admission("checkpoint"), 7)
	if err != nil || checkpoint.Status != "succeeded" || !strings.Contains(string(checkpoint.Outcome.Inline), `"generation":"7"`) {
		t.Fatalf("checkpoint = %+v, %v", checkpoint, err)
	}
}

func TestDeleteAcceptanceDoesNotBlockControlOrRepeatWork(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "runtime.db"))
	ctx, cancel := context.WithCancel(t.Context())
	control := newControl(ctx, store)
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(func() { unblock(); cancel(); <-control.done })
	rootID := createRoot(t, store)
	admission := session.CommandAdmission{ClientID: "test", CommandID: "delete", RequestDigest: "stable", Payload: session.RuntimePayload{Data: []byte(`{}`)}}
	remove := func(ctx context.Context, id string) error {
		close(entered)
		select {
		case <-release:
			return store.DeleteSession(ctx, id)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	record, err := control.AcceptDeleteSession(t.Context(), admission, rootID, remove)
	if err != nil || record.Status != "queued" {
		t.Fatalf("receipt=%+v error=%v", record, err)
	}
	<-entered
	queryCtx, queryCancel := context.WithTimeout(t.Context(), time.Second)
	defer queryCancel()
	if err := control.route(queryCtx, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("deletion blocked actor: %v", err)
	}
	duplicate, err := control.AcceptDeleteSession(queryCtx, admission, rootID, remove)
	if err != nil || duplicate.Status != "running" {
		t.Fatalf("retry=%+v error=%v", duplicate, err)
	}
	unblock()
	for {
		record, err = store.LoadCommand(queryCtx, admission.ClientID, admission.CommandID)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == "succeeded" {
			break
		}
		select {
		case <-queryCtx.Done():
			t.Fatal("deletion did not complete")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestDeleteQueueExhaustionDoesNotAdmitRejectedWork(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "runtime.db"))
	ctx, cancel := context.WithCancel(t.Context())
	control := newControl(ctx, store)
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { cancel(); close(release); <-control.done })
	admission := func(id string) session.CommandAdmission {
		return session.CommandAdmission{ClientID: "test", CommandID: id, RequestDigest: id, Payload: session.RuntimePayload{Data: []byte(`{}`)}}
	}
	if _, err := control.AcceptDeleteSession(t.Context(), admission("active"), "unused", func(context.Context, string) error { close(entered); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	<-entered
	for i := range cap(control.deletes) {
		if _, err := control.AcceptDeleteSession(t.Context(), admission(fmt.Sprint(i)), "unused", func(context.Context, string) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := control.AcceptDeleteSession(t.Context(), admission("rejected"), "unused", func(context.Context, string) error { t.Error("rejected deletion executed"); return nil }); err == nil {
		t.Fatal("queue admitted excess work")
	}
	if _, err := store.LoadCommand(t.Context(), "test", "rejected"); err == nil {
		t.Fatal("rejected command entered durable journal")
	}
	if record, err := control.AcceptDeleteSession(t.Context(), admission("0"), "unused", nil); err != nil || record.Status != "queued" {
		t.Fatalf("full queue prevented matching retry: %+v %v", record, err)
	}
}
