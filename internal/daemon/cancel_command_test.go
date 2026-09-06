package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestExactCommandCancellationBeforeAndAfterTurnStart(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "runtime.db"))
	rootID := createRoot(t, store)
	started := make(chan struct{}, 3)
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{turn: func(ctx context.Context, _ string, _ bool) (string, error) {
			started <- struct{}{}
			<-ctx.Done()
			return "", ctx.Err()
		}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	client := runtimeUnixClient(t, value)
	cancelTarget := func(id, target string) CommandResult {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{"target_command_id": target})
		result, err := client.Command(t.Context(), CommandParams{Scope: "root", RootID: rootID, CommandID: id, Operation: "cancel", Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	admit := func(id string) {
		t.Helper()
		_, err := root.AcceptCommand(t.Context(), session.CommandAdmission{ClientID: "fixture-client", CommandID: id, Kind: "submit", Operation: "submit", RequestDigest: id, Payload: session.RuntimePayload{Data: []byte(id)}})
		if err != nil {
			t.Fatal(err)
		}
	}
	admit("first")
	<-started
	admit("queued")
	cancelled := cancelTarget("cancel-queued", "queued")
	if cancelled.Error != "" {
		t.Fatal(cancelled.Error)
	}
	queued, err := store.LoadCommand(t.Context(), "fixture-client", "queued")
	if err != nil || queued.Status != "cancelled" {
		t.Fatalf("queued=%+v error=%v", queued, err)
	}
	first, err := store.LoadCommand(t.Context(), "fixture-client", "first")
	if err != nil || first.Status != "running" {
		t.Fatalf("unrelated running input=%+v error=%v", first, err)
	}
	wrongNamespace := clientCommand(t, root, "other", "cancel-other", "cancel", map[string]any{"target_command_id": "first"})
	if wrongNamespace.Error == "" {
		t.Fatal("another namespace cancelled input")
	}
	cancelled = cancelTarget("cancel-first", "first")
	if cancelled.Error != "" {
		t.Fatal(cancelled.Error)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for {
		first, err = store.LoadCommand(ctx, "fixture-client", "first")
		if err != nil {
			t.Fatal(err)
		}
		if first.Status == "cancelled" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("running target did not cancel")
		case <-time.After(time.Millisecond):
		}
	}
	admit("newer")
	<-started
	stale := cancelTarget("cancel-stale", "first")
	if stale.Error == "" {
		t.Fatal("terminal target accepted cancellation of newer work")
	}
	newer, err := store.LoadCommand(t.Context(), "fixture-client", "newer")
	if err != nil || newer.Status != "running" {
		t.Fatalf("newer=%+v error=%v", newer, err)
	}
}
