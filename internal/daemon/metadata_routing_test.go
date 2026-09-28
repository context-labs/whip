package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/legacy/session"
	"github.com/context-labs/whip/internal/llm"
)

func TestColdMetadataReroutesAfterRootOpens(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &titleRunner{
		fakeRunner: &fakeRunner{}, title: "Generated title",
		started: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{}),
	}
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	changes := observeTitleChanges(t, owner)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	// Capture the command after the server chooses its cold route, but before
	// daemon control executes it. Open/submit then wins the registry race.
	waiting, captured, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	blocked := make(chan error, 1)
	go func() {
		blocked <- owner.control.route(ctx, func(actorCtx context.Context) error {
			close(waiting)
			select {
			case request := <-owner.control.requests:
				close(captured)
				select {
				case <-release:
				case <-ctx.Done():
				}
				request.done <- request.work(actorCtx)
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	server := &Server{daemon: owner, ctx: ctx}
	connection := &serverConn{client: InitializeParams{ClientID: "metadata-routing"}}
	params := CommandParams{
		Scope: "root", RootID: rootID, CommandID: "same-value-rename",
		Operation: "session.rename", Payload: json.RawMessage(`{"title":"First question about naming"}`),
	}
	commandDone := make(chan error, 1)
	go func() {
		_, err := server.command(connection, params)
		commandDone <- err
	}()
	select {
	case <-captured:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := root.Submit(ctx, "First question about naming")
	if err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, receipt)
	select {
	case <-runner.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	unblock()
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("metadata command blocked the daemon actor")
	}
	if err := <-blocked; err != nil {
		t.Fatal(err)
	}
	if err := root.routeControl(ctx, func(context.Context) error {
		if root.titleWork != nil {
			return errors.New("same-value rename did not invalidate automatic title")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.finished:
	case <-ctx.Done():
		t.Fatal("manual rename did not cancel title request")
	}
	record, err := store.LoadCommand(ctx, connection.client.ClientID, params.CommandID)
	if err != nil || record.Scope != session.CommandScopeRoot || record.Status != "succeeded" {
		t.Fatalf("rerouted admission = %+v, %v", record, err)
	}
	meta, err := store.SessionMetadata(ctx, rootID)
	if err != nil || meta.Title != "First question about naming" {
		t.Fatalf("manual title = %+v, %v", meta, err)
	}
	assertTitleChanges(t, changes, rootID, rootID) // Initial title and rerouted rename, no stale generation.
}

func TestOpenReloadsMetadataAfterRegistryPublication(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	constructed := make(chan session.Meta, 1)
	owner, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		constructed <- meta
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	// This is Open's initial ID-resolution read, before it publishes the entry.
	stale, err := store.LoadMeta(rootID)
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"title":"Cold rename"}`)
	_, err = owner.control.SessionMetadataCommand(t.Context(), session.CommandAdmission{
		ClientID: "metadata-routing", CommandID: "cold-rename", RequestDigest: "cold-rename",
		Payload: session.RuntimePayload{Data: payload},
	}, "session.rename", payload, rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.openResolved(stale.ID); err != nil {
		t.Fatal(err)
	}
	if meta := <-constructed; meta.Title != "Cold rename" {
		t.Fatalf("constructed stale metadata: factory=%q", meta.Title)
	}
}
