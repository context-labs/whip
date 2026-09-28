package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func forkDefaultsTest() session.ForkDefaults {
	return session.ForkDefaults{
		Resources: []session.ResourceLimit{{Kind: session.ResourceDescendants, Limit: new(int64(7))}},
		Budgets: []session.BudgetLimit{
			{Kind: session.BudgetLogicalWrites, Limit: new(int64(1000))},
			{Kind: session.BudgetLogicalWriteBytes, Limit: new(int64(1 << 20))},
			{Kind: session.BudgetModelCalls, Limit: new(int64(20))},
		},
	}
}

func forkRequestTest(t *testing.T, s *Store, owner session.SessionID, id session.ForkID, keep int64) session.ForkRequest {
	t.Helper()
	snapshot, err := s.HistorySnapshot(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.Session(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	return session.ForkRequest{ID: id, SessionID: owner, ExpectedHistoryRevision: snapshot.Revision, ExpectedConfigRevision: source.ConfigRevision, ObservedThrough: snapshot.ThroughSequence, KeepThrough: keep}
}

func forkTest(t *testing.T, s *Store, request session.ForkRequest) session.ForkResult {
	t.Helper()
	result, err := s.Fork(t.Context(), request, forkDefaultsTest())
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted || result.Root == nil || result.Tree == nil {
		t.Fatal("new fork has no destination", result)
	}
	return result
}

func TestForkCopiesHistoryConfigurationWithoutAuthorityOrSpending(t *testing.T) {
	s := fresh(t)
	_, source := create(t, s, nil)
	history := compactionHistoryTest(t, s, source.ID, "source")
	child := controlChild(t, s, source.ID, "child").Session
	if child == nil {
		t.Fatal("missing child")
	}
	if _, err := s.UpdateConfiguration(t.Context(), source.ID, source.ConfigRevision, session.ConfigPatch{Model: &session.ModelSelection{Provider: "copied", Name: "effective"}}); err != nil {
		t.Fatal(err)
	}
	createGoalTest(t, s, source.ID, "source_goal", nil, false)
	standingGrant(t, s, source.ID, "source_grant")
	makeSchedule(t, s, source.ID, "source_schedule", "@at 2099-01-01T00:00:00Z")
	private := putStateTest(t, s, stateWrite(source.ID, session.SessionState, "source_private", "key", `"private"`, 0))
	shared := putStateTest(t, s, stateWrite(source.ID, session.TreeState, "source_shared", "key", `"shared"`, 0))
	source, err := s.Session(t.Context(), source.ID)
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{"inputs", "turns", "model_attempts", "logical_writes", "logical_write_ancestors", "attempt_budget_ancestors", "cells", "operations", "grants", "permissions", "goals", "schedules", "mail", "mail_revisions", "turn_mail_observations", "state_versions", "state_subscriptions", "receipts"}
	before := map[string]int{}
	for _, table := range tables {
		before[table] = count(t, s, table)
	}
	request := forkRequestTest(t, s, source.ID, "copy", 4)
	request.Title = new("Copied task")
	result := forkTest(t, s, request)
	if result.Root.ID == source.ID || result.Tree.ID == source.TreeID || result.Root.ParentID != nil || result.Root.Definition != source.Definition || result.Root.WorkingDirectory != source.WorkingDirectory || !reflect.DeepEqual(result.Root.Config, source.Config) || result.Root.ConfigRevision != 1 || result.Root.HistoryRevision != 1 || result.Root.Lifecycle != session.Active {
		t.Fatal("fork did not create an independent root with captured effective config", result.Root, source)
	}
	if result.Tree.Engine != session.Starlark || result.Tree.Metadata.Title == nil || *result.Tree.Metadata.Title != *request.Title || result.Tree.Metadata.Archived || result.Tree.Metadata.Pinned {
		t.Fatal(result.Tree)
	}
	copied, err := s.History(t.Context(), result.Root.ID, 0, 100)
	if err != nil || len(copied) != len(history) {
		t.Fatal(copied, err)
	}
	for i, message := range copied {
		if message.ID == history[i].ID || message.GroupID == history[i].GroupID || message.Source == nil || message.Source.SessionID != source.ID || message.Source.MessageID != history[i].ID || message.Source.Sequence != history[i].Sequence || message.TurnID != "" || message.InputID != nil || message.Mail != nil || message.OpeningInput != history[i].OpeningInput || !reflect.DeepEqual(message.Parts, history[i].Parts) {
			t.Fatal("copy lost immutable evidence or invented execution", message)
		}
	}
	group, err := s.HistoryGroup(t.Context(), result.Root.ID, copied[0].GroupID)
	if err != nil || group.TurnID != nil || group.Source == nil || group.Source.SessionID != source.ID || group.Source.GroupID != history[0].GroupID {
		t.Fatal(group, err)
	}
	for _, table := range tables {
		if count(t, s, table) != before[table] {
			t.Fatalf("fork copied %s", table)
		}
	}
	resources, err := s.Resources(t.Context(), result.Root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, resource := range resources {
		if resource.Used != 0 {
			t.Fatal("fork inherited resource use", resource)
		}
		if resource.Kind == session.ResourceDescendants && (resource.Limit == nil || *resource.Limit != 7) {
			t.Fatal("fork ignored injected resources", resource)
		}
	}
	budgets, err := s.Budgets(t.Context(), result.Root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range budgets {
		if budget.Used != 0 || budget.Reserved != 0 || budget.Uncertain != 0 || budget.Incomplete {
			t.Fatal("fork inherited spending", budget)
		}
		if budget.Kind == session.BudgetModelCalls && (budget.Limit == nil || *budget.Limit != 20) {
			t.Fatal("fork ignored injected budget", budget)
		}
	}
	if count(t, s, "sessions") != 3 {
		t.Fatal("fork cloned children")
	}
	for _, state := range []session.StateValue{private, shared} {
		if _, err := s.StateValue(t.Context(), result.Root.ID, state.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("fork inherited state access", err)
		}
	}
	if grants, err := s.Grants(t.Context(), result.Root.ID, "", 100); err != nil || len(grants) != 0 {
		t.Fatal("fork inherited authority", grants, err)
	}
	// A fork of imported history adds its own provenance edge, without fake turns.
	second := forkTest(t, s, forkRequestTest(t, s, result.Root.ID, "copy_again", 4))
	secondHistory, err := s.History(t.Context(), second.Root.ID, 0, 100)
	if err != nil || secondHistory[0].Source.SessionID != result.Root.ID || secondHistory[0].Source.MessageID != copied[0].ID || !reflect.DeepEqual(secondHistory[0].Parts, history[0].Parts) {
		t.Fatal(secondHistory, err)
	}
}

func TestForkReceiptRetriesRollbackAndDeletionTombstone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, source := create(t, s, nil)
	compactionHistoryTest(t, s, source.ID, "source")
	request := forkRequestTest(t, s, source.ID, "stable", 4)
	before := map[string]int{}
	for _, table := range []string{"session_trees", "sessions", "session_configurations", "resource_limits", "budget_limits", "messages", "history_groups", "forks"} {
		before[table] = count(t, s, table)
	}
	for _, table := range []string{"messages", "forks"} {
		execTest(t, s, fmt.Sprintf("CREATE TRIGGER injected_fork BEFORE INSERT ON %s BEGIN SELECT RAISE(ABORT,'injected'); END", table))
		if result, err := s.Fork(t.Context(), request, forkDefaultsTest()); err == nil || result.Root != nil {
			t.Fatal("failed write returned admitted fork", result, err)
		}
		for name, n := range before {
			if count(t, s, name) != n {
				t.Fatalf("partial fork persisted %s", name)
			}
		}
		execTest(t, s, "DROP TRIGGER injected_fork")
	}
	first := forkTest(t, s, request)
	// Retry lookup precedes invalid current defaults and mutable source checks.
	if _, err := s.UpdateConfiguration(t.Context(), source.ID, 1, session.ConfigPatch{Model: &session.ModelSelection{Provider: "changed", Name: "later"}}); err != nil {
		t.Fatal(err)
	}
	retry, err := s.Fork(t.Context(), request, session.ForkDefaults{})
	if err != nil || !reflect.DeepEqual(first, retry) {
		t.Fatal("exact retry changed", retry, err)
	}
	changed := request
	changed.Title = new("changed request")
	if _, err := s.Fork(t.Context(), changed, forkDefaultsTest()); !errors.Is(err, ErrConflict) {
		t.Fatal("identity reused", err)
	}
	if err := s.DeleteSubtree(t.Context(), source.ID); err != nil {
		t.Fatal(err)
	}
	if retry, err = s.Fork(t.Context(), request, session.ForkDefaults{}); err != nil || !reflect.DeepEqual(first, retry) {
		t.Fatal("source deletion broke retry", err)
	}
	if err := s.DeleteSubtree(t.Context(), first.Root.ID); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	retry, err = s.Fork(t.Context(), request, session.ForkDefaults{})
	if err != nil || !retry.Deleted || retry.Root != nil || retry.Tree != nil || !reflect.DeepEqual(retry.Fork, first.Fork) || count(t, s, "sessions") != 0 {
		t.Fatal("deleted destination was recreated", retry, err)
	}
	if _, err := s.Fork(t.Context(), changed, session.ForkDefaults{}); !errors.Is(err, ErrConflict) {
		t.Fatal("tombstone identity reused", err)
	}
	mustFail(t, s, "UPDATE forks SET keep_through=0 WHERE id=?", request.ID)
	mustFail(t, s, "DELETE FROM forks WHERE id=?", request.ID)
}

func TestForkExactBoundaryRejectsStaleSplitAndActiveIncludedGroups(t *testing.T) {
	s := fresh(t)
	_, source := create(t, s, nil)
	compactionHistoryTest(t, s, source.ID, "first")
	initial := forkRequestTest(t, s, source.ID, "boundary", 4)
	for _, change := range []func(*session.ForkRequest){
		func(r *session.ForkRequest) { r.ExpectedHistoryRevision++ },
		func(r *session.ForkRequest) { r.ExpectedConfigRevision++ },
		func(r *session.ForkRequest) { r.ObservedThrough++ },
	} {
		request := initial
		change(&request)
		if _, err := s.Fork(t.Context(), request, forkDefaultsTest()); !errors.Is(err, ErrConflict) {
			t.Fatal("stale snapshot admitted", err)
		}
	}
	for _, keep := range []int64{1, 2, 3} {
		request := initial
		request.KeepThrough = keep
		if _, err := s.Fork(t.Context(), request, forkDefaultsTest()); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("partial group admitted", keep, err)
		}
	}
	submit(t, s, source.ID, "later")
	later := claim(t, s, source.ID).Turn
	if _, err := s.Fork(t.Context(), initial, forkDefaultsTest()); !errors.Is(err, ErrConflict) {
		t.Fatal("concurrent append ignored", err)
	}
	active := forkRequestTest(t, s, source.ID, "active", 5)
	if _, err := s.Fork(t.Context(), active, forkDefaultsTest()); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("active group admitted", err)
	}
	active.KeepThrough = 4
	forkTest(t, s, active)
	// Empty history remains valid under the same explicit source snapshot.
	empty := active
	empty.ID = "empty"
	empty.KeepThrough = 0
	zero := forkTest(t, s, empty)
	if history, err := s.History(t.Context(), zero.Root.ID, 0, 100); err != nil || len(history) != 0 {
		t.Fatal(history, err)
	}
	finishMailTest(t, s, later.ID, session.Succeeded)
	// Imported groups may interleave; a terminal-looking boundary cannot split
	// another group whose opening is earlier in the selected prefix.
	_, imported := create(t, s, nil)
	for _, group := range []string{"a", "b"} {
		execTest(t, s, `INSERT INTO history_groups(id,session_id,source_session_id,source_group_id,created_at) VALUES(?,?,'gone',?,?)`, group, imported.ID, group, now())
	}
	for i, group := range []string{"a", "b", "a"} {
		execTest(t, s, `INSERT INTO messages(id,session_id,group_id,sequence,role,parts,created_at,source_session_id,source_message_id,source_sequence) VALUES(?,?,?,?,'assistant','[]',?,'gone',?,?)`, fmt.Sprintf("interleaved_%d", i), imported.ID, group, i+1, now(), fmt.Sprintf("old_%d", i), i+1)
	}
	request := forkRequestTest(t, s, imported.ID, "interleaved", 2)
	if _, err := s.Fork(t.Context(), request, forkDefaultsTest()); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("interleaved group split", err)
	}
	request.KeepThrough = 3
	forkTest(t, s, request)
}

