package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
)

func TestStatePrivateIsolationBlackboardVisibilityAndHandles(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "left")
	admitTestChild(t, store, rootID, rootAgentID, "right")
	ctx := context.Background()

	left, err := store.SetPrivateState(ctx, rootID, "left", "same", RuntimePayload{Data: []byte("left"), MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	right, err := store.SetPrivateState(ctx, rootID, "right", "same", RuntimePayload{Data: []byte("right"), MediaType: "text/plain"})
	if err != nil {
		t.Fatal(err)
	}
	if left.Version != 1 || right.Version != 1 || string(left.Payload.Inline) != "left" || string(right.Payload.Inline) != "right" {
		t.Fatalf("isolated private values: left=%+v right=%+v", left, right)
	}
	left, err = store.AppendPrivateState(ctx, rootID, "left", "same", RuntimePayload{Data: []byte("!")})
	if err != nil || left.Version != 2 || string(left.Payload.Inline) != "left!" {
		t.Fatalf("private append=%+v err=%v", left, err)
	}
	left, err = store.CompareAndSwapPrivateState(ctx, rootID, "left", "same", 2, RuntimePayload{Data: []byte("final"), MediaType: "text/plain"})
	if err != nil || left.Version != 3 {
		t.Fatalf("private CAS=%+v err=%v", left, err)
	}
	if _, err := store.CompareAndSwapPrivateState(ctx, rootID, "left", "same", 2, RuntimePayload{Data: []byte("stale"), MediaType: "text/plain"}); !errors.Is(err, ErrStateVersion) {
		t.Fatalf("stale private CAS error=%v", err)
	}
	listed, err := store.ListPrivateState(ctx, rootID, "left")
	if err != nil || len(listed) != 1 || listed[0].Key != "same" || string(listed[0].Payload.Inline) != "final" {
		t.Fatalf("private list=%+v err=%v", listed, err)
	}

	large := bytes.Repeat([]byte("shared immutable bytes"), 512)
	privateLarge, err := store.SetPrivateState(ctx, rootID, "left", "large", RuntimePayload{Data: large, MediaType: "application/octet-stream"})
	if err != nil || privateLarge.Payload.ReferenceID == "" || len(privateLarge.Payload.Inline) != 0 {
		t.Fatalf("large private state=%+v err=%v", privateLarge, err)
	}
	shared, err := store.SetBlackboard(ctx, rootID, "left", "evidence", RuntimePayload{Data: large, MediaType: "application/octet-stream"})
	if err != nil || shared.Payload.ReferenceID == "" || len(shared.Payload.Inline) != 0 {
		t.Fatalf("large blackboard=%+v err=%v", shared, err)
	}
	if _, _, err := store.ReadContent(ctx, privateLarge.Payload.ReferenceID, rootID, "right", 0, len(large)); !errors.Is(err, ErrContentAccess) {
		t.Fatalf("sibling private handle read error=%v", err)
	}
	visible, err := store.GetBlackboard(ctx, rootID, "right", "evidence")
	if err != nil || visible.Payload.ReferenceID != shared.Payload.ReferenceID {
		t.Fatalf("sibling blackboard=%+v err=%v", visible, err)
	}
	got, _, err := store.ReadContent(ctx, visible.Payload.ReferenceID, rootID, "right", 0, len(large))
	if err != nil || !bytes.Equal(got, large) {
		t.Fatalf("shared blackboard handle bytes=%d err=%v", len(got), err)
	}
	var objects, references, agentGrants, rootGrants int
	if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM content_objects`).Scan(&objects); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM content_references`).Scan(&references); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM content_grants WHERE scope='agent' AND agent_id='left'`).Scan(&agentGrants); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(context.Background(), `SELECT count(*) FROM content_grants WHERE scope='root' AND agent_id=''`).Scan(&rootGrants); err != nil {
		t.Fatal(err)
	}
	if objects != 1 || references != 2 || agentGrants != 1 || rootGrants != 1 {
		t.Fatalf("content objects=%d references=%d agent grants=%d root grants=%d", objects, references, agentGrants, rootGrants)
	}

	if _, err := store.SetBlackboard(ctx, rootID, "left", "notes", RuntimePayload{Data: []byte(`[{"n":1}]`), MediaType: "application/json"}); err != nil {
		t.Fatal(err)
	}
	notes, err := store.AppendBlackboard(ctx, rootID, "right", "notes", RuntimePayload{Data: []byte(`{"n":2}`), MediaType: "application/json"})
	if err != nil || string(notes.Payload.Inline) != `[{"n":1},{"n":2}]` {
		t.Fatalf("JSON append=%q err=%v", notes.Payload.Inline, err)
	}
	if _, err := store.AppendBlackboard(ctx, rootID, "right", "notes", RuntimePayload{Data: []byte("{"), MediaType: "application/json"}); !errors.Is(err, ErrStateAppend) {
		t.Fatalf("invalid JSON append error=%v", err)
	}
	if _, err := store.AppendBlackboard(ctx, rootID, "right", "notes", RuntimePayload{Data: []byte("bad"), MediaType: "text/plain"}); !errors.Is(err, ErrStateAppend) {
		t.Fatalf("invalid append error=%v", err)
	}
}

func TestBlackboardCASRetainsHistoryAndAuditsStaleAttempt(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "left")
	admitTestChild(t, store, rootID, rootAgentID, "right")
	ctx := context.Background()
	if _, err := store.SetBlackboard(ctx, rootID, "left", "race", RuntimePayload{Data: []byte("initial"), MediaType: "text/plain"}); err != nil {
		t.Fatal(err)
	}

	type result struct {
		author string
		value  StateValue
		err    error
	}
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, author := range []string{"left", "right"} {
		wg.Go(func() {
			value, err := store.CompareAndSwapBlackboard(ctx, rootID, author, "race", 1, RuntimePayload{Data: []byte(author), MediaType: "text/plain"})
			results <- result{author: author, value: value, err: err}
		})
	}
	wg.Wait()
	close(results)
	var winner string
	for result := range results {
		switch {
		case result.err == nil:
			winner = result.author
			if result.value.Version != 2 || result.value.AuthorAgentID != result.author {
				t.Fatalf("winning CAS=%+v", result.value)
			}
		case errors.Is(result.err, ErrStateVersion):
		default:
			t.Fatalf("CAS %s error=%v", result.author, result.err)
		}
	}
	if winner == "" {
		t.Fatal("no CAS succeeded")
	}
	history, err := store.BlackboardHistory(ctx, rootID, "left", "race")
	if err != nil || len(history) != 2 || history[0].Version != 1 || history[1].Version != 2 || history[1].AuthorAgentID != winner {
		t.Fatalf("blackboard history=%+v err=%v", history, err)
	}
	current, err := store.GetBlackboard(ctx, rootID, "right", "race")
	if err != nil || current.Version != 2 || string(current.Payload.Inline) != winner {
		t.Fatalf("blackboard current=%+v err=%v", current, err)
	}

	rows, err := store.db.QueryContext(ctx, `SELECT payload_inline FROM events WHERE root_id=? AND kind='blackboard.cas' ORDER BY seq`, rootID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		var event actorEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}
		seen[event.AgentID] = event.Attempt
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[winner] != "accepted" || seen[map[string]string{"left": "right", "right": "left"}[winner]] != "stale" {
		t.Fatalf("CAS audit=%v winner=%q", seen, winner)
	}
}

func TestStateRejectsCrossRootTerminalAndInvalidInputs(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "child")
	otherRoot, err := store.Create(t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	otherAuthority, err := store.EnsureClassicAuthority(context.Background(), otherRoot)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := store.GetBlackboard(ctx, rootID, otherAuthority.AgentID, "key"); !errors.Is(err, ErrAgentAccess) {
		t.Fatalf("cross-root blackboard error=%v", err)
	}
	if _, err := store.SetPrivateState(ctx, rootID, "child", " bad", RuntimePayload{Data: []byte("x")}); err == nil {
		t.Fatal("invalid key was accepted")
	}
	if _, err := store.CompareAndSwapPrivateState(ctx, rootID, "child", "key", 0, RuntimePayload{Data: []byte("x")}); err == nil {
		t.Fatal("invalid CAS version was accepted")
	}
	if _, err := store.TerminalizeSubtree(ctx, rootID, rootAgentID, "child", "stopped"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPrivateState(ctx, rootID, "child", "key", RuntimePayload{Data: []byte("x")}); !errors.Is(err, ErrAgentTerminal) {
		t.Fatalf("terminal private state error=%v", err)
	}
	if _, err := store.CreateBlackboardSubscription(ctx, rootID, "child", "key"); !errors.Is(err, ErrAgentTerminal) {
		t.Fatalf("terminal subscription error=%v", err)
	}
}

func TestPrivateStateGetLargeAppendAndErrorCases(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "child")
	ctx := context.Background()

	if _, err := store.GetPrivateState(ctx, rootID, "child", "missing"); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("missing private state error=%v", err)
	}
	if _, err := store.GetPrivateState(ctx, rootID, "child", ""); err == nil {
		t.Fatal("invalid private state key was accepted")
	}
	body := bytes.Repeat([]byte("large text"), InlineValueLimit/4)
	if len(body) <= InlineValueLimit {
		t.Fatal("test payload is not large")
	}
	set, err := store.SetPrivateState(ctx, rootID, "child", "large", RuntimePayload{Data: body, MediaType: "text/plain", Source: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetPrivateState(ctx, rootID, "child", "large")
	if err != nil || got.Payload.ReferenceID != set.Payload.ReferenceID || len(got.Payload.Inline) != 0 {
		t.Fatalf("large private get=%+v err=%v", got, err)
	}
	got, err = store.AppendPrivateState(ctx, rootID, "child", "large", RuntimePayload{Data: []byte("!"), Source: "append"})
	if err != nil || got.Version != 2 || got.Payload.ReferenceID == "" || got.Payload.Source != "append" {
		t.Fatalf("large private append=%+v err=%v", got, err)
	}
	data, _, err := store.ReadContent(ctx, got.Payload.ReferenceID, rootID, "child", 0, len(body)+1)
	if err != nil || !bytes.Equal(data, append(append([]byte(nil), body...), '!')) {
		t.Fatalf("large appended content bytes=%d err=%v", len(data), err)
	}

	if _, err := store.AppendPrivateState(ctx, rootID, "child", "absent", RuntimePayload{Data: []byte("x")}); !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("missing append error=%v", err)
	}
	for name, payload := range map[string]RuntimePayload{
		"binary":      {Data: []byte{0xff}},
		"unsupported": {Data: bytes.Repeat([]byte("bytes"), InlineValueLimit), MediaType: "application/octet-stream"},
		"object":      {Data: []byte(`{"value":"` + strings.Repeat("x", InlineValueLimit) + `"}`), MediaType: "application/json"},
	} {
		if _, err := store.SetPrivateState(ctx, rootID, "child", name, payload); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AppendPrivateState(ctx, rootID, "child", name, RuntimePayload{Data: []byte("x")}); !errors.Is(err, ErrStateAppend) {
			t.Fatalf("%s append error=%v", name, err)
		}
	}
	if _, err := store.SetPrivateState(ctx, rootID, "child", "utf8", RuntimePayload{Data: []byte("ok"), MediaType: "text/plain"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendPrivateState(ctx, rootID, "child", "utf8", RuntimePayload{Data: []byte{0xff}}); !errors.Is(err, ErrStateAppend) {
		t.Fatalf("invalid UTF-8 append error=%v", err)
	}
	if _, err := store.SetPrivateState(ctx, rootID, "child", "version", RuntimePayload{Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE agent_state SET version=? WHERE root_id=? AND agent_id='child' AND key='version'`, int64(math.MaxInt64), rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPrivateState(ctx, rootID, "child", "version", RuntimePayload{Data: []byte("y")}); err == nil || !strings.Contains(err.Error(), "version exhausted") {
		t.Fatalf("exhausted version error=%v", err)
	}
}

