package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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
	create := CreateSession{CWD: "/tmp", Model: "m", Provider: "p"}
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
		if rootID == "" {
			rootID = string(record.Outcome.Inline)
		} else if got := string(record.Outcome.Inline); got != rootID {
			t.Fatalf("retry root = %q, want %q", got, rootID)
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
	if !errors.Is(err, removeErr) || failed.Status != "failed" || string(failed.Outcome.Inline) != removeErr.Error() {
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
	if err != nil || deleted.Status != "succeeded" || string(deleted.Outcome.Inline) != rootID {
		t.Fatalf("delete = %+v, %v", deleted, err)
	}
	checkpoint, err := value.control.Checkpoint(t.Context(), admission("checkpoint"), 7)
	if err != nil || checkpoint.Status != "succeeded" || !strings.Contains(string(checkpoint.Outcome.Inline), `"generation":7`) {
		t.Fatalf("checkpoint = %+v, %v", checkpoint, err)
	}
}
