package session

import (
	"bytes"
	"context"
	"database/sql"
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
	rootID, err := st.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureAuthority(context.Background(), rootID)
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

func TestRootTurnCommitsProtocolCommandOutcome(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootID, err := st.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureAuthority(context.Background(), rootID)
	if err != nil {
		t.Fatal(err)
	}
	control, err := st.AdmitControlCommand(context.Background(), CommandAdmission{
		ClientID: "client", CommandID: "control", Scope: CommandScopeRoot,
		RootID: rootID, AgentID: authority.AgentID, Kind: "goal.set", RequestDigest: "control-digest",
	})
	if err != nil || control.Command.IngressSeq >= 0 {
		t.Fatalf("control command = %+v, %v", control, err)
	}
	result, err := st.AdmitCommand(context.Background(), CommandAdmission{
		ClientID: "client", CommandID: "command", Scope: CommandScopeRoot,
		RootID: rootID, AgentID: authority.AgentID, Kind: "submit", RequestDigest: "digest",
		Payload: RuntimePayload{Data: []byte("prompt")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.StartRootTurn(context.Background(), rootID, authority.AgentID, result.Command.IngressSeq); err != nil {
		t.Fatal(err)
	}
	if record, err := st.LoadCommand(context.Background(), "client", "command"); err != nil || record.Status != "running" {
		t.Fatalf("running command = %+v, %v", record, err)
	}
	outcome := bytes.Repeat([]byte("result"), 2048)
	if err := st.CommitRootTurn(context.Background(), RootTurnCommit{
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
	controlRecord, err := st.LoadCommand(context.Background(), "client", "control")
	if err != nil || controlRecord.Status != "queued" || len(controlRecord.Outcome.Inline) != 0 || controlRecord.Outcome.ReferenceID != "" {
		t.Fatalf("turn commit changed control command = %+v, %v", controlRecord, err)
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

func TestCommandAdmissionRejectsInvalidScopesAndMissingRecords(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	for _, admission := range []CommandAdmission{
		{},
		{ClientID: "c", CommandID: "id", RequestDigest: "d", Scope: CommandScopeRoot},
		{ClientID: "c", CommandID: "id", RequestDigest: "d", Scope: CommandScopeDaemon, RootID: "root"},
		{ClientID: "c", CommandID: "id", RequestDigest: "d", Scope: "invalid"},
		{ClientID: "c", CommandID: "id", RequestDigest: "d", Scope: CommandScopeDaemon, Payload: RuntimePayload{Data: make([]byte, InlineValueLimit+1)}},
	} {
		if _, err := st.AdmitCommand(ctx, admission); err == nil {
			t.Fatalf("invalid admission was accepted: %+v", admission)
		}
	}
	if _, err := st.LoadCommand(ctx, "missing", "missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing command = %v", err)
	}
	if _, err := st.CreateSessionForCommand(ctx, "c", "id", SessionKindAgent, "", "", ""); err == nil {
		t.Fatal("incomplete session creation was accepted")
	}
	if _, err := st.CreateSessionForCommand(ctx, "c", "id", SessionKindAgent, "/tmp", "m", "p"); err == nil {
		t.Fatal("unadmitted session command was executed")
	}
	rootID, err := st.Create(SessionKindAgent, t.TempDir(), "m", "p")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := st.EnsureAuthority(ctx, rootID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AdmitControlCommand(ctx, CommandAdmission{
		ClientID: "control", CommandID: "wrong-scope", Scope: CommandScopeDaemon, RequestDigest: "wrong",
	}); err == nil {
		t.Fatal("daemon-scoped control command was accepted")
	}
	control := CommandAdmission{
		ClientID: "control", CommandID: "valid", Scope: CommandScopeRoot, RootID: rootID,
		AgentID: authority.AgentID, Kind: "goal.set", RequestDigest: "control-digest",
	}
	createdControl, err := st.AdmitControlCommand(ctx, control)
	if err != nil || !createdControl.New || createdControl.Command.IngressSeq >= 0 {
		t.Fatalf("control admission = %+v, %v", createdControl, err)
	}
	if retry, err := st.AdmitControlCommand(ctx, control); err != nil || retry.New || retry.Command.IngressSeq != createdControl.Command.IngressSeq {
		t.Fatalf("control retry = %+v, %v", retry, err)
	}
	control.RequestDigest = "conflict"
	if _, err := st.AdmitControlCommand(ctx, control); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("control conflict = %v", err)
	}
	if _, err := st.AdmitCommand(ctx, CommandAdmission{
		ClientID: "root-client", CommandID: "root", Scope: CommandScopeRoot, RootID: rootID,
		AgentID: authority.AgentID, Kind: "submit", RequestDigest: "root", Payload: RuntimePayload{Data: []byte("work")},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSessionForCommand(ctx, "root-client", "root", SessionKindAgent, "/tmp", "m", "p"); err == nil {
		t.Fatal("root command created a session")
	}
	if _, err := st.AdmitCommand(ctx, CommandAdmission{
		ClientID: "daemon", CommandID: "create", Scope: CommandScopeDaemon, RequestDigest: "create",
	}); err != nil {
		t.Fatal(err)
	}
	created, err := st.CreateSessionForCommand(ctx, "daemon", "create", SessionKindAgent, "/tmp", "m", "p")
	if err != nil || created.Status != "succeeded" {
		t.Fatalf("create session command = %+v, %v", created, err)
	}
	retry, err := st.CreateSessionForCommand(ctx, "daemon", "create", SessionKindAgent, "/tmp", "m", "p")
	if err != nil || string(retry.Outcome.Inline) != string(created.Outcome.Inline) {
		t.Fatalf("create session retry = %+v, %v", retry, err)
	}
}

func TestCommandAPIsReturnClosedStoreErrors(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := st.AdmitCommand(ctx, CommandAdmission{ClientID: "c", CommandID: "id", RequestDigest: "d", Scope: CommandScopeDaemon}); err == nil {
		t.Fatal("closed store admitted command")
	}
	if _, err := st.AdmitControlCommand(ctx, CommandAdmission{
		ClientID: "c", CommandID: "control", RequestDigest: "d", Scope: CommandScopeRoot,
		RootID: "root", AgentID: "agent", Kind: "goal.set",
	}); err == nil {
		t.Fatal("closed store admitted control command")
	}
	if _, err := st.LoadCommand(ctx, "c", "id"); err == nil {
		t.Fatal("closed store loaded command")
	}
	if _, err := st.CreateSessionForCommand(ctx, "c", "id", SessionKindAgent, "/tmp", "m", "p"); err == nil {
		t.Fatal("closed store created session")
	}
}