func TestStateRejectsCorruptRows(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "child")
	ctx := context.Background()

	for _, key := range []string{"version", "oversized"} {
		if _, err := store.SetPrivateState(ctx, rootID, "child", key, RuntimePayload{Data: []byte("ok")}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE agent_state SET version=0 WHERE root_id=? AND agent_id='child' AND key='version'`, rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPrivateState(ctx, rootID, "child", "version"); err == nil || !strings.Contains(err.Error(), "invalid version") {
		t.Fatalf("corrupt version error=%v", err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE agent_state SET payload_inline=? WHERE root_id=? AND agent_id='child' AND key='oversized'`, bytes.Repeat([]byte("x"), InlineValueLimit+1), rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPrivateState(ctx, rootID, "child", "oversized"); err == nil || !strings.Contains(err.Error(), "oversized inline") {
		t.Fatalf("oversized inline error=%v", err)
	}
	large, err := store.SetPrivateState(ctx, rootID, "child", "reference", RuntimePayload{Data: bytes.Repeat([]byte("x"), InlineValueLimit+1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE content_references SET size=-1 WHERE id=?`, large.Payload.ReferenceID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPrivateState(ctx, rootID, "child", "reference"); err == nil || !strings.Contains(err.Error(), "references missing content") {
		t.Fatalf("corrupt reference error=%v", err)
	}
}

func TestBlackboardSubscriptionLifecycleAndCorruptRows(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "watcher")
	admitTestChild(t, store, rootID, rootAgentID, "author")
	ctx := context.Background()

	subscription, err := store.CreateBlackboardSubscription(ctx, rootID, "watcher", "topic")
	if err != nil || subscription.Cursor != 0 || subscription.Status != "active" {
		t.Fatalf("subscription=%+v err=%v", subscription, err)
	}
	duplicate, err := store.CreateBlackboardSubscription(ctx, rootID, "watcher", "topic")
	if err != nil || duplicate.ID != subscription.ID {
		t.Fatalf("duplicate subscription=%+v err=%v", duplicate, err)
	}
	if _, err := store.SetBlackboard(ctx, rootID, "author", "topic", RuntimePayload{Data: []byte("first")}); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListBlackboardSubscriptions(ctx, rootID, "watcher")
	if err != nil || len(listed) != 1 || listed[0].Cursor != 1 {
		t.Fatalf("subscriptions=%+v err=%v", listed, err)
	}
	items, err := store.LoadQueuedInbox(ctx, rootID, "watcher", 0, 10)
	if err != nil || len(items) != 1 || items[0].Kind != "subscription" {
		t.Fatalf("subscription inbox=%+v err=%v", items, err)
	}
	var wake BlackboardWake
	if err := json.Unmarshal(items[0].Payload.Inline, &wake); err != nil || wake.SubscriptionID != subscription.ID || wake.Key != "topic" || wake.Version != 1 || wake.AuthorAgentID != "author" {
		t.Fatalf("subscription wake=%+v err=%v", wake, err)
	}

	if err := store.CancelBlackboardSubscription(ctx, rootID, "author", subscription.ID); !errors.Is(err, ErrSubscriptionAccess) {
		t.Fatalf("wrong-owner cancellation error=%v", err)
	}
	if err := store.CancelBlackboardSubscription(ctx, rootID, "watcher", "invalid"); err == nil {
		t.Fatal("invalid subscription ID was accepted")
	}
	if err := store.CancelBlackboardSubscription(ctx, rootID, "watcher", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"); !errors.Is(err, ErrSubscriptionAccess) {
		t.Fatalf("missing subscription cancellation error=%v", err)
	}
	if err := store.CancelBlackboardSubscription(ctx, rootID, "watcher", subscription.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.CancelBlackboardSubscription(ctx, rootID, "watcher", subscription.ID); !errors.Is(err, ErrSubscriptionInactive) {
		t.Fatalf("duplicate cancellation error=%v", err)
	}
	listed, err = store.ListBlackboardSubscriptions(ctx, rootID, "watcher")
	if err != nil || len(listed) != 0 {
		t.Fatalf("cancelled subscriptions=%+v err=%v", listed, err)
	}
	if _, err := store.SetBlackboard(ctx, rootID, "author", "topic", RuntimePayload{Data: []byte("second")}); err != nil {
		t.Fatal(err)
	}
	items, err = store.LoadQueuedInbox(ctx, rootID, "watcher", 0, 10)
	if err != nil || len(items) != 1 {
		t.Fatalf("cancelled subscription inbox=%+v err=%v", items, err)
	}
	recreated, err := store.CreateBlackboardSubscription(ctx, rootID, "watcher", "topic")
	if err != nil || recreated.ID == subscription.ID || recreated.Cursor != 2 {
		t.Fatalf("recreated subscription=%+v err=%v", recreated, err)
	}

	if _, err := store.db.ExecContext(ctx, `INSERT INTO subscriptions(id,root_id,agent_id,key,cursor,status,created_at,updated_at) VALUES('aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',?,?,?,'bad','active',?,?)`, rootID, "watcher", "corrupt", now(), now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateBlackboardSubscription(ctx, rootID, "watcher", "corrupt"); err == nil {
		t.Fatal("corrupt active subscription was accepted")
	}
	if _, err := store.ListBlackboardSubscriptions(ctx, rootID, "watcher"); err == nil {
		t.Fatal("corrupt subscription row was listed")
	}
}

func TestStateAPIsReturnClosedStoreErrors(t *testing.T) {
	store, rootID, agentID := newSwarmFixture(t)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"get private":  func() error { _, err := store.GetPrivateState(ctx, rootID, agentID, "key"); return err },
		"list private": func() error { _, err := store.ListPrivateState(ctx, rootID, agentID); return err },
		"set private": func() error {
			_, err := store.SetPrivateState(ctx, rootID, agentID, "key", RuntimePayload{Data: []byte("x")})
			return err
		},
		"get blackboard": func() error { _, err := store.GetBlackboard(ctx, rootID, agentID, "key"); return err },
		"set blackboard": func() error {
			_, err := store.SetBlackboard(ctx, rootID, agentID, "key", RuntimePayload{Data: []byte("x")})
			return err
		},
		"history": func() error { _, err := store.BlackboardHistory(ctx, rootID, agentID, "key"); return err },
		"create subscription": func() error {
			_, err := store.CreateBlackboardSubscription(ctx, rootID, agentID, "key")
			return err
		},
		"list subscriptions": func() error { _, err := store.ListBlackboardSubscriptions(ctx, rootID, agentID); return err },
		"cancel subscription": func() error {
			return store.CancelBlackboardSubscription(ctx, rootID, agentID, strings.Repeat("a", 32))
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("closed store call succeeded")
			}
		})
	}
}

func TestStatePublicErrorAndCorruptionPaths(t *testing.T) {
	store, rootID, rootAgentID := newSwarmFixture(t)
	admitTestChild(t, store, rootID, rootAgentID, "child")
	ctx := context.Background()

	for name, call := range map[string]func() error{
		"get private caller":  func() error { _, err := store.GetPrivateState(ctx, rootID, "", "key"); return err },
		"list private caller": func() error { _, err := store.ListPrivateState(ctx, rootID, ""); return err },
		"set blackboard caller": func() error {
			_, err := store.SetBlackboard(ctx, rootID, "", "key", RuntimePayload{Data: []byte("x")})
			return err
		},
		"history caller":            func() error { _, err := store.BlackboardHistory(ctx, rootID, "", "key"); return err },
		"list subscriptions caller": func() error { _, err := store.ListBlackboardSubscriptions(ctx, rootID, ""); return err },
		"cancel subscription caller": func() error {
			return store.CancelBlackboardSubscription(ctx, rootID, "", strings.Repeat("a", 32))
		},
		"get blackboard key": func() error { _, err := store.GetBlackboard(ctx, rootID, "child", ""); return err },
		"set blackboard key": func() error {
			_, err := store.SetBlackboard(ctx, rootID, "child", "", RuntimePayload{Data: []byte("x")})
			return err
		},
		"history key":        func() error { _, err := store.BlackboardHistory(ctx, rootID, "child", ""); return err },
		"subscription key":   func() error { _, err := store.CreateBlackboardSubscription(ctx, rootID, "child", ""); return err },
		"missing blackboard": func() error { _, err := store.GetBlackboard(ctx, rootID, "child", "missing"); return err },
		"missing append": func() error {
			_, err := store.AppendBlackboard(ctx, rootID, "child", "missing", RuntimePayload{Data: []byte("x")})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("invalid call succeeded")
			}
		})
	}

	if _, err := store.SetPrivateState(ctx, rootID, "child", "corrupt", RuntimePayload{Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE agent_state SET version=0 WHERE root_id=? AND agent_id='child' AND key='corrupt'`, rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPrivateState(ctx, rootID, "child", "corrupt", RuntimePayload{Data: []byte("y")}); err == nil {
		t.Fatal("corrupt private state was replaced")
	}
	if _, err := store.ListPrivateState(ctx, rootID, "child"); err == nil {
		t.Fatal("corrupt private state was listed")
	}

	if _, err := store.SetBlackboard(ctx, rootID, "child", "corrupt", RuntimePayload{Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE blackboard SET version=0 WHERE root_id=? AND key='corrupt'`, rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetBlackboard(ctx, rootID, "child", "corrupt", RuntimePayload{Data: []byte("y")}); err == nil {
		t.Fatal("corrupt blackboard value was replaced")
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE blackboard_history SET version=0 WHERE root_id=? AND key='corrupt'`, rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BlackboardHistory(ctx, rootID, "child", "corrupt"); err == nil {
		t.Fatal("corrupt blackboard history was returned")
	}

	if _, err := store.SetBlackboard(ctx, rootID, "child", "max", RuntimePayload{Data: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `UPDATE blackboard SET version=? WHERE root_id=? AND key='max'`, int64(math.MaxInt64), rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetBlackboard(ctx, rootID, "child", "max", RuntimePayload{Data: []byte("y")}); err == nil || !strings.Contains(err.Error(), "version exhausted") {
		t.Fatalf("exhausted blackboard version error=%v", err)
	}

	if err := validateStateMutation("key", "delete", 0); err == nil {
		t.Fatal("invalid mutation was accepted")
	}
	if _, err := store.appendStatePayload(RuntimeValue{ReferenceID: "missing", Digest: strings.Repeat("0", 64), Size: 1}, RuntimePayload{Data: []byte("x")}); err == nil {
		t.Fatal("missing referenced state content was appended")
	}
	if _, err := store.SetPrivateState(ctx, rootID, "child", "typed", RuntimePayload{Data: []byte("a"), MediaType: "text/plain; charset=utf-8"}); err != nil {
		t.Fatal(err)
	}
	if value, err := store.AppendPrivateState(ctx, rootID, "child", "typed", RuntimePayload{Data: []byte("b"), MediaType: "text/plain"}); err != nil || string(value.Payload.Inline) != "ab" {
		t.Fatalf("parameterized text append=%+v err=%v", value, err)
	}
}

func TestStateStorageFailuresAreReturned(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		fault string
		setup func(*testing.T, *Store, string) string
		call  func(*Store, string, string) error
	}{
		{"private content", `CREATE TRIGGER fail_state BEFORE INSERT ON content_objects BEGIN SELECT RAISE(ABORT,'test'); END`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.SetPrivateState(ctx, rootID, "child", "key", RuntimePayload{Data: bytes.Repeat([]byte("x"), InlineValueLimit+1)})
			return err
		}},
		{"private row", `CREATE TRIGGER fail_state BEFORE INSERT ON agent_state BEGIN SELECT RAISE(ABORT,'test'); END`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.SetPrivateState(ctx, rootID, "child", "key", RuntimePayload{Data: []byte("x")})
			return err
		}},
		{"private event", `CREATE TRIGGER fail_state BEFORE INSERT ON events WHEN NEW.kind='state.private.set' BEGIN SELECT RAISE(ABORT,'test'); END`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.SetPrivateState(ctx, rootID, "child", "key", RuntimePayload{Data: []byte("x")})
			return err
		}},
		{"blackboard content", `CREATE TRIGGER fail_state BEFORE INSERT ON content_objects BEGIN SELECT RAISE(ABORT,'test'); END`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.SetBlackboard(ctx, rootID, "child", "key", RuntimePayload{Data: bytes.Repeat([]byte("x"), InlineValueLimit+1)})
			return err
		}},
		{"blackboard row", `CREATE TRIGGER fail_state BEFORE INSERT ON blackboard BEGIN SELECT RAISE(ABORT,'test'); END`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.SetBlackboard(ctx, rootID, "child", "key", RuntimePayload{Data: []byte("x")})
			return err
		}},
		{"blackboard history", `CREATE TRIGGER fail_state BEFORE INSERT ON blackboard_history BEGIN SELECT RAISE(ABORT,'test'); END`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.SetBlackboard(ctx, rootID, "child", "key", RuntimePayload{Data: []byte("x")})
			return err
		}},
		{"blackboard event", `CREATE TRIGGER fail_state BEFORE INSERT ON events WHEN NEW.kind='blackboard.set' BEGIN SELECT RAISE(ABORT,'test'); END`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.SetBlackboard(ctx, rootID, "child", "key", RuntimePayload{Data: []byte("x")})
			return err
		}},
		{"wake inbox", `CREATE TRIGGER fail_state BEFORE INSERT ON inbox BEGIN SELECT RAISE(ABORT,'test'); END`, func(t *testing.T, store *Store, rootID string) string {
			t.Helper()
			if _, err := store.CreateBlackboardSubscription(ctx, rootID, "child", "key"); err != nil {
				t.Fatal(err)
			}
			return ""
		}, func(store *Store, rootID, _ string) error {
			_, err := store.SetBlackboard(ctx, rootID, "author", "key", RuntimePayload{Data: []byte("x")})
			return err
		}},
		{"subscription row", `CREATE TRIGGER fail_state BEFORE INSERT ON subscriptions BEGIN SELECT RAISE(ABORT,'test'); END`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.CreateBlackboardSubscription(ctx, rootID, "child", "key")
			return err
		}},
		{"subscription event", `CREATE TRIGGER fail_state BEFORE INSERT ON events WHEN NEW.kind='subscription.created' BEGIN SELECT RAISE(ABORT,'test'); END`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.CreateBlackboardSubscription(ctx, rootID, "child", "key")
			return err
		}},
		{"subscription update", `CREATE TRIGGER fail_state BEFORE UPDATE ON subscriptions BEGIN SELECT RAISE(ABORT,'test'); END`, func(t *testing.T, store *Store, rootID string) string {
			t.Helper()
			subscription, err := store.CreateBlackboardSubscription(ctx, rootID, "child", "key")
			if err != nil {
				t.Fatal(err)
			}
			return subscription.ID
		}, func(store *Store, rootID, id string) error {
			return store.CancelBlackboardSubscription(ctx, rootID, "child", id)
		}},
		{"subscription cancel event", `CREATE TRIGGER fail_state BEFORE INSERT ON events WHEN NEW.kind='subscription.cancelled' BEGIN SELECT RAISE(ABORT,'test'); END`, func(t *testing.T, store *Store, rootID string) string {
			t.Helper()
			subscription, err := store.CreateBlackboardSubscription(ctx, rootID, "child", "key")
			if err != nil {
				t.Fatal(err)
			}
			return subscription.ID
		}, func(store *Store, rootID, id string) error {
			return store.CancelBlackboardSubscription(ctx, rootID, "child", id)
		}},
		{"private list query", `DROP TABLE agent_state`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.ListPrivateState(ctx, rootID, "child")
			return err
		}},
		{"history query", `DROP TABLE blackboard_history`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.BlackboardHistory(ctx, rootID, "child", "key")
			return err
		}},
		{"subscription cursor query", `DROP TABLE blackboard`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.CreateBlackboardSubscription(ctx, rootID, "child", "key")
			return err
		}},
		{"subscription list query", `DROP TABLE subscriptions`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.ListBlackboardSubscriptions(ctx, rootID, "child")
			return err
		}},
		{"subscription cancel query", `DROP TABLE subscriptions`, nil, func(store *Store, rootID, _ string) error {
			return store.CancelBlackboardSubscription(ctx, rootID, "child", strings.Repeat("a", 32))
		}},
		{"subscription wake query", `DROP TABLE subscriptions`, nil, func(store *Store, rootID, _ string) error {
			_, err := store.SetBlackboard(ctx, rootID, "author", "key", RuntimePayload{Data: []byte("x")})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, rootID, rootAgentID := newSwarmFixture(t)
			admitTestChild(t, store, rootID, rootAgentID, "child")
			admitTestChild(t, store, rootID, rootAgentID, "author")
			var value string
			if test.setup != nil {
				value = test.setup(t, store, rootID)
			}
			var before stateFailureSnapshot
			verifyRollback := strings.HasPrefix(test.fault, "CREATE TRIGGER")
			if verifyRollback {
				before = snapshotStateFailure(t, store, rootID)
			}
			if _, err := store.db.ExecContext(ctx, test.fault); err != nil {
				t.Fatal(err)
			}
			if err := test.call(store, rootID, value); err == nil {
				t.Fatal("storage failure was ignored")
			}
			if verifyRollback {
				if after := snapshotStateFailure(t, store, rootID); after != before {
					t.Fatalf("failed mutation changed state: before=%+v after=%+v", before, after)
				}
			}
		})
	}
}

