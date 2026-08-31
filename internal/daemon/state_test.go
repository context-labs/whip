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

func TestBlackboardSubscriptionRoutesThroughActorAndSurvivesRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createRoot(t, store)
	factory := func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	}
	daemon, err := New(store, factory)
	if err != nil {
		t.Fatal(err)
	}
	root, err := daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := root.AdmitChild(ctx, root.authority.AgentID, "subscriber", "exec-subscriber"); err != nil {
		t.Fatal(err)
	}
	subscription, err := root.CreateBlackboardSubscription(ctx, "subscriber", "build")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := root.ListBlackboardSubscriptions(ctx, "subscriber")
	if err != nil || len(listed) != 1 || listed[0].ID != subscription.ID || listed[0].Cursor != 0 {
		t.Fatalf("subscriptions=%+v err=%v", listed, err)
	}
	if err := root.CancelBlackboardSubscription(ctx, root.authority.AgentID, subscription.ID); !errors.Is(err, session.ErrSubscriptionAccess) {
		t.Fatalf("foreign cancel error=%v", err)
	}
	value, err := root.SetBlackboard(ctx, root.authority.AgentID, "build", session.RuntimePayload{Data: []byte("green"), MediaType: "text/plain"})
	if err != nil || value.Version != 1 {
		t.Fatalf("actor blackboard mutation=%+v err=%v", value, err)
	}
	queued, err := store.LoadQueuedInbox(ctx, rootID, "subscriber", 0, 10)
	if err != nil || len(queued) != 1 || queued[0].Kind != "subscription" {
		t.Fatalf("subscription wake=%+v err=%v", queued, err)
	}
	var wake session.BlackboardWake
	if err := json.Unmarshal(queued[0].Payload.Inline, &wake); err != nil || wake.SubscriptionID != subscription.ID || wake.Key != "build" || wake.Version != 1 || wake.AuthorAgentID != root.authority.AgentID {
		t.Fatalf("wake=%+v err=%v", wake, err)
	}
	if err := daemon.Close(); err != nil {
		t.Fatal(err)
	}

	store = openStore(t, path)
	daemon, err = New(store, factory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = daemon.Close() })
	root, err = daemon.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	queued, err = store.LoadQueuedInbox(ctx, rootID, "subscriber", 0, 10)
	if err != nil || len(queued) != 1 || queued[0].Seq != 1 {
		t.Fatalf("recovered wake=%+v err=%v", queued, err)
	}
	listed, err = root.ListBlackboardSubscriptions(ctx, "subscriber")
	if err != nil || len(listed) != 1 || listed[0].Cursor != 1 {
		t.Fatalf("recovered subscriptions=%+v err=%v", listed, err)
	}
	if err := root.CancelBlackboardSubscription(ctx, "subscriber", subscription.ID); err != nil {
		t.Fatal(err)
	}
	listed, err = root.ListBlackboardSubscriptions(ctx, "subscriber")
	if err != nil || len(listed) != 0 {
		t.Fatalf("cancelled subscriptions=%+v err=%v", listed, err)
	}
	if _, err := root.SetBlackboard(ctx, root.authority.AgentID, "build", session.RuntimePayload{Data: []byte("still green"), MediaType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	queued, err = store.LoadQueuedInbox(ctx, rootID, "subscriber", 0, 10)
	if err != nil || len(queued) != 1 {
		t.Fatalf("cancelled subscription enqueued another wake=%+v err=%v", queued, err)
	}
}
