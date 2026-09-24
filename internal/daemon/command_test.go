package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestProtocolCommandRetryAttachesToOneRootExecution(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &fakeRunner{}
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	command := session.CommandAdmission{
		ClientID: "client", CommandID: "command", Kind: "submit", RequestDigest: "digest",
		Payload: session.RuntimePayload{Data: []byte("prompt")},
	}
	first, receipt, err := root.AdmitCommand(context.Background(), command)
	if err != nil || !first.New {
		t.Fatalf("first admission = %+v, %v", first, err)
	}
	if got := waitReceipt(t, receipt); got.Err != nil || got.Output != "prompt" {
		t.Fatalf("first completion = %+v", got)
	}
	select {
	case <-receipt.Done():
	default:
		t.Fatal("completed receipt did not close Done")
	}
	retry, retryReceipt, err := root.AdmitCommand(context.Background(), command)
	if err != nil || retry.New || retry.Command.IngressSeq != first.Command.IngressSeq {
		t.Fatalf("retry admission = %+v, %v", retry, err)
	}
	if got := waitReceipt(t, retryReceipt); got.Err != nil || got.Output != "prompt" {
		t.Fatalf("retry completion = %+v", got)
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls.Load())
	}
	if root.ID() != rootID {
		t.Fatalf("root ID = %q", root.ID())
	}
	if meta, history, err := root.History(); err != nil || meta.ID != rootID || len(history) != 2 {
		t.Fatalf("root history = %+v, %d, %v", meta, len(history), err)
	}
	conflict := command
	conflict.RequestDigest = "different"
	if _, _, err := root.AdmitCommand(context.Background(), conflict); !errors.Is(err, session.ErrCommandConflict) {
		t.Fatalf("conflicting retry error = %v", err)
	}
}
