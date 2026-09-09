package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func TestClientArchiveIsDurableIdempotentAndDoesNotStopWork(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	started, release := make(chan struct{}), make(chan struct{})
	runner := &fakeRunner{turn: func(ctx context.Context, input string, _ bool) (string, error) {
		close(started)
		select {
		case <-release:
			return input, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := root.Submit(t.Context(), "work continues")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not start")
	}
	for _, archived := range []bool{true, false} {
		id := "archive"
		if !archived {
			id = "restore"
		}
		result := clientCommand(t, root, "client", id, "session.archive", protocol.ArchiveParams{Archived: archived})
		var outcome protocol.ArchiveResult
		if err := json.Unmarshal(result.Result, &outcome); err != nil || result.Status != "succeeded" || outcome.Archived != archived {
			t.Fatalf("archive command %+v, %v", result, err)
		}
		metadata, err := store.SessionMetadata(t.Context(), rootID)
		if err != nil || metadata.Archived != archived {
			t.Fatalf("archive did not persist: %+v %v", metadata, err)
		}
		revision, err := store.SessionCatalogRevision(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		retry := clientCommand(t, root, "client", id, "session.archive", protocol.ArchiveParams{Archived: archived})
		if !reflect.DeepEqual(result, retry) {
			t.Fatalf("archive retry changed result: %+v %+v", result, retry)
		}
		after, err := store.SessionCatalogRevision(t.Context())
		if err != nil || after != revision {
			t.Fatalf("archive retry mutated catalog: %+v %+v %v", revision, after, err)
		}
		turnID, err := store.ActiveTurn(t.Context(), rootID, rootID)
		if err != nil || turnID == "" {
			t.Fatalf("archive interrupted turn: %q %v", turnID, err)
		}
	}
	events, _, err := store.ReplayEvents(t.Context(), rootID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	states := []bool{}
	for _, event := range events {
		if event.Kind == "session.archived.updated" {
			var update protocol.SessionUpdateEvent
			if err := json.Unmarshal(event.Payload.Inline, &update); err != nil || update.Archived == nil {
				t.Fatalf("archive event missing boolean %+v %v", event, err)
			}
			states = append(states, *update.Archived)
		}
	}
	if !reflect.DeepEqual(states, []bool{true, false}) {
		t.Fatalf("archive events = %v", states)
	}
	close(release)
	if completion := waitReceipt(t, receipt); completion.Err != nil || completion.Output != "work continues" {
		t.Fatalf("archived work did not complete: %+v", completion)
	}
}

func TestClientForkDefaultsToRecognizableTitle(t *testing.T) {
	f := newV2Fixture(t, &fakeRunner{})
	if err := f.store.SetTitle(f.rootID, "Original"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetArchived(t.Context(), f.rootID, true); err != nil {
		t.Fatal(err)
	}
	client := f.dial("unix", "fork-default-title")
	result, err := client.Command(t.Context(), CommandParams{
		Scope: "root", CommandID: "fork-default", RootID: f.rootID, Operation: "session.fork",
		Payload: json.RawMessage(`{"expected_revision":"0"}`),
	})
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("fork %+v %v", result, err)
	}
	var fork protocol.RootIDResult
	if err := json.Unmarshal(result.Result, &fork); err != nil {
		t.Fatal(err)
	}
	metadata, err := f.store.SessionMetadata(t.Context(), fork.RootID)
	if err != nil || metadata.Title != "Original (fork #1)" || metadata.Archived {
		t.Fatalf("default fork metadata %+v %v", metadata, err)
	}
}