type stateFailureSnapshot struct {
	privateState, blackboard, history, events, inbox int64
	subscriptions, objects, references, grants       int64
	subscriptionState                                string
}

func snapshotStateFailure(t *testing.T, store *Store, rootID string) stateFailureSnapshot {
	t.Helper()
	var snapshot stateFailureSnapshot
	err := store.db.QueryRowContext(context.Background(), `SELECT
		(SELECT count(*) FROM agent_state WHERE root_id=?),
		(SELECT count(*) FROM blackboard WHERE root_id=?),
		(SELECT count(*) FROM blackboard_history WHERE root_id=?),
		(SELECT count(*) FROM events WHERE root_id=?),
		(SELECT count(*) FROM inbox WHERE root_id=?),
		(SELECT count(*) FROM subscriptions WHERE root_id=?),
		(SELECT count(*) FROM content_objects),(SELECT count(*) FROM content_references),
		(SELECT count(*) FROM content_grants WHERE root_id=?),
		COALESCE((SELECT group_concat(id || ':' || status || ':' || cursor, '|') FROM subscriptions WHERE root_id=?),'')`,
		rootID, rootID, rootID, rootID, rootID, rootID, rootID, rootID).Scan(
		&snapshot.privateState, &snapshot.blackboard, &snapshot.history, &snapshot.events, &snapshot.inbox,
		&snapshot.subscriptions, &snapshot.objects, &snapshot.references, &snapshot.grants, &snapshot.subscriptionState,
	)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