func TestForkOpaqueContentPrivateContinuationAndMailIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, source := create(t, s, nil)
	_, outsider := create(t, s, nil)
	ref, err := s.RegisterContent(t.Context(), contentReference(source.ID, "opaque_handle", "source bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterContent(t.Context(), contentReference(outsider.ID, ref.ID, "other bytes")); err != nil {
		t.Fatal(err)
	}
	submit(t, s, source.ID, "opaque_handle")
	turn := claim(t, s, source.ID).Turn
	continuation := &session.ModelContinuation{Scope: strings.Repeat("a", 64), Data: `[{"type":"reasoning","encrypted_content":"private-marker"}]`}
	draft := session.MessageDraft{ID: "visible", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "Use opaque_handle without rewriting it"}, {Type: "content", ReferenceID: ref.ID}}, Continuation: continuation}
	if _, err := s.AppendMessage(t.Context(), turn.ID, draft); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, turn.ID, session.Succeeded)
	sendMailTest(t, s, session.MailSpec{ID: "incoming", SenderID: source.ID, RecipientID: source.ID, Delivery: session.MailQueued, Body: "mail evidence with opaque_handle"})
	mailTurn := claim(t, s, source.ID).Turn
	finishMailTest(t, s, mailTurn.ID, session.Succeeded)
	history, err := s.History(t.Context(), source.ID, 0, 100)
	if err != nil || len(history) != 3 || history[2].Mail == nil {
		t.Fatal(history, err)
	}
	mailCount, observations := count(t, s, "mail"), count(t, s, "turn_mail_observations")
	result := forkTest(t, s, forkRequestTest(t, s, source.ID, "opaque_copy", 3))
	copied, err := s.History(t.Context(), result.Root.ID, 0, 100)
	if err != nil || !reflect.DeepEqual(copied[1].Parts, draft.Parts) || !reflect.DeepEqual(copied[2].Parts, history[2].Parts) || copied[2].Mail != nil || count(t, s, "mail") != mailCount || count(t, s, "turn_mail_observations") != observations {
		t.Fatal("copied evidence mutated mail or text", copied, err)
	}
	if count(t, s, "content_bodies") != 2 {
		t.Fatal("fork duplicated body metadata")
	}
	if err := s.DeleteSubtree(t.Context(), source.ID); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	got, err := s.ContentReference(t.Context(), result.Root.ID, ref.ID)
	if err != nil || got.Digest != ref.Digest {
		t.Fatal("opaque content lost ownership", got, err)
	}
	other, err := s.ContentReference(t.Context(), outsider.ID, ref.ID)
	if err != nil || other.Digest == got.Digest {
		t.Fatal("same opaque id changed outsider access", other, err)
	}
	private, err := s.Continuations(t.Context(), result.Root.ID, []session.MessageID{copied[1].ID})
	if err != nil || private[copied[1].ID] != *continuation {
		t.Fatal("private evidence lost immutable scope", private, err)
	}
	if _, err := s.Continuations(t.Context(), outsider.ID, []session.MessageID{copied[1].ID}); !errors.Is(err, ErrNotFound) {
		t.Fatal("private continuation escaped owner", err)
	}
	public, err := json.Marshal(copied)
	if err != nil || strings.Contains(string(public), "private-marker") {
		t.Fatal("private state leaked in transcript", err)
	}
	if _, err := s.ContentReference(t.Context(), result.Root.ID, ref.Digest); !errors.Is(err, ErrNotFound) {
		t.Fatal("digest granted access", err)
	}
}

func TestForkConcurrentIdentitiesCommitOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	other := openTest(t, path)
	_, source := create(t, s, nil)
	compactionHistoryTest(t, s, source.ID, "source")
	request := forkRequestTest(t, s, source.ID, "concurrent", 4)
	var workers sync.WaitGroup
	results := make(chan session.ForkResult, 2)
	failures := make(chan error, 2)
	for _, db := range []*Store{s, other} {
		workers.Go(func() {
			result, err := db.Fork(t.Context(), request, forkDefaultsTest())
			results <- result
			failures <- err
		})
	}
	workers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	var root session.SessionID
	for result := range results {
		if root != "" && result.Fork.RootID != root {
			t.Fatal("duplicate destination")
		}
		root = result.Fork.RootID
	}
	if count(t, s, "forks") != 1 || count(t, s, "sessions") != 2 {
		t.Fatal("duplicate admission")
	}
}

func TestForkToolExchangeDoesNotCopyOperationsPermissionsOrCheckpoint(t *testing.T) {
	s := fresh(t)
	source, cell := operationCell(t, s)
	op := admitOperation(t, s, operationSpec(cell, "source_operation"))
	if _, err := s.ResolvePermission(t.Context(), op.ID, false); err != nil {
		t.Fatal(err)
	}
	checkpoint := &session.Checkpoint{Digest: strings.Repeat("a", 64), Size: 17, Engine: session.Starlark, Metadata: json.RawMessage(`{}`)}
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: cell.CallID, Output: "denied operation; local cell result"}, checkpoint); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, cell.TurnID, session.Succeeded)
	history, err := s.History(t.Context(), source.ID, 0, 100)
	if err != nil || len(history) != 3 {
		t.Fatal(history, err)
	}
	result := forkTest(t, s, forkRequestTest(t, s, source.ID, "tool_copy", 3))
	copied, err := s.History(t.Context(), result.Root.ID, 0, 100)
	if err != nil || len(copied) != 3 || !reflect.DeepEqual(copied[1].Parts, history[1].Parts) || !reflect.DeepEqual(copied[2].Parts, history[2].Parts) || copied[1].GroupID != copied[2].GroupID {
		t.Fatal("tool exchange changed", copied, err)
	}
	if latest, err := s.LatestCell(t.Context(), result.Root.ID); err != nil || latest != nil {
		t.Fatal("fork restored execution checkpoint", latest, err)
	}
	if count(t, s, "cells") != 1 || count(t, s, "operations") != 1 || count(t, s, "permissions") != 1 || count(t, s, "turns") != 1 {
		t.Fatal("fork fabricated execution or permission rows")
	}
}
