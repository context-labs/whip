package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestSwarmMutationsRouteThroughRootActor(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	daemon, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := root.AdmitChild(ctx, root.authority.AgentID, "tester", "exec-tester"); err != nil {
		t.Fatal(err)
	}
	if err := root.AdmitChild(ctx, root.authority.AgentID, "implementer", "exec-implementer"); err != nil {
		t.Fatal(err)
	}
	sequence, err := root.SendAgentMessage(ctx, "tester", "implementer", session.AgentMessage{
		Delivery: session.DeliveryQueued, Body: "actor routed",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sequence.InboxSeq != 1 {
		t.Fatalf("message sequence = %+v", sequence)
	}
	relatives, err := root.ListAgentRelatives(ctx, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if relatives.Parent == nil || relatives.Parent.ID != root.authority.AgentID || len(relatives.Siblings) != 1 || relatives.Siblings[0].ID != "implementer" {
		t.Fatalf("actor relatives = %+v", relatives)
	}
	items, err := store.LoadQueuedInbox(ctx, rootID, "implementer", 0, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("actor message inbox=%+v err=%v", items, err)
	}
	var envelope session.AgentMessageEnvelope
	if err := json.Unmarshal(items[0].Payload.Inline, &envelope); err != nil || envelope.SenderAgentID != "tester" {
		t.Fatalf("actor message envelope=%+v err=%v", envelope, err)
	}
	if err := root.TerminalizeSubtree(ctx, root.authority.AgentID, "tester", "deleted"); err != nil {
		t.Fatal(err)
	}
	relatives, err = root.ListAgentRelatives(ctx, "tester")
	if err != nil || relatives.Parent == nil || relatives.Parent.ID != root.authority.AgentID {
		t.Fatalf("deleted lineage=%+v err=%v", relatives, err)
	}
}

func TestSwarmControlQueuedAfterFailureIsRejected(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	t.Cleanup(func() { _ = store.Close() })
	rootID := createRoot(t, store)
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	root := newSession(store, meta, authority, Components{Runner: &fakeRunner{}})
	t.Cleanup(root.supervisor.stop)
	failure := errors.New("worker failed")
	reply := make(chan error, 1)
	ran := false
	root.supervisor.post(workerEnvelope{kind: "failed worker", err: failure})
	root.supervisor.post(workerEnvelope{kind: workerControl, reply: reply, control: func(context.Context) error {
		ran = true
		return nil
	}})
	if err := root.actor(); !errors.Is(err, failure) {
		t.Fatalf("actor error = %v", err)
	}
	if err := <-reply; !errors.Is(err, ErrStopped) || ran {
		t.Fatalf("control error=%v ran=%v", err, ran)
	}

	root = newSession(store, meta, authority, Components{Runner: &fakeRunner{}})
	t.Cleanup(root.supervisor.stop)
	taskReply := make(chan error, 1)
	reply = make(chan error, 1)
	ran = false
	root.supervisor.post(workerEnvelope{kind: workerTaskRecord, reply: taskReply})
	root.supervisor.post(workerEnvelope{kind: workerControl, reply: reply, control: func(context.Context) error {
		ran = true
		return nil
	}})
	if err := root.actor(); err == nil {
		t.Fatal("invalid task record should fail the actor")
	}
	if err := <-taskReply; err == nil {
		t.Fatal("invalid task record should return its error")
	}
	if err := <-reply; !errors.Is(err, ErrStopped) || ran {
		t.Fatalf("control after task failure error=%v ran=%v", err, ran)
	}
}
