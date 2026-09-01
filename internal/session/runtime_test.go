package session

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
)

func TestRuntimeTransitionIsAtomicAndLargeValuesAreHandleBacked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := OpenWithDefaultMode(path, ModeRLM)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create("/workspace", "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	large := bytes.Repeat([]byte("runtime-value"), 1024)
	transition := RuntimeTransition{
		Agent: &RuntimeAgent{ID: "agent-root", RootID: rootID, Status: "idle"},
		Command: &RuntimeCommand{
			ClientID: "client-1", ID: "command-1", Scope: CommandScopeRoot, RootID: rootID,
			RequestDigest: "request-digest", Status: "queued", Payload: RuntimePayload{Data: large, MediaType: "application/json", Source: "command"},
		},
		Inbox: &RuntimeInbox{RootID: rootID, AgentID: "agent-root", Seq: 1, Kind: "command", Status: "queued", Payload: RuntimePayload{Data: large, Source: "inbox"}},
		State: &RuntimeState{RootID: rootID, AgentID: "agent-root", Key: "working", Version: 1, AuthorAgentID: "agent-root", Payload: RuntimePayload{Data: large, Source: "state"}},
		Event: &RuntimeEvent{RootID: rootID, Seq: 1, Kind: "transition", Payload: RuntimePayload{Data: large, Source: "event"}},
		Usage: &RuntimeUsage{ID: "usage-1", RootID: rootID, AgentID: "agent-root", CommandClientID: "client-1", CommandID: "command-1", InputTokens: 12, CachedTokens: 3, OutputTokens: 4, CostMicros: 55},
	}

	result, err := st.commitRuntime(context.Background(), transition, func() error {
		reader, err := sql.Open("sqlite", path)
		if err != nil {
			return err
		}
		defer reader.Close()
		for _, table := range []string{"agents", "commands", "inbox", "agent_state", "events", "usage_charges", "content_references"} {
			var n int
			if err := reader.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
				return err
			}
			if n != 0 {
				t.Fatalf("reader observed partial %s rows before commit: %d", table, n)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	values := []RuntimeValue{result.Command, result.Inbox, result.State, result.Event}
	for _, value := range values {
		if value.ReferenceID == "" || len(value.Inline) != 0 || value.Size != int64(len(large)) {
			t.Fatalf("large value was not handle-backed: %+v", value)
		}
		if value.Digest != values[0].Digest {
			t.Fatalf("equal values should share a body: %q != %q", value.Digest, values[0].Digest)
		}
	}
	var bodies int
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM content_objects`).Scan(&bodies); err != nil || bodies != 1 {
		t.Fatalf("content objects = %d, err %v", bodies, err)
	}
	for _, table := range []string{"agents", "commands", "inbox", "agent_state", "events", "usage_charges"} {
		var n int
		if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s rows = %d, err %v", table, n, err)
		}
	}
	for _, table := range []string{"commands", "inbox", "agent_state", "events"} {
		var inline, refs int
		if err := st.db.QueryRowContext(context.Background(), `SELECT COALESCE(MAX(length(payload_inline)),0), count(payload_ref) FROM `+table).Scan(&inline, &refs); err != nil {
			t.Fatal(err)
		}
		if inline > InlineValueLimit || refs != 1 {
			t.Fatalf("%s persisted inline=%d refs=%d", table, inline, refs)
		}
	}
	got, meta, err := st.ReadContent(context.Background(), result.Command.ReferenceID, rootID, "agent-root", 0, len(large)+1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, large) || meta.Source != "command" || meta.MediaType != "application/json" {
		t.Fatalf("authorized read = %d bytes, metadata %+v", len(got), meta)
	}
	outcome, err := st.FinishCommand(context.Background(), "client-1", "command-1", "succeeded", RuntimePayload{Data: large, Source: "outcome"})
	if err != nil || outcome.ReferenceID == "" {
		t.Fatalf("terminal command outcome = %+v, err %v", outcome, err)
	}
	if _, err := st.FinishCommand(context.Background(), "client-1", "command-1", "failed", RuntimePayload{Data: []byte("replacement")}); err == nil {
		t.Fatal("terminal command outcome should be immutable")
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var status string
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM commands WHERE client_id='client-1' AND command_id='command-1'`).Scan(&status); err != nil || status != "succeeded" {
		t.Fatalf("terminal command status=%q err=%v", status, err)
	}
	got, meta, err = st.ReadContent(context.Background(), outcome.ReferenceID, rootID, "agent-root", 0, len(large))
	if err != nil || !bytes.Equal(got, large) || meta.Source != "outcome" {
		t.Fatalf("committed outcome did not survive reopen: %d bytes, %+v, %v", len(got), meta, err)
	}
}

func TestRuntimeValueInlineBoundary(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rootID, _ := st.Create("/workspace", "m", "p")
	inline := bytes.Repeat([]byte("i"), InlineValueLimit)
	result, err := st.CommitRuntime(context.Background(), RuntimeTransition{Event: &RuntimeEvent{RootID: rootID, Seq: 1, Kind: "inline", Payload: RuntimePayload{Data: inline}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Event.Inline) != InlineValueLimit || result.Event.ReferenceID != "" {
		t.Fatalf("8 KiB value should stay inline: %+v", result.Event)
	}
	handled := append(inline, 'x')
	result, err = st.CommitRuntime(context.Background(), RuntimeTransition{Event: &RuntimeEvent{RootID: rootID, Seq: 2, Kind: "handle", Payload: RuntimePayload{Data: handled}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Event.Inline) != 0 || result.Event.ReferenceID == "" {
		t.Fatalf("value over 8 KiB should use a handle: %+v", result.Event)
	}
}

func TestContentReferencesEnforceRootAgentAndSubtreeGrants(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	root1, _ := st.Create("/one", "m", "p")
	root2, _ := st.Create("/two", "m", "p")
	for _, agent := range []RuntimeAgent{
		{ID: "parent", RootID: root1, Status: "idle"},
		{ID: "child", RootID: root1, ParentID: "parent", Status: "idle"},
		{ID: "sibling", RootID: root1, Status: "idle"},
		{ID: "other-root", RootID: root2, Status: "idle"},
	} {
		a := agent
		if _, err := st.CommitRuntime(context.Background(), RuntimeTransition{Agent: &a}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.CommitRuntime(context.Background(), RuntimeTransition{Inbox: &RuntimeInbox{
		RootID: root1, AgentID: "other-root", Seq: 99, Kind: "invalid", Status: "queued", Payload: RuntimePayload{Data: []byte("x")},
	}}); err == nil {
		t.Fatal("cross-root inbox ownership should fail")
	}
	if _, err := st.db.ExecContext(context.Background(), `INSERT INTO blackboard(root_id,key,version,author_agent_id,updated_at) VALUES(?,?,?,?,?)`, root1, "invalid", 1, "other-root", now()); err == nil {
		t.Fatal("cross-root blackboard authorship should fail")
	}
	if _, err := st.CommitRuntime(context.Background(), RuntimeTransition{
		Command: &RuntimeCommand{ClientID: "mixed", ID: "mixed", Scope: CommandScopeRoot, RootID: root1, RequestDigest: "d", Status: "queued"},
		Event:   &RuntimeEvent{RootID: root2, Seq: 1, Kind: "mixed"},
	}); err == nil {
		t.Fatal("one transition should not span roots")
	}
	if _, err := st.CommitRuntime(context.Background(), RuntimeTransition{Usage: &RuntimeUsage{ID: "cross-root-usage", RootID: root1, AgentID: "other-root"}}); err == nil {
		t.Fatal("usage should not name an agent from another root")
	}
	exec(t, st, `INSERT INTO operations(id,root_id,agent_id,status,created_at,updated_at) VALUES('other-operation',?,'other-root','running',?,?)`, root2, now(), now())
	if _, err := st.db.ExecContext(context.Background(), `INSERT INTO leases(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES('cross-root-lease',?,'parent','other-operation','running',?,?)`, root1, now(), now()); err == nil {
		t.Fatal("lease should not name an operation from another root")
	}
	body := bytes.Repeat([]byte("authorized"), 8192)

	subtree, err := st.StoreContent(context.Background(), ContentGrant{RootID: root1, AgentID: "parent", Scope: ContentGrantSubtree}, RuntimePayload{Data: body})
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.StoreContent(context.Background(), ContentGrant{RootID: root2, AgentID: "other-root", Scope: ContentGrantAgent}, RuntimePayload{Data: body})
	if err != nil {
		t.Fatal(err)
	}
	if subtree.Digest != other.Digest || subtree.ReferenceID == other.ReferenceID {
		t.Fatalf("deduplication must share bytes, not authority: subtree=%+v other=%+v", subtree, other)
	}
	if _, _, err := st.ReadContent(context.Background(), other.ReferenceID, root2, "other-root", 0, 10); err != nil {
		t.Fatalf("exact agent grant should authorize its agent: %v", err)
	}
	if _, _, err := st.ReadContent(context.Background(), subtree.ReferenceID, root1, "child", 0, 10); err != nil {
		t.Fatalf("descendant should read subtree grant: %v", err)
	}
	for name, args := range map[string][3]string{
		"sibling":      {subtree.ReferenceID, root1, "sibling"},
		"other root":   {subtree.ReferenceID, root2, "other-root"},
		"digest reuse": {subtree.Digest, root1, "child"},
	} {
		if _, _, err := st.ReadContent(context.Background(), args[0], args[1], args[2], 0, 10); err == nil {
			t.Errorf("%s should not authorize content", name)
		}
	}
	rootGrant, err := st.StoreContent(context.Background(), ContentGrant{RootID: root1, Scope: ContentGrantRoot}, RuntimePayload{Data: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ReadContent(context.Background(), rootGrant.ReferenceID, root1, "sibling", 0, 10); err != nil {
		t.Fatalf("root grant should authorize a root agent: %v", err)
	}
	if got, _, err := st.ReadContent(context.Background(), rootGrant.ReferenceID, root1, "sibling", 0, len(body)); err != nil || len(got) != MaxContentRead {
		t.Fatalf("authorized read cap = %d bytes, err %v", len(got), err)
	}
	small, err := st.StoreContent(context.Background(), ContentGrant{RootID: root1, Scope: ContentGrantRoot}, RuntimePayload{Data: []byte("small")})
	if err != nil || small.ReferenceID == "" || len(small.Inline) != 0 {
		t.Fatalf("explicit content store must return a handle: %+v, %v", small, err)
	}
	if got, _, err := st.ReadContent(context.Background(), small.ReferenceID, root1, "sibling", 0, 5); err != nil || string(got) != "small" {
		t.Fatalf("small content handle read = %q, %v", got, err)
	}
	if err := st.RevokeContentGrant(context.Background(), subtree.ReferenceID, root1, "parent"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ReadContent(context.Background(), subtree.ReferenceID, root1, "child", 0, 10); err == nil {
		t.Fatal("revoked grant should not authorize a descendant")
	}
}

func TestInboxSequencesPersistAndConsumedItemsDoNotReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rootID, err := st.Create(t.TempDir(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}

	large := bytes.Repeat([]byte("queued-payload"), 8192)
	first, err := st.EnqueueInbox(context.Background(), InboxEnqueue{
		RootID: rootID, AgentID: authority.AgentID, Kind: "command", TraceID: "trace-large",
		Payload: RuntimePayload{Data: large, MediaType: "text/plain", Source: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}

	const concurrent = 24
	pairs := make(chan InboxSequence, concurrent)
	errs := make(chan error, concurrent)
	var wg sync.WaitGroup
	for range concurrent {
		wg.Go(func() {
			pair, err := st.EnqueueInbox(context.Background(), InboxEnqueue{
				RootID: rootID, AgentID: authority.AgentID, Kind: "command",
				Payload: RuntimePayload{Data: []byte("small")},
			})
			if err != nil {
				errs <- err
				return
			}
			pairs <- pair
		})
	}
	wg.Wait()
	close(pairs)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	gotPairs := []InboxSequence{first}
	for pair := range pairs {
		gotPairs = append(gotPairs, pair)
	}
	sort.Slice(gotPairs, func(i, j int) bool { return gotPairs[i].InboxSeq < gotPairs[j].InboxSeq })
	for i, pair := range gotPairs {
		want := int64(i + 1)
		if pair.InboxSeq != want || pair.EventSeq != want {
			t.Fatalf("sequence pair %d = %+v, want inbox/event %d", i, pair, want)
		}
	}
	var eventPayload []byte
	if err := st.db.QueryRowContext(context.Background(), `SELECT payload_inline FROM events WHERE root_id=? AND seq=1`, rootID).Scan(&eventPayload); err != nil {
		t.Fatal(err)
	}
	var event actorEvent
	if err := json.Unmarshal(eventPayload, &event); err != nil || event.AgentID != authority.AgentID || event.InboxSeq != 1 || event.TraceID != "trace-large" {
		t.Fatalf("correlated enqueue event=%+v err=%v", event, err)
	}

	items, err := st.LoadQueuedInbox(context.Background(), rootID, authority.AgentID, 0, concurrent+1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != concurrent+1 || items[0].Seq != 1 || items[len(items)-1].Seq != concurrent+1 {
		t.Fatalf("ordered inbox = %+v", items)
	}
	if items[0].Payload.ReferenceID == "" || len(items[0].Payload.Inline) != 0 || items[0].Payload.Size != int64(len(large)) {
		t.Fatalf("large inbox payload was not left handle-backed: %+v", items[0].Payload)
	}
	if body, _, err := st.ReadContent(context.Background(), items[0].Payload.ReferenceID, rootID, authority.AgentID, 0, len(large)); err != nil || len(body) != MaxContentRead {
		t.Fatalf("bounded inbox content read = %d bytes, err %v", len(body), err)
	}

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	items, err = st.LoadQueuedInbox(context.Background(), rootID, authority.AgentID, 0, concurrent+1)
	if err != nil || len(items) != concurrent+1 {
		t.Fatalf("reopened inbox count=%d err=%v", len(items), err)
	}
	eventSeq, err := st.ConsumeInbox(context.Background(), rootID, authority.AgentID, 1)
	if err != nil || eventSeq != concurrent+2 {
		t.Fatalf("consume event seq=%d err=%v", eventSeq, err)
	}
	if _, err := st.ConsumeInbox(context.Background(), rootID, authority.AgentID, 1); !errors.Is(err, ErrInboxTerminal) {
		t.Fatalf("second consume error = %v", err)
	}
	items, err = st.LoadQueuedInbox(context.Background(), rootID, authority.AgentID, 0, concurrent+1)
	if err != nil || len(items) != concurrent || items[0].Seq != 2 {
		t.Fatalf("consumed item replayed: first=%d count=%d err=%v", items[0].Seq, len(items), err)
	}
}

func TestClassicTurnCommitAtomicallyAppendsHistoryAndConsumesAcknowledgedInbox(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, _ := st.Create(t.TempDir(), "old-model", "old-provider")
	authority, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	stale := []llm.Message{{Role: "user", Content: "stale"}, {Role: "assistant", Content: "one"}, {Role: "user", Content: "stale two"}, {Role: "assistant", Content: "tail"}}
	if err := st.Save(rootID, 0, stale, "old-model", "old-provider"); err != nil {
		t.Fatal(err)
	}
	first, err := st.EnqueueInbox(context.Background(), InboxEnqueue{RootID: rootID, AgentID: authority.AgentID, Kind: "submit", Payload: RuntimePayload{Data: []byte("work")}})
	if err != nil {
		t.Fatal(err)
	}
	steer, err := st.EnqueueInbox(context.Background(), InboxEnqueue{RootID: rootID, AgentID: authority.AgentID, Kind: "steer", Payload: RuntimePayload{Data: []byte("adjust")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.StartClassicTurn(context.Background(), rootID, authority.AgentID, first.InboxSeq); err != nil {
		t.Fatal(err)
	}
	history := []llm.Message{
		{Role: "system", Content: "not persisted"},
		{Role: "user", Content: "work", Authored: true},
		{Role: "user", Content: "adjust"},
		{Role: "assistant", Content: "done"},
	}
	if err := st.CommitClassicTurn(context.Background(), ClassicTurnCommit{
		RootID: rootID, AgentID: authority.AgentID, InboxSeq: first.InboxSeq,
		AcknowledgedInbox: []int64{steer.InboxSeq}, Messages: history,
		Model: "new-model", Provider: "new-provider",
	}); err != nil {
		t.Fatal(err)
	}

	meta, restored, err := st.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Model != "new-model" || meta.Provider != "new-provider" {
		t.Fatalf("route=%s/%s", meta.Provider, meta.Model)
	}
	if len(restored) != 7 || restored[0].Content != "stale" || restored[4].Content != "work" || restored[6].Content != "done" {
		t.Fatalf("restored history=%+v", restored)
	}
	var activeInbox, activeTurns int
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM inbox WHERE root_id=? AND status!='consumed'`, rootID).Scan(&activeInbox); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM turns WHERE root_id=? AND status!='succeeded'`, rootID).Scan(&activeTurns); err != nil {
		t.Fatal(err)
	}
	if activeInbox != 0 || activeTurns != 0 {
		t.Fatalf("active inbox=%d turns=%d", activeInbox, activeTurns)
	}
}

func TestClassicTurnCommitPreservesRawHistoryAcrossCompaction(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, _ := st.Create(t.TempDir(), "model", "provider")
	authority, _ := st.EnsureClassicAuthority(context.Background(), rootID)
	raw := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "a3"},
	}
	if err := st.Save(rootID, 0, raw, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordCompaction(rootID, 4, "q1 and q2 summary"); err != nil {
		t.Fatal(err)
	}
	item, _ := st.EnqueueInbox(context.Background(), InboxEnqueue{RootID: rootID, AgentID: authority.AgentID, Kind: "submit", Payload: RuntimePayload{Data: []byte("q4")}})
	if err := st.StartClassicTurn(context.Background(), rootID, authority.AgentID, item.InboxSeq); err != nil {
		t.Fatal(err)
	}
	if err := st.CommitClassicTurn(context.Background(), ClassicTurnCommit{
		RootID: rootID, AgentID: authority.AgentID, InboxSeq: item.InboxSeq,
		Messages: []llm.Message{{Role: "user", Content: "q4"}, {Role: "assistant", Content: "a4"}},
		Model:    "model", Provider: "provider",
	}); err != nil {
		t.Fatal(err)
	}
	stored := st.RawMessages(rootID)
	if len(stored) != 9 || stored[1].Content != "q1" || stored[8].Content != "a4" {
		t.Fatalf("raw history was rewritten: %+v", stored)
	}
	_, history, err := st.Load(rootID)
	if err != nil || len(history) != 7 || history[1].Role != "system" || history[6].Content != "a4" {
		t.Fatalf("compacted reconstruction=%+v err=%v", history, err)
	}
}

func TestClassicTurnCommitMapsCompactionsWithoutPersistedSystem(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, _ := st.Create(t.TempDir(), "model", "provider")
	authority, _ := st.EnsureClassicAuthority(context.Background(), rootID)
	history := []llm.Message{
		{Role: "system", Content: "system"},
		{Role: "user", Content: "q1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "q2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "q3"},
		{Role: "assistant", Content: "a3"},
	}
	if err := st.Save(rootID, 1, history, "model", "provider"); err != nil {
		t.Fatal(err)
	}
	commit := func(input, output string, cutoff, rawTail int) {
		t.Helper()
		item, err := st.EnqueueInbox(context.Background(), InboxEnqueue{RootID: rootID, AgentID: authority.AgentID, Kind: "submit", Payload: RuntimePayload{Data: []byte(input)}})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.StartClassicTurn(context.Background(), rootID, authority.AgentID, item.InboxSeq); err != nil {
			t.Fatal(err)
		}
		if err := st.CommitClassicTurn(context.Background(), ClassicTurnCommit{
			RootID: rootID, AgentID: authority.AgentID, InboxSeq: item.InboxSeq,
			Messages:    []llm.Message{{Role: "user", Content: input}, {Role: "assistant", Content: output}},
			Compactions: []ClassicCompaction{{Summary: "summary " + input, Cutoff: cutoff, RawTailStart: rawTail}},
			Model:       "model", Provider: "provider",
		}); err != nil {
			t.Fatal(err)
		}
	}
	commit("q4", "a4", 4, 1)
	commit("q5", "a5", 4, 3)
	events := st.Compactions(rootID)
	if len(events) != 2 || events[0].Cutoff != 3 || events[1].Cutoff != 4 {
		t.Fatalf("raw compaction coordinates=%+v", events)
	}
	_, restored, err := st.Load(rootID)
	if err != nil || len(restored) != 8 || restored[0].Content != "q1" || restored[2].Content != "q3" || restored[7].Content != "a5" {
		t.Fatalf("restored compaction=%+v err=%v", restored, err)
	}
}

func TestClassicTurnCommitRollsBackAsOneTransition(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, _ := st.Create(t.TempDir(), "model", "provider")
	authority, _ := st.EnsureClassicAuthority(context.Background(), rootID)
	if err := st.SetGoal(rootID, "finish the work"); err != nil {
		t.Fatal(err)
	}
	item, _ := st.EnqueueInbox(context.Background(), InboxEnqueue{RootID: rootID, AgentID: authority.AgentID, Kind: "submit", Payload: RuntimePayload{Data: []byte("work")}})
	if err := st.StartClassicTurn(context.Background(), rootID, authority.AgentID, item.InboxSeq); err != nil {
		t.Fatal(err)
	}
	want := errors.New("commit fault")
	err = st.commitClassicTurn(context.Background(), ClassicTurnCommit{
		RootID: rootID, AgentID: authority.AgentID, InboxSeq: item.InboxSeq,
		Messages: []llm.Message{{Role: "user", Content: "work"}}, GoalContinuation: "continue", Model: "model", Provider: "provider",
	}, func() error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("commit error=%v", err)
	}
	var inboxStatus, turnStatus string
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM inbox WHERE root_id=? AND agent_id=? AND seq=?`, rootID, authority.AgentID, item.InboxSeq).Scan(&inboxStatus); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM turns WHERE id=?`, classicTurnID(authority.AgentID, item.InboxSeq)).Scan(&turnStatus); err != nil {
		t.Fatal(err)
	}
	if inboxStatus != "running" || turnStatus != "running" {
		t.Fatalf("partial terminal state inbox=%q turn=%q", inboxStatus, turnStatus)
	}
	var messages int
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM messages WHERE session_id=?`, rootID).Scan(&messages); err != nil || messages != 0 {
		t.Fatalf("messages=%d err=%v", messages, err)
	}
	meta, _, err := st.Load(rootID)
	if err != nil || meta.Goal != "finish the work" {
		t.Fatalf("goal changed after rollback: %q, %v", meta.Goal, err)
	}
	var continuations int
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM inbox WHERE root_id=? AND kind='goal'`, rootID).Scan(&continuations); err != nil || continuations != 0 {
		t.Fatalf("goal continuation survived rollback: count=%d err=%v", continuations, err)
	}
}

func TestClassicTaskAndTranscriptRollBackTogether(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, _ := st.Create(t.TempDir(), "model", "provider")
	authority, _ := st.EnsureClassicAuthority(context.Background(), rootID)
	if _, err := st.db.ExecContext(context.Background(), `CREATE TRIGGER reject_task_transcript BEFORE INSERT ON messages
		WHEN NEW.session_id LIKE 'task-%' BEGIN SELECT RAISE(ABORT,'no transcript'); END`); err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "task-1", Description: "work", Prompt: "do it", Status: "done", StartedAt: time.Now(), EndedAt: time.Now()}
	if err := st.RecordClassicTaskTranscript(context.Background(), rootID, authority.AgentID, task,
		[]llm.Message{{Role: "assistant", Content: "done"}}, "model", "provider"); err == nil {
		t.Fatal("task transcript write should fail")
	}
	var tasks, transcripts int
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM tasks WHERE session_id=? AND task_id=?`, rootID, task.ID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM sessions WHERE id=?`, subagentSessionID(rootID, task.ID)).Scan(&transcripts); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 || transcripts != 0 {
		t.Fatalf("partial task persistence: tasks=%d transcripts=%d", tasks, transcripts)
	}
}

func TestScheduleFireClaimIsExactOnceAndGridAnchored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rootID, err := st.Create(t.TempDir(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	anchor := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	scheduleID, err := st.AddSchedule(rootID, "@every 10m", "check the build", anchor)
	if err != nil {
		t.Fatal(err)
	}
	bad := ScheduleFireClaim{RootID: rootID, AgentID: authority.AgentID, ScheduleID: scheduleID, Slot: anchor.Add(5 * time.Minute)}
	if _, err := st.ClaimScheduleFire(context.Background(), bad); !errors.Is(err, ErrInvalidScheduleSlot) {
		t.Fatalf("off-grid claim error = %v", err)
	}

	claim := ScheduleFireClaim{RootID: rootID, AgentID: authority.AgentID, ScheduleID: scheduleID, Slot: anchor}
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := st.ClaimScheduleFire(context.Background(), claim)
			results <- err
		}()
	}
	var succeeded, claimed int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrScheduleClaimed):
			claimed++
		default:
			t.Fatalf("schedule claim error = %v", err)
		}
	}
	if succeeded != 1 || claimed != 1 {
		t.Fatalf("schedule claims succeeded=%d already-claimed=%d", succeeded, claimed)
	}
	if got := st.Schedules(rootID); len(got) != 1 || !got[0].LastFire.Equal(anchor) {
		t.Fatalf("schedule stamp = %+v", got)
	}
	items, err := st.LoadQueuedInbox(context.Background(), rootID, authority.AgentID, 0, 10)
	if err != nil || len(items) != 1 || string(items[0].Payload.Inline) != "check the build" {
		t.Fatalf("schedule inbox = %+v, err %v", items, err)
	}

	at := anchor.Add(time.Hour)
	oneShotID, err := st.AddSchedule(rootID, "@at "+at.Format(time.RFC3339), "one shot", anchor)
	if err != nil {
		t.Fatal(err)
	}
	oneShot := ScheduleFireClaim{RootID: rootID, AgentID: authority.AgentID, ScheduleID: oneShotID, Slot: at}
	if _, err := st.ClaimScheduleFire(context.Background(), oneShot); err != nil {
		t.Fatal(err)
	}
	oneShot.ExpectedLastFire = at
	oneShot.Slot = at.Add(time.Second)
	if _, err := st.ClaimScheduleFire(context.Background(), oneShot); !errors.Is(err, ErrScheduleClaimed) {
		t.Fatalf("completed one-shot error = %v", err)
	}

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.ClaimScheduleFire(context.Background(), claim); !errors.Is(err, ErrScheduleClaimed) {
		t.Fatalf("reopened schedule replay error = %v", err)
	}
	items, err = st.LoadQueuedInbox(context.Background(), rootID, authority.AgentID, 0, 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("reopened schedule inbox count=%d err=%v", len(items), err)
	}
}

func TestFailClassicRootIsIsolatedAndPreservesTerminalRows(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	type fixture struct {
		root      string
		authority capability.ClassicAuthority
		pending   string
	}
	fixtures := make([]fixture, 2)
	for i := range fixtures {
		rootID, err := st.Create(t.TempDir(), "m", "p")
		if err != nil {
			t.Fatal(err)
		}
		authority, err := st.EnsureClassicAuthority(context.Background(), rootID)
		if err != nil {
			t.Fatal(err)
		}
		fixtures[i] = fixture{root: rootID, authority: authority}
		prefix := string(rune('a' + i))
		exec(t, st, `INSERT INTO commands(client_id,command_id,scope,root_id,request_digest,status,created_at,updated_at) VALUES(?,?, 'root',?,'d','running',?,?)`, prefix, "active", rootID, now(), now())
		exec(t, st, `INSERT INTO commands(client_id,command_id,scope,root_id,request_digest,status,created_at,updated_at) VALUES(?,?, 'root',?,'d','succeeded',?,?)`, prefix, "done", rootID, now(), now())
		for _, status := range []string{"running", "succeeded"} {
			exec(t, st, `INSERT INTO turns(id,root_id,agent_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?)`, prefix+"-turn-"+status, rootID, authority.AgentID, status, now(), now())
			exec(t, st, `INSERT INTO child_executions(id,root_id,parent_agent_id,child_agent_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, prefix+"-child-"+status, rootID, authority.AgentID, authority.AgentID, status, now(), now())
		}
		for seq, status := range []string{"queued", "running", "consumed"} {
			exec(t, st, `INSERT INTO inbox(root_id,agent_id,seq,kind,status,payload_inline,created_at) VALUES(?,?,?,?,?,?,?)`, rootID, authority.AgentID, seq+1, "test", status, []byte(status), now())
		}
		admission := capability.Admission{Request: capability.Request{
			RootID: rootID, AgentID: authority.AgentID, CapabilityID: authority.Files.ID,
			CapabilityGeneration: authority.Files.Generation, OperationID: prefix + "-operation", Operation: "read", TraceID: prefix + "-trace",
			Reservations: []capability.Reservation{{Kind: "active_operations", Amount: 1}},
		}}
		if _, err := st.Begin(context.Background(), admission); err != nil {
			t.Fatal(err)
		}
		pending := admission
		pending.Request.OperationID = prefix + "-pending-operation"
		pending.RequirePermission = true
		ticket, err := st.Begin(context.Background(), pending)
		if err != nil {
			t.Fatal(err)
		}
		fixtures[i].pending = ticket.PermissionID
		exec(t, st, `INSERT INTO operations(id,root_id,agent_id,status,created_at,updated_at) VALUES(?,?,?,'succeeded',?,?)`, prefix+"-done-operation", rootID, authority.AgentID, now(), now())
		exec(t, st, `INSERT INTO leases(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES(?,?,?,?, 'succeeded',?,?)`, prefix+"-done-lease", rootID, authority.AgentID, prefix+"-done-operation", now(), now())
	}

	root := fixtures[0]
	var eventsBefore int
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE root_id=?`, root.root).Scan(&eventsBefore); err != nil {
		t.Fatal(err)
	}
	eventSeq, err := st.FailClassicRoot(context.Background(), root.root, "actor panic")
	if err != nil || eventSeq != int64(eventsBefore+1) {
		t.Fatalf("failure event seq=%d err=%v", eventSeq, err)
	}
	if _, err := st.FailClassicRoot(context.Background(), root.root, "second panic"); !errors.Is(err, ErrRootTerminal) {
		t.Fatalf("second failure error = %v", err)
	}
	var eventsAfter int
	if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM events WHERE root_id=?`, root.root).Scan(&eventsAfter); err != nil || eventsAfter != eventsBefore+1 {
		t.Fatalf("terminal retry appended events=%d want=%d err=%v", eventsAfter, eventsBefore+1, err)
	}

	var status string
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM agents WHERE id=?`, root.authority.AgentID).Scan(&status); err != nil || status != "failed" {
		t.Fatalf("root agent status=%q err=%v", status, err)
	}
	for table, id := range map[string]string{
		"commands": "active", "turns": "a-turn-running", "child_executions": "a-child-running",
		"operations": "a-operation",
	} {
		idColumn := "id"
		if table == "commands" {
			idColumn = "command_id"
		}
		if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM `+table+` WHERE root_id=? AND `+idColumn+`=?`, root.root, id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != "interrupted" {
			t.Errorf("%s status=%q want interrupted", table, status)
		}
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM leases WHERE root_id=? AND operation_id='a-operation'`, root.root).Scan(&status); err != nil || status != "interrupted" {
		t.Errorf("active lease status=%q err=%v", status, err)
	}
	for table, id := range map[string]string{"commands": "done", "turns": "a-turn-succeeded", "child_executions": "a-child-succeeded", "operations": "a-done-operation"} {
		idColumn := "id"
		if table == "commands" {
			idColumn = "command_id"
		}
		if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM `+table+` WHERE root_id=? AND `+idColumn+`=?`, root.root, id).Scan(&status); err != nil || status != "succeeded" {
			t.Errorf("terminal %s status=%q err=%v", table, status, err)
		}
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM leases WHERE root_id=? AND id='a-done-lease'`, root.root).Scan(&status); err != nil || status != "succeeded" {
		t.Errorf("terminal lease status=%q err=%v", status, err)
	}
	for seq, want := range map[int]string{1: "interrupted", 2: "interrupted", 3: "consumed"} {
		if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM inbox WHERE root_id=? AND seq=?`, root.root, seq).Scan(&status); err != nil || status != want {
			t.Errorf("inbox %d status=%q want=%q err=%v", seq, status, want, err)
		}
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM permission_requests WHERE id=?`, root.pending).Scan(&status); err != nil || status != "interrupted" {
		t.Fatalf("pending permission status=%q err=%v", status, err)
	}
	var reserved int64
	if err := st.db.QueryRowContext(context.Background(), `SELECT reserved_value FROM budgets WHERE root_id=? AND kind='active_operations'`, root.root).Scan(&reserved); err != nil || reserved != 0 {
		t.Fatalf("failed root reservation=%d err=%v", reserved, err)
	}
	other := fixtures[1]
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM agents WHERE id=?`, other.authority.AgentID).Scan(&status); err != nil || status != "idle" {
		t.Fatalf("other root agent status=%q err=%v", status, err)
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM operations WHERE id='b-operation'`).Scan(&status); err != nil || status != "running" {
		t.Fatalf("other root operation status=%q err=%v", status, err)
	}
	if err := st.db.QueryRowContext(context.Background(), `SELECT reserved_value FROM budgets WHERE root_id=? AND kind='active_operations'`, other.root).Scan(&reserved); err != nil || reserved != 2 {
		t.Fatalf("other root reservation=%d err=%v", reserved, err)
	}
}

func TestRecoveryInterruptsEveryNonterminalRuntimeRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rootID, _ := st.Create("/workspace", "m", "p")
	exec(t, st, `INSERT INTO agents(id,root_id,parent_id,status,created_at,updated_at) VALUES('a',?,NULL,'idle',?,?)`, rootID, now(), now())
	for _, status := range []string{"queued", "running", "succeeded"} {
		suffix := status
		exec(t, st, `INSERT INTO commands(client_id,command_id,scope,root_id,request_digest,status,created_at,updated_at) VALUES('c',?,'root',?,'d',?,?,?)`, "cmd-"+suffix, rootID, status, now(), now())
		exec(t, st, `INSERT INTO turns(id,root_id,agent_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?)`, "turn-"+suffix, rootID, "a", status, now(), now())
		exec(t, st, `INSERT INTO child_executions(id,root_id,parent_agent_id,child_agent_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "child-"+suffix, rootID, "a", "a", status, now(), now())
		exec(t, st, `INSERT INTO operations(id,root_id,agent_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?)`, "op-"+suffix, rootID, "a", status, now(), now())
		exec(t, st, `INSERT INTO leases(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "lease-"+suffix, rootID, "a", "op-"+suffix, status, now(), now())
	}
	for seq, item := range []struct{ kind, status string }{
		{"command", "queued"}, {"command", "running"}, {"command", "consumed"}, {"schedule", "queued"},
	} {
		exec(t, st, `INSERT INTO inbox(root_id,agent_id,seq,kind,status,created_at) VALUES(?,?,?,?,?,?)`, rootID, "a", seq+1, item.kind, item.status, now())
	}
	exec(t, st, `INSERT INTO permission_requests(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES('permission',?,'a','op-queued','pending',?,?)`, rootID, now(), now())
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var status string
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM commands WHERE command_id='cmd-queued'`).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("ordinary open changed active command status=%q err=%v", status, err)
	}
	if err := st.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"commands", "turns", "child_executions", "operations", "leases"} {
		idColumn := "id"
		if table == "commands" {
			idColumn = "command_id"
		}
		for _, original := range []string{"queued", "running", "succeeded"} {
			var got string
			prefix := map[string]string{"commands": "cmd-", "turns": "turn-", "child_executions": "child-", "operations": "op-", "leases": "lease-"}[table]
			if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM `+table+` WHERE `+idColumn+`=?`, prefix+original).Scan(&got); err != nil {
				t.Fatal(err)
			}
			want := "interrupted"
			if original == "succeeded" {
				want = original
			}
			if got != want {
				t.Errorf("%s %s recovered as %q, want %q", table, original, got, want)
			}
		}
	}
	for seq, want := range map[int]string{1: "interrupted", 2: "interrupted", 3: "consumed", 4: "queued"} {
		var got string
		if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM inbox WHERE root_id=? AND seq=?`, rootID, seq).Scan(&got); err != nil || got != want {
			t.Errorf("recovered inbox %d status=%q want=%q err=%v", seq, got, want, err)
		}
	}
	var permission string
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM permission_requests WHERE id='permission'`).Scan(&permission); err != nil || permission != "interrupted" {
		t.Errorf("recovered permission status=%q err=%v", permission, err)
	}
	items, err := st.LoadQueuedInbox(context.Background(), rootID, "a", 0, 10)
	if err != nil || len(items) != 1 || items[0].Kind != "schedule" {
		t.Fatalf("recovery replay queue=%+v err=%v", items, err)
	}
}

func TestRecoveryReleasesReservationsAndInterruptsClassicTasks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	rootID, _ := st.Create(t.TempDir(), "model", "provider")
	authority, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	task := Task{ID: "task-1", Description: "work", Prompt: "do it", Status: "running", StartedAt: time.Now()}
	if err := st.RecordClassicTask(context.Background(), rootID, authority.AgentID, task); err != nil {
		t.Fatal(err)
	}
	admission := capability.Admission{Request: capability.Request{
		RootID: rootID, AgentID: authority.AgentID, CapabilityID: authority.Files.ID,
		CapabilityGeneration: authority.Files.Generation, OperationID: "active-operation", Operation: "read", TraceID: "trace",
		Reservations: []capability.Reservation{{Kind: "active_operations", Amount: 1}},
	}}
	if _, err := st.Begin(context.Background(), admission); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	var taskStatus, report string
	if err := st.db.QueryRowContext(context.Background(), `SELECT status,report FROM tasks WHERE session_id=? AND task_id='task-1'`, rootID).Scan(&taskStatus, &report); err != nil {
		t.Fatal(err)
	}
	if taskStatus != "error" || report != "interrupted by daemon restart" {
		t.Fatalf("recovered task=%q %q", taskStatus, report)
	}
	var operationStatus string
	if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM operations WHERE id='active-operation'`).Scan(&operationStatus); err != nil || operationStatus != "interrupted" {
		t.Fatalf("operation status=%q err=%v", operationStatus, err)
	}
	var reserved int64
	if err := st.db.QueryRowContext(context.Background(), `SELECT reserved_value FROM budgets WHERE root_id=? AND kind='active_operations'`, rootID).Scan(&reserved); err != nil || reserved != 0 {
		t.Fatalf("reserved=%d err=%v", reserved, err)
	}
}

func TestRuntimeAPIValidationMatrix(t *testing.T) {
	st, rootID, agentID := actorFailureFixture(t)
	large := bytes.Repeat([]byte("x"), InlineValueLimit+1)

	cases := map[string]func() error{
		"enqueue identity": func() error {
			_, err := st.EnqueueInbox(context.Background(), InboxEnqueue{RootID: rootID, AgentID: agentID})
			return err
		},
		"load cursor": func() error {
			_, err := st.LoadQueuedInbox(context.Background(), rootID, agentID, -1, 1)
			return err
		},
		"consume sequence": func() error {
			_, err := st.ConsumeInbox(context.Background(), rootID, agentID, 0)
			return err
		},
		"start sequence": func() error {
			return st.StartClassicTurn(context.Background(), rootID, agentID, 0)
		},
		"commit identity": func() error {
			return st.CommitClassicTurn(context.Background(), ClassicTurnCommit{})
		},
		"commit duplicate acknowledgement": func() error {
			return st.CommitClassicTurn(context.Background(), ClassicTurnCommit{RootID: rootID, AgentID: agentID, InboxSeq: 1, AcknowledgedInbox: []int64{1}})
		},
		"commit conflicting goal": func() error {
			return st.CommitClassicTurn(context.Background(), ClassicTurnCommit{RootID: rootID, AgentID: agentID, InboxSeq: 1, ClearGoal: true, GoalContinuation: "continue"})
		},
		"task identity": func() error {
			return st.RecordClassicTask(context.Background(), rootID, agentID, Task{})
		},
		"schedule identity": func() error {
			_, err := st.ClaimScheduleFire(context.Background(), ScheduleFireClaim{})
			return err
		},
		"unknown command scope": func() error {
			_, err := st.CommitRuntime(context.Background(), RuntimeTransition{Command: &RuntimeCommand{Scope: CommandScope("unknown")}})
			return err
		},
		"root command without root": func() error {
			_, err := st.CommitRuntime(context.Background(), RuntimeTransition{Command: &RuntimeCommand{Scope: CommandScopeRoot}})
			return err
		},
		"daemon command with root": func() error {
			_, err := st.CommitRuntime(context.Background(), RuntimeTransition{Command: &RuntimeCommand{Scope: CommandScopeDaemon, RootID: rootID}})
			return err
		},
		"mixed roots": func() error {
			_, err := st.CommitRuntime(context.Background(), RuntimeTransition{
				State: &RuntimeState{RootID: rootID}, Event: &RuntimeEvent{RootID: "other"},
			})
			return err
		},
		"daemon mixed with root": func() error {
			_, err := st.CommitRuntime(context.Background(), RuntimeTransition{
				Command: &RuntimeCommand{Scope: CommandScopeDaemon}, Event: &RuntimeEvent{RootID: rootID},
			})
			return err
		},
		"large daemon payload": func() error {
			_, err := st.CommitRuntime(context.Background(), RuntimeTransition{Command: &RuntimeCommand{Scope: CommandScopeDaemon, Payload: RuntimePayload{Data: large}}})
			return err
		},
		"usage unknown agent": func() error {
			_, err := st.CommitRuntime(context.Background(), RuntimeTransition{Usage: &RuntimeUsage{ID: "bad-agent", RootID: rootID, AgentID: "unknown"}})
			return err
		},
		"usage incomplete command": func() error {
			_, err := st.CommitRuntime(context.Background(), RuntimeTransition{Usage: &RuntimeUsage{ID: "bad-command-pair", RootID: rootID, CommandID: "command"}})
			return err
		},
		"usage unknown command": func() error {
			_, err := st.CommitRuntime(context.Background(), RuntimeTransition{Usage: &RuntimeUsage{ID: "bad-command", RootID: rootID, CommandClientID: "client", CommandID: "command"}})
			return err
		},
		"nonterminal command outcome": func() error {
			_, err := st.FinishCommand(context.Background(), "client", "command", "running", RuntimePayload{})
			return err
		},
		"content without root": func() error {
			_, err := st.StoreContent(context.Background(), ContentGrant{Scope: ContentGrantRoot}, RuntimePayload{})
			return err
		},
		"root content naming agent": func() error {
			_, err := st.StoreContent(context.Background(), ContentGrant{RootID: rootID, AgentID: agentID, Scope: ContentGrantRoot}, RuntimePayload{})
			return err
		},
		"invalid content scope": func() error {
			_, err := st.StoreContent(context.Background(), ContentGrant{RootID: rootID, Scope: ContentGrantScope("invalid")}, RuntimePayload{})
			return err
		},
		"unknown content agent": func() error {
			_, err := st.StoreContent(context.Background(), ContentGrant{RootID: rootID, AgentID: "unknown", Scope: ContentGrantAgent}, RuntimePayload{})
			return err
		},
		"unknown content read": func() error {
			_, _, err := st.ReadContent(context.Background(), "missing", rootID, agentID, 0, 1)
			return err
		},
		"unknown content revocation": func() error {
			return st.RevokeContentGrant(context.Background(), "missing", rootID, agentID)
		},
		"missing failure root": func() error {
			_, err := st.FailClassicRoot(context.Background(), "", "reason")
			return err
		},
		"missing interruption root": func() error {
			_, err := st.InterruptClassicRoot(context.Background(), "", "reason")
			return err
		},
		"missing stop root": func() error {
			_, err := st.StopClassicRoot(context.Background(), "", "reason")
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			if err := call(); err == nil {
				t.Fatal("invalid request succeeded")
			}
		})
	}
}

func TestClassicRootInterruptAndStop(t *testing.T) {
	for _, test := range []struct {
		name             string
		stop             bool
		wantAgent        string
		wantScheduleItem string
	}{
		{name: "interrupt", wantAgent: "idle", wantScheduleItem: "queued"},
		{name: "stop", stop: true, wantAgent: "stopped", wantScheduleItem: "interrupted"},
	} {
		t.Run(test.name, func(t *testing.T) {
			st, rootID, agentID := actorFailureFixture(t)
			for _, kind := range []string{"command", "schedule"} {
				if _, err := st.EnqueueInbox(context.Background(), InboxEnqueue{RootID: rootID, AgentID: agentID, Kind: kind}); err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if test.stop {
				_, err = st.StopClassicRoot(context.Background(), rootID, "shutdown")
			} else {
				_, err = st.InterruptClassicRoot(context.Background(), rootID, "restart")
			}
			if err != nil {
				t.Fatal(err)
			}
			var agentStatus, commandStatus, scheduleStatus string
			if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM agents WHERE id=?`, agentID).Scan(&agentStatus); err != nil {
				t.Fatal(err)
			}
			if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM inbox WHERE root_id=? AND kind='command'`, rootID).Scan(&commandStatus); err != nil {
				t.Fatal(err)
			}
			if err := st.db.QueryRowContext(context.Background(), `SELECT status FROM inbox WHERE root_id=? AND kind='schedule'`, rootID).Scan(&scheduleStatus); err != nil {
				t.Fatal(err)
			}
			if agentStatus != test.wantAgent || commandStatus != "interrupted" || scheduleStatus != test.wantScheduleItem {
				t.Fatalf("statuses agent=%q command=%q schedule=%q", agentStatus, commandStatus, scheduleStatus)
			}
		})
	}
}
