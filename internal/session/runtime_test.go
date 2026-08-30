package session

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
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
			if err := reader.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
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
	if err := st.db.QueryRow(`SELECT count(*) FROM content_objects`).Scan(&bodies); err != nil || bodies != 1 {
		t.Fatalf("content objects = %d, err %v", bodies, err)
	}
	for _, table := range []string{"agents", "commands", "inbox", "agent_state", "events", "usage_charges"} {
		var n int
		if err := st.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("%s rows = %d, err %v", table, n, err)
		}
	}
	for _, table := range []string{"commands", "inbox", "agent_state", "events"} {
		var inline, refs int
		if err := st.db.QueryRow(`SELECT COALESCE(MAX(length(payload_inline)),0), count(payload_ref) FROM `+table).Scan(&inline, &refs); err != nil {
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
	if err := st.db.QueryRow(`SELECT status FROM commands WHERE client_id='client-1' AND command_id='command-1'`).Scan(&status); err != nil || status != "succeeded" {
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
	if _, err := st.db.Exec(`INSERT INTO blackboard(root_id,key,version,author_agent_id,updated_at) VALUES(?,?,?,?,?)`, root1, "invalid", 1, "other-root", now()); err == nil {
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
	if _, err := st.db.Exec(`INSERT INTO leases(id,root_id,agent_id,operation_id,status,created_at,updated_at) VALUES('cross-root-lease',?,'parent','other-operation','running',?,?)`, root1, now(), now()); err == nil {
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
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var status string
	if err := st.db.QueryRow(`SELECT status FROM commands WHERE command_id='cmd-queued'`).Scan(&status); err != nil || status != "queued" {
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
			if err := st.db.QueryRow(`SELECT status FROM `+table+` WHERE `+idColumn+`=?`, prefix+original).Scan(&got); err != nil {
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
}
