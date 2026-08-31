package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	if err := store.db.QueryRow(`SELECT count(*) FROM content_objects`).Scan(&objects); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM content_references`).Scan(&references); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM content_grants WHERE scope='agent' AND agent_id='left'`).Scan(&agentGrants); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow(`SELECT count(*) FROM content_grants WHERE scope='root' AND agent_id=''`).Scan(&rootGrants); err != nil {
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
		author := author
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, err := store.CompareAndSwapBlackboard(ctx, rootID, author, "race", 1, RuntimePayload{Data: []byte(author), MediaType: "text/plain"})
			results <- result{author: author, value: value, err: err}
		}()
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

	rows, err := store.db.Query(`SELECT payload_inline FROM events WHERE root_id=? AND kind='blackboard.cas' ORDER BY seq`, rootID)
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
