package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestClientAcceptancePrecedesProviderConstructionAndSurvivesDisconnect(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	entered, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var constructed atomic.Int32
	value, err := New(store, func(ctx context.Context, meta session.Meta, _ []llm.Message) (Components, error) {
		constructed.Add(1)
		if meta.Model == "replacement" {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return Components{}, ctx.Err()
			}
		}
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { unblock(); _ = value.Close() }()
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"model":"replacement","provider":"provider"}`)
	admission := session.CommandAdmission{ClientID: "client", CommandID: "accepted-model", RequestDigest: "model-request", Payload: session.RuntimePayload{Data: payload, MediaType: "application/json"}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	accepted, err := root.AcceptClientCommand(ctx, admission, "session.model", payload)
	cancel()
	if err != nil || accepted.Status != "queued" {
		t.Fatalf("acceptance=%+v,error=%v", accepted, err)
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("provider preparation did not start")
	}
	record, err := store.LoadCommand(t.Context(), admission.ClientID, admission.CommandID)
	if err != nil || record.Status != "running" {
		t.Fatalf("durable execution=%+v,error=%v", record, err)
	}
	queryCtx, queryCancel := context.WithTimeout(t.Context(), time.Second)
	defer queryCancel()
	// This root query needs the actor: it must complete while preparation blocks.
	if _, err := root.Snapshot(queryCtx); err != nil {
		t.Fatalf("provider construction blocked actor: %v", err)
	}
	duplicate, err := root.AcceptClientCommand(queryCtx, admission, "session.model", payload)
	if err != nil || duplicate.Status != "running" || constructed.Load() != 2 {
		t.Fatalf("retry=%+v,error=%v,constructions=%d", duplicate, err, constructed.Load())
	}
	unblock()
	deadline := time.Now().Add(time.Second)
	for {
		record, err = store.LoadCommand(t.Context(), admission.ClientID, admission.CommandID)
		if err != nil {
			t.Fatal(err)
		}
		if record.Status == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("disconnected command did not finish: %+v", record)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestReplacementPreparationClosesPartialFactoryResult(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &fakeRunner{}
	runtime := &fakeCloser{}
	root := &Session{store: store}
	_, err := root.prepareReplacement(t.Context(), func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner, Runtime: runtime}, context.Canceled
	}, rootID, "replacement", "provider")
	if err == nil || !runner.closed.Load() || !runtime.closed.Load() {
		t.Fatalf("error=%v runner closed=%t runtime closed=%t", err, runner.closed.Load(), runtime.closed.Load())
	}
}

func TestCommandAcceptanceRetriesDoNotRetainReceipts(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "runtime.db"))
	rootID := createRoot(t, store)
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{turn: func(ctx context.Context, _ string, _ bool) (string, error) { <-ctx.Done(); return "", ctx.Err() }}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = value.Close() }()
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	admission := session.CommandAdmission{ClientID: "client", CommandID: "input", Kind: "input", Operation: "input", RequestDigest: "stable", Payload: session.RuntimePayload{Data: []byte("hello")}}
	for range 100 {
		if _, err := root.AcceptCommand(t.Context(), admission); err != nil {
			t.Fatal(err)
		}
	}
	count, err := routeControlValue(root, t.Context(), func(context.Context) (int, error) {
		count := 0
		for _, receipts := range root.receipts {
			count += len(receipts)
		}
		return count, nil
	})
	if err != nil || count != 0 {
		t.Fatalf("retained receipts=%d error=%v", count, err)
	}
}
