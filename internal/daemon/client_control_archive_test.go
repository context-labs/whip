package daemon

import (
	"context"
	"encoding/json"
	"os"
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

func TestControlSessionMetadataCommandWhenRootCannotOpen(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	missing := filepath.Join(t.TempDir(), "gone")
	if err := store.SetWorkingDirectory(rootID, missing); err != nil {
		t.Fatal(err)
	}
	owner, err := New(store, func(_ context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		// Production runner construction stats the workspace; mirror that here.
		if _, err := os.Stat(meta.CWD); err != nil {
			return Components{}, err
		}
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if _, err := owner.Open(rootID); err == nil {
		t.Fatal("open should fail for a missing workspace")
	}
	for _, tc := range []struct {
		id, operation, payload string
	}{
		{"archive-cold", "session.archive", `{"archived":true}`},
		{"rename-cold", "session.rename", `{"title":"Cold rename"}`},
	} {
		raw := json.RawMessage(tc.payload)
		digest, err := requestDigest("root", rootID, tc.operation, raw)
		if err != nil {
			t.Fatal(err)
		}
		record, err := owner.control.SessionMetadataCommand(t.Context(), session.CommandAdmission{
			ClientID: "client", CommandID: tc.id, RequestDigest: digest,
			Payload: session.RuntimePayload{Data: raw, MediaType: "application/json", Source: tc.operation},
		}, tc.operation, raw, rootID)
		if err != nil || record.Status != "succeeded" {
			t.Fatalf("%s cold root %+v %v", tc.operation, record, err)
		}
		// Idempotent retry replays the durable record without re-executing.
		retry, err := owner.control.SessionMetadataCommand(t.Context(), session.CommandAdmission{
			ClientID: "client", CommandID: tc.id, RequestDigest: digest,
			Payload: session.RuntimePayload{Data: raw, MediaType: "application/json", Source: tc.operation},
		}, tc.operation, raw, rootID)
		if err != nil || retry.Status != "succeeded" || retry.CommandID != tc.id || retry.IngressSeq != record.IngressSeq {
			t.Fatalf("%s retry changed record: %+v %+v %v", tc.operation, record, retry, err)
		}
	}
	meta, err := store.SessionMetadata(t.Context(), rootID)
	if err != nil || !meta.Archived || meta.Title != "Cold rename" {
		t.Fatalf("metadata not persisted: %+v %v", meta, err)
	}
	events, _, err := store.ReplayEvents(t.Context(), rootID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, event := range events {
		if event.Kind == "session.archived.updated" || event.Kind == "session.title.updated" {
			kinds = append(kinds, event.Kind)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"session.archived.updated", "session.title.updated"}) {
		t.Fatalf("metadata events = %v", kinds)
	}
}

func TestArchiveAndRenameColdRootWithoutWorkspace(t *testing.T) {
	f := newV2Fixture(t, &fakeRunner{})
	// A worktree deleted after the session last ran: opening must not gate
	// catalog mutations.
	missing := filepath.Join(t.TempDir(), "gone")
	if err := f.store.SetWorkingDirectory(f.rootID, missing); err != nil {
		t.Fatal(err)
	}
	client := f.dial("unix", "archive-cold")
	archive, err := client.Command(t.Context(), CommandParams{
		Scope: "root", CommandID: "archive-missing", RootID: f.rootID, Operation: "session.archive",
		Payload: json.RawMessage(`{"archived":true}`),
	})
	if err != nil || archive.Status != "succeeded" {
		t.Fatalf("archive cold root %+v %v", archive, err)
	}
	meta, err := f.store.SessionMetadata(t.Context(), f.rootID)
	if err != nil || !meta.Archived {
		t.Fatalf("archive not persisted: %+v %v", meta, err)
	}
	rename, err := client.Command(t.Context(), CommandParams{
		Scope: "root", CommandID: "rename-missing", RootID: f.rootID, Operation: "session.rename",
		Payload: json.RawMessage(`{"title":"Renamed without workspace"}`),
	})
	if err != nil || rename.Status != "succeeded" {
		t.Fatalf("rename cold root %+v %v", rename, err)
	}
	meta, err = f.store.SessionMetadata(t.Context(), f.rootID)
	if err != nil || meta.Title != "Renamed without workspace" {
		t.Fatalf("rename not persisted: %+v %v", meta, err)
	}
	// Idempotent retry replays the durable record.
	retry, err := client.Command(t.Context(), CommandParams{
		Scope: "root", CommandID: "archive-missing", RootID: f.rootID, Operation: "session.archive",
		Payload: json.RawMessage(`{"archived":true}`),
	})
	if err != nil || retry.Status != "succeeded" {
		t.Fatalf("archive retry %+v %v", retry, err)
	}
	events, _, err := f.store.ReplayEvents(t.Context(), f.rootID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	kinds := []string{}
	for _, event := range events {
		if event.Kind == "session.archived.updated" || event.Kind == "session.title.updated" {
			kinds = append(kinds, event.Kind)
		}
	}
	if !reflect.DeepEqual(kinds, []string{"session.archived.updated", "session.title.updated"}) {
		t.Fatalf("metadata events = %v", kinds)
	}
	// Restore the workspace, then a live root still routes through the actor.
	if err := f.store.SetWorkingDirectory(f.rootID, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	root, err := f.server.daemon.Open(f.rootID)
	if err != nil {
		t.Fatal(err)
	}
	live := clientCommand(t, root, "client", "archive-live", "session.archive", protocol.ArchiveParams{Archived: false})
	if live.Status != "succeeded" {
		t.Fatalf("archive live root %+v", live)
	}
	meta, err = f.store.SessionMetadata(t.Context(), f.rootID)
	if err != nil || meta.Archived {
		t.Fatalf("live archive not persisted: %+v %v", meta, err)
	}
}

func TestClientForkDefaultsToRecognizableTitle(t *testing.T) {
	f := newV2Fixture(t, &fakeRunner{})
	// Open before the persisted title arrives, as with an asynchronous title
	// update. The fork must use store metadata, not the actor's cached name.
	if _, err := f.server.daemon.Open(f.rootID); err != nil {
		t.Fatal(err)
	}
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
	for _, test := range []struct {
		name, sourceTitle, customTitle, want string
	}{
		{"next fork", "Original", "", "Original (fork #2)"},
		{"updated source", "Session naming cleanup", "", "Session naming cleanup (fork #1)"},
		{"custom name", "Session naming cleanup", "My experiment", "My experiment"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := f.store.SetTitle(f.rootID, test.sourceTitle); err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(map[string]string{"title": test.customTitle, "expected_revision": "0"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.Command(t.Context(), CommandParams{
				Scope: "root", CommandID: test.name, RootID: f.rootID, Operation: "session.fork", Payload: payload,
			})
			if err != nil || result.Status != "succeeded" {
				t.Fatalf("fork %+v %v", result, err)
			}
			var fork protocol.RootIDResult
			if err := json.Unmarshal(result.Result, &fork); err != nil {
				t.Fatal(err)
			}
			metadata, err := f.store.SessionMetadata(t.Context(), fork.RootID)
			if err != nil || metadata.Title != test.want || metadata.Archived {
				t.Fatalf("fork metadata %+v %v; want %q", metadata, err, test.want)
			}
		})
	}
}
