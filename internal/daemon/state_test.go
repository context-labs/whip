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

func TestStateWrappersRouteThroughActor(t *testing.T) {
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
	agentID := root.authority.AgentID
	payload := func(value string) session.RuntimePayload {
		return session.RuntimePayload{Data: []byte(value), MediaType: "text/plain"}
	}

	private, err := root.SetPrivateState(ctx, agentID, "private", payload("one"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := root.GetPrivateState(ctx, agentID, "private"); err != nil || got.Version != private.Version {
		t.Fatalf("private get=%+v err=%v", got, err)
	}
	private, err = root.AppendPrivateState(ctx, agentID, "private", payload(" two"))
	if err != nil || string(private.Payload.Inline) != "one two" {
		t.Fatalf("private append=%+v err=%v", private, err)
	}
	private, err = root.CompareAndSwapPrivateState(ctx, agentID, "private", private.Version, payload("three"))
	if err != nil || private.Version != 3 {
		t.Fatalf("private CAS=%+v err=%v", private, err)
	}
	if values, err := root.ListPrivateState(ctx, agentID); err != nil || len(values) != 1 || values[0].Version != 3 {
		t.Fatalf("private list=%+v err=%v", values, err)
	}

	blackboard, err := root.SetBlackboard(ctx, agentID, "shared", payload("one"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := root.GetBlackboard(ctx, agentID, "shared"); err != nil || got.Version != blackboard.Version {
		t.Fatalf("blackboard get=%+v err=%v", got, err)
	}
	blackboard, err = root.AppendBlackboard(ctx, agentID, "shared", payload(" two"))
	if err != nil || string(blackboard.Payload.Inline) != "one two" {
		t.Fatalf("blackboard append=%+v err=%v", blackboard, err)
	}
	blackboard, err = root.CompareAndSwapBlackboard(ctx, agentID, "shared", blackboard.Version, payload("three"))
	if err != nil || blackboard.Version != 3 {
		t.Fatalf("blackboard CAS=%+v err=%v", blackboard, err)
	}
	if values, err := root.BlackboardHistory(ctx, agentID, "shared"); err != nil || len(values) != 3 || values[2].Version != 3 {
		t.Fatalf("blackboard history=%+v err=%v", values, err)
	}
}

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
	if err := root.AdmitAgent(ctx, session.AgentAdmission{ParentAgentID: root.authority.AgentID, ChildAgentID: "subscriber", Name: "subscriber"}); err != nil {
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
	pending, err := store.ListMailboxMessages(ctx, rootID, "subscriber", "pending", "", 10)
	if err != nil || len(pending) != 1 || pending[0].Kind != session.MessageKindStateChanged || pending[0].Subject != "build" {
		t.Fatalf("subscription message=%+v err=%v", pending, err)
	}
	message, err := store.ReadMailboxMessage(ctx, rootID, "subscriber", pending[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var wake session.BlackboardWake
	if err := json.Unmarshal(message.Body.Inline, &wake); err != nil || wake.SubscriptionID != subscription.ID || wake.Key != "build" || wake.Version != 1 || wake.AuthorAgentID != root.authority.AgentID {
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
	pending, err = store.ListMailboxMessages(ctx, rootID, "subscriber", "pending", "", 10)
	if err != nil || len(pending) != 1 || pending[0].Status != "pending" {
		t.Fatalf("recovered message=%+v err=%v", pending, err)
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
	pending, err = store.ListMailboxMessages(ctx, rootID, "subscriber", "all", "", 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("cancelled subscription posted another message=%+v err=%v", pending, err)
	}
}
