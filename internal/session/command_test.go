package session

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

func TestCommandAdmissionIsIdempotentAndBoundToOneInboxSequence(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	admission := CommandAdmission{
		ClientID: "client", CommandID: "command", Scope: CommandScopeRoot,
		RootID: rootID, AgentID: authority.AgentID, Kind: "submit", RequestDigest: "digest",
		Payload: RuntimePayload{Data: []byte("do the work"), MediaType: "text/plain"},
	}

	const callers = 16
	results := make(chan CommandAdmissionResult, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Go(func() {
			result, err := st.AdmitCommand(context.Background(), admission)
			results <- result
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	inserted := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result.New {
			inserted++
		}
		if result.Command.IngressSeq != 1 || result.Command.Status != "queued" {
			t.Fatalf("admitted command = %+v", result.Command)
		}
	}
	if inserted != 1 {
		t.Fatalf("new admissions = %d, want 1", inserted)
	}
	for table := range map[string]struct{}{"commands": {}, "inbox": {}} {
		var count int
		if err := st.db.QueryRowContext(context.Background(), `SELECT count(*) FROM `+table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s rows = %d, err %v", table, count, err)
		}
	}
	conflict := admission
	conflict.RequestDigest = "different"
	if _, err := st.AdmitCommand(context.Background(), conflict); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("conflicting reuse error = %v", err)
	}
}

func TestClassicTurnCommitsProtocolCommandOutcome(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureClassicAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := st.AdmitCommand(context.Background(), CommandAdmission{
		ClientID: "client", CommandID: "command", Scope: CommandScopeRoot,
		RootID: rootID, AgentID: authority.AgentID, Kind: "submit", RequestDigest: "digest",
		Payload: RuntimePayload{Data: []byte("prompt")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.StartClassicTurn(context.Background(), rootID, authority.AgentID, result.Command.IngressSeq); err != nil {
		t.Fatal(err)
	}
	if record, err := st.LoadCommand(context.Background(), "client", "command"); err != nil || record.Status != "running" {
		t.Fatalf("running command = %+v, %v", record, err)
	}
	outcome := bytes.Repeat([]byte("result"), 2048)
	if err := st.CommitClassicTurn(context.Background(), ClassicTurnCommit{
		RootID: rootID, AgentID: authority.AgentID, InboxSeq: result.Command.IngressSeq,
		Model: "model", Provider: "provider", Outcome: RuntimePayload{Data: outcome, Source: "outcome"},
	}); err != nil {
		t.Fatal(err)
	}
	record, err := st.LoadCommand(context.Background(), "client", "command")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "succeeded" || record.Outcome.ReferenceID == "" || record.Outcome.Size != int64(len(outcome)) {
		t.Fatalf("terminal command = %+v", record)
	}
	got, _, err := st.ReadContent(context.Background(), record.Outcome.ReferenceID, rootID, authority.AgentID, 0, MaxContentRead)
	if err != nil || !bytes.Equal(got, outcome) {
		t.Fatalf("outcome read = %d bytes, %v", len(got), err)
	}
}

func TestDaemonCommandAdmissionHasIndependentSequence(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	for i, id := range []string{"one", "two"} {
		result, err := st.AdmitCommand(context.Background(), CommandAdmission{
			ClientID: "client", CommandID: id, Scope: CommandScopeDaemon,
			RequestDigest: id, Kind: "ping", Payload: RuntimePayload{Data: []byte(id)},
		})
		if err != nil {
			t.Fatal(err)
		}
		if !result.New || result.Command.IngressSeq != int64(i+1) {
			t.Fatalf("daemon admission %d = %+v", i, result)
		}
	}
}
