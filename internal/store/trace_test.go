package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func traceQuery(root session.SessionID) session.TraceQuery {
	return session.TraceQuery{RootID: root, Limit: 2048, MaxBytes: 512 << 10}
}

func readTrace(t *testing.T, s *Store, root session.SessionID) session.TracePage {
	t.Helper()
	page, err := s.TracePage(t.Context(), traceQuery(root))
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func traceRow(t *testing.T, page session.TracePage, kind, id string) session.TraceRow {
	t.Helper()
	for _, row := range page.Items {
		if row.SourceKind == kind && row.SourceID == id {
			return row
		}
	}
	t.Fatalf("missing %s/%s in %+v", kind, id, page)
	return session.TraceRow{}
}

func TestTraceIndexAtomicSettlementPagingAndQuestionUpdate(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := admitOperation(t, s, operationSpec(cell, "read"))
	initial := readTrace(t, s, owner.ID)
	if len(initial.Items) != 4 || initial.Next != initial.Revision || initial.HasMore {
		t.Fatal(initial)
	}
	wait := traceRow(t, initial, "permission", string(op.ID))
	host := traceRow(t, initial, "operation", string(op.ID))
	if wait.Span.ParentSpanID == nil || *wait.Span.ParentSpanID != host.SpanID || host.Span.ParentSpanID == nil || *host.Span.ParentSpanID != session.TraceSpanID("cell", string(cell.ID)) {
		t.Fatal(wait, host)
	}
	query := traceQuery(owner.ID)
	query.Limit = 1
	first, err := s.TracePage(t.Context(), query)
	if err != nil || !first.HasMore {
		t.Fatal(first, err)
	}
	query.After = first.Next
	query.ExpectedRevision = &first.Revision
	execTest(t, s, `CREATE TRIGGER reject_trace_decision BEFORE UPDATE ON permissions BEGIN SELECT RAISE(ABORT,'rollback'); END`)
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err == nil {
		t.Fatal("injected failure ignored")
	}
	unchanged := readTrace(t, s, owner.ID)
	unchanged.ObservedAtNS = initial.ObservedAtNS
	if !reflect.DeepEqual(unchanged, initial) {
		t.Fatal("trace index escaped rollback", unchanged, initial)
	}
	execTest(t, s, "DROP TRIGGER reject_trace_decision")
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TracePage(t.Context(), query); !errors.Is(err, ErrConflict) {
		t.Fatal("missed paging conflict", err)
	}
	incremental := traceQuery(owner.ID)
	incremental.After = initial.Revision
	changes, err := s.TracePage(t.Context(), incremental)
	if err != nil || len(changes.Items) != 2 {
		t.Fatal(changes, err)
	}
	if approved := traceRow(t, changes, "permission", string(op.ID)); approved.Span.State != "approved" || approved.Span.EndNS == nil {
		t.Fatal(approved)
	}
	beginQuestion(t, s, cell, "ask", false)
	before := readTrace(t, s, owner.ID)
	if _, err := s.AnswerQuestion(t.Context(), owner.ID, "ask", []session.QuestionAnswer{{Answer: []string{"A"}}}); err != nil {
		t.Fatal(err)
	}
	incremental.After = before.Revision
	after, err := s.TracePage(t.Context(), incremental)
	if err != nil {
		t.Fatal(err)
	}
	question := traceRow(t, after, "question", "ask")
	if question.Span.State != "answered" || question.Span.EndNS == nil {
		t.Fatal(question)
	}
	if count(t, s, "trace_index") != 6 {
		t.Fatal("index retained every update instead of latest identity")
	}
}

func TestTraceAttemptsRecoveryDeletionAndReopenTombstones(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "input")
	turn := claim(t, s, owner.ID).Turn
	request := attemptRequest(turn.ID, "attempt")
	reserveTest(t, s, request)
	dispatchTest(t, s, request.ID)
	running := readTrace(t, s, owner.ID)
	attempt := traceRow(t, running, "attempt", string(request.ID))
	if attempt.Span.EndNS != nil || attempt.Span.State != "dispatched" {
		t.Fatal(attempt)
	}
	for _, attribute := range attempt.Span.Attributes {
		if attribute.Key == "whip.cost.nano_usd" {
			t.Fatal("unknown cost fabricated", attribute)
		}
	}
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	finished := readTrace(t, s, owner.ID)
	attempt = traceRow(t, finished, "attempt", string(request.ID))
	if attempt.Span.EndNS == nil || attempt.Span.State != "uncertain" {
		t.Fatal(attempt)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	other := openTest(t, path)
	query := traceQuery(owner.ID)
	query.After = finished.Revision
	query.TraceID = strings.Repeat("a", 32)
	query.RootsOnly = true
	deleted, err := other.TracePage(t.Context(), query)
	if err != nil || len(deleted.Items) != 2 {
		t.Fatal(deleted, err)
	}
	for _, row := range deleted.Items {
		if row.Span != nil || row.RootID != owner.ID {
			t.Fatal("deletion leaked surviving billing rows", row)
		}
	}

	_, foreign := create(t, s, nil)
	if page := readTrace(t, s, foreign.ID); len(page.Items) != 0 {
		t.Fatal("cross-root trace", page)
	}
	if _, err := s.TracePage(t.Context(), traceQuery("missing")); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestTraceExactSpawnCausalityAndHumanReceiptCannotForgeIt(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	request := ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "child"}}}
	op := childControl(t, s, owner, cell, "spawn", "agents.spawn", request)
	child, err := s.SpawnChildOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	childTurn := claim(t, s, child.Session.ID).Turn
	page := readTrace(t, s, owner.ID)
	row := traceRow(t, page, "turn", string(childTurn.ID))
	if row.Span.TraceID != session.TraceID(cell.TurnID) || row.Span.ParentSpanID == nil || *row.Span.ParentSpanID != session.TraceSpanID("operation", string(op.ID)) {
		t.Fatal("lost exact child cause", row)
	}
	if _, err := s.Finish(t.Context(), childTurn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	// This human API may choose the same receipt namespace, but there is no
	// successful canonical operation that emitted this exact input identity.
	if _, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "operation", RequestID: "forged"}, Submission{SessionID: child.Session.ID, Source: session.AgentInput, Parts: []session.Part{{Type: "text", Text: "human"}}}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("reserved receipt accepted", err)
	}
	submit(t, s, child.Session.ID, "human")
	separate := claim(t, s, child.Session.ID).Turn
	row = traceRow(t, readTrace(t, s, owner.ID), "turn", string(separate.ID))
	if row.Span.ParentSpanID != nil || row.Span.TraceID != session.TraceID(separate.ID) {
		t.Fatal("forged parentage", row)
	}
	query := traceQuery(owner.ID)
	query.TraceID = session.TraceID(cell.TurnID)
	query.RootsOnly = true
	roots, err := s.TracePage(t.Context(), query)
	if err != nil || len(roots.Items) != 1 || roots.Items[0].SourceID != string(cell.TurnID) || roots.Next != roots.Revision {
		t.Fatal(roots, err)
	}
}

func TestTraceDirectOperationBoundedPreviewFilteringAndCancellation(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	admission := directInput(t, s, owner.ID, "direct")
	work := claim(t, s, owner.ID)
	spec := session.OperationSpec{ID: "direct", DirectTurnID: work.Turn.ID, RequestID: "direct", Capability: "shell.run", Resource: owner.WorkingDirectory, Arguments: admission.Input.HostOperation.Arguments}
	// Store retains the prepared intent, which can be larger than page previews.
	spec.Arguments = json.RawMessage(`{"command":"` + strings.Repeat("界", 10000) + `"}`)
	admitOperation(t, s, spec)
	page := readTrace(t, s, owner.ID)
	row := traceRow(t, page, "operation", "direct")
	if row.Span.ParentSpanID == nil || *row.Span.ParentSpanID != session.TraceSpanID("turn", string(work.Turn.ID)) || count(t, s, "cells") != 0 {
		t.Fatal(row)
	}
	for _, attribute := range row.Span.Attributes {
		if attribute.Text != nil && len([]rune(*attribute.Text)) > 512 {
			t.Fatal("unbounded preview", attribute.Key)
		}
	}
	query := traceQuery(owner.ID)
	query.MaxBytes = 4096
	first, err := s.TracePage(t.Context(), query)
	if err != nil || len(first.Items) == 0 || !first.HasMore {
		t.Fatal(first, err)
	}
	query.After = first.Next
	query.ExpectedRevision = &first.Revision
	query.MaxBytes = 512 << 10
	rest, err := s.TracePage(t.Context(), query)
	if err != nil || len(rest.Items) == 0 || rest.HasMore {
		t.Fatal(rest, err)
	}
	query = traceQuery(owner.ID)
	query.TraceID = strings.Repeat("f", 32)
	empty, err := s.TracePage(t.Context(), query)
	if err != nil || len(empty.Items) != 0 || empty.Next != page.Revision {
		t.Fatal(empty, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.TracePage(ctx, traceQuery(owner.ID)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, change := range []func(*session.TraceQuery){func(q *session.TraceQuery) { q.Limit = 2049 }, func(q *session.TraceQuery) { q.MaxBytes = 4095 }, func(q *session.TraceQuery) { q.After = -1 }, func(q *session.TraceQuery) { q.TraceID = "bad" }} {
		q := traceQuery(owner.ID)
		change(&q)
		if _, err := s.TracePage(t.Context(), q); !errors.Is(err, session.ErrInvalid) {
			t.Fatal(err)
		}
	}
}

func TestTraceSchemaRejectsPredecessor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.db")
	s := openTest(t, path)
	execTest(t, s, "PRAGMA user_version=44")
	if old, err := Open(t.Context(), path); err == nil {
		_ = old.Close()
		t.Fatal("opened predecessor schema")
	}
}

func TestTraceBodiesCanonicalEvidenceRevisionAndOwnerBinding(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	spec := operationSpec(cell, "body")
	spec.Arguments = json.RawMessage(`{"path":"` + strings.Repeat("full", 1000) + `"}`)
	op := admitOperation(t, s, spec)
	page := readTrace(t, s, owner.ID)
	row := traceRow(t, page, "operation", string(op.ID))
	input, output, err := s.TraceBodies(t.Context(), row, page.Revision)
	if err != nil || input == nil || *input != string(spec.Arguments) || output != nil {
		t.Fatal(input, output, err)
	}
	tampered := row
	tampered.SessionID = "foreign"
	if _, _, err := s.TraceBodies(t.Context(), tampered, page.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("owner spoof", err)
	}
	if _, err := s.ResolvePermission(t.Context(), op.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.TraceBodies(t.Context(), row, page.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("body mixed newer revision", err)
	}
	page = readTrace(t, s, owner.ID)
	row = traceRow(t, page, "cell", string(cell.ID))
	input, output, err = s.TraceBodies(t.Context(), row, page.Revision)
	if err != nil || input == nil || !strings.Contains(*input, `"code":"1"`) || output != nil {
		t.Fatal(input, output, err)
	}
}

func TestTraceSettledModelBodiesUsageAndCostAreExact(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "input")
	turn := claim(t, s, owner.ID).Turn
	attempt := reserveTest(t, s, attemptRequest(turn.ID, "model"))
	dispatchTest(t, s, attempt.ID)
	outcome := session.ModelAttemptResult{State: session.AttemptSucceeded, Usage: session.ModelUsage{Input: new(int64(10)), Output: new(int64(2))}, ReportedCostNanoUSD: new(int64(9007199254740993))}
	draft := session.MessageDraft{ID: "model_output", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: strings.Repeat("available output", 100)}}}
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, outcome, &draft); err != nil {
		t.Fatal(err)
	}
	page := readTrace(t, s, owner.ID)
	row := traceRow(t, page, "attempt", string(attempt.ID))
	input, output, err := s.TraceBodies(t.Context(), row, page.Revision)
	if err != nil || input != nil || output == nil || !strings.Contains(*output, strings.Repeat("available output", 100)) {
		t.Fatal(input, output, err)
	}
	count := 0
	for _, attribute := range row.Span.Attributes {
		switch attribute.Key {
		case "whip.cost.nano_usd":
			if attribute.Count == nil || *attribute.Count != 9007199254740993 {
				t.Fatal(attribute)
			}
			count++
		case "whip.input.body_available":
			if attribute.Flag == nil || *attribute.Flag {
				t.Fatal(attribute)
			}
			count++
		}
	}
	if count != 2 {
		t.Fatal(row)
	}
}

func TestTraceChildDeletionTombstonesRetainedAccounting(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	child := controlChild(t, s, owner.ID, "child")
	turn := claim(t, s, child.Session.ID).Turn
	attempt := reserveTest(t, s, attemptRequest(turn.ID, "child-attempt"))
	dispatchTest(t, s, attempt.ID)
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, session.ModelAttemptResult{State: session.AttemptFailed, Failure: new("provider failed")}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Failed, new("provider failed"), nil); err != nil {
		t.Fatal(err)
	}
	page := readTrace(t, s, owner.ID)
	if err := s.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "model_attempts") != 1 {
		t.Fatal("ancestor accounting was removed")
	}
	query := traceQuery(owner.ID)
	query.After = page.Revision
	tombstones, err := s.TracePage(t.Context(), query)
	if err != nil || len(tombstones.Items) != 2 {
		t.Fatal(tombstones, err)
	}
	for _, row := range tombstones.Items {
		if row.Span != nil {
			t.Fatal("retained billing was projected as live trace", row)
		}
	}
}

func TestTraceFilteredScanHasBoundedForwardProgress(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	// Canonical waiting intents exercise scan limits without thousands of test
	// transactions. No process or provider work is created by these fixture rows.
	execTest(t, s, `WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<2050)
 INSERT INTO operations(id,cell_id,request_id,capability,resource,arguments,state,created_at)
 SELECT 'scan_'||x,?,'scan_'||x,'filesystem.read','/workspace','{}','waiting',? FROM n`, cell.ID, now())
	query := traceQuery(owner.ID)
	query.TraceID = strings.Repeat("a", 32)
	first, err := s.TracePage(t.Context(), query)
	if err != nil || !first.HasMore || len(first.Items) != 0 || first.Next <= 0 || first.Next >= first.Revision {
		t.Fatal(first, err)
	}
	query.After = first.Next
	query.ExpectedRevision = &first.Revision
	second, err := s.TracePage(t.Context(), query)
	if err != nil || second.HasMore || len(second.Items) != 0 || second.Next != first.Revision {
		t.Fatal(second, err)
	}
}

func TestTraceBackwardExactWindowRevisionAndScope(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := admitOperation(t, s, operationSpec(cell, "read"))
	execTest(t, s, "UPDATE trace_index SET sequence=sequence+9007199254740993")
	forward := readTrace(t, s, owner.ID)
	query := traceQuery(owner.ID)
	query.Backward, query.Limit = true, 2
	first, err := s.TracePage(t.Context(), query)
	if err != nil || len(first.Items) != 2 || !first.HasMore || first.Revision != forward.Revision {
		t.Fatal(first, err)
	}
	if first.Items[0].Sequence != forward.Items[3].Sequence || first.Next != first.Items[1].Sequence || first.Next <= 9007199254740993 {
		t.Fatal("inexact newest window", first)
	}
	query.Before, query.ExpectedRevision = &first.Next, &first.Revision
	second, err := s.TracePage(t.Context(), query)
	if err != nil || len(second.Items) != 2 || second.HasMore || second.Next != 0 || second.Items[0].Sequence >= first.Next || second.Items[1].Sequence != forward.Items[0].Sequence {
		t.Fatal("exclusive older window", second, err)
	}
	query.Before = new(int64(0))
	empty, err := s.TracePage(t.Context(), query)
	if err != nil || len(empty.Items) != 0 || empty.HasMore || empty.Next != 0 {
		t.Fatal("zero is an exclusive lower edge, not a newest alias", empty, err)
	}
	query.Before = nil
	query.RootsOnly = true
	roots, err := s.TracePage(t.Context(), query)
	if err != nil || len(roots.Items) != 1 || roots.Items[0].SourceID != string(cell.TurnID) || roots.Next != 0 {
		t.Fatal("filtered newest scan", roots, err)
	}
	query.Before, query.RootsOnly = &first.Next, false
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TracePage(t.Context(), query); !errors.Is(err, ErrConflict) {
		t.Fatal("missed changed revision", err)
	}
	// An explicit refreshed older window keeps its captured edge, never jumps
	// to newly changed sources at the head.
	query.ExpectedRevision = nil
	refreshed, err := s.TracePage(t.Context(), query)
	if err != nil || refreshed.Revision <= first.Revision {
		t.Fatal(refreshed, err)
	}
	for _, row := range refreshed.Items {
		if row.Sequence >= first.Next || row.RootID != owner.ID {
			t.Fatal("older window retargeted", row)
		}
	}
	_, foreign := create(t, s, nil)
	query.RootID, query.Before = foreign.ID, nil
	foreignPage, err := s.TracePage(t.Context(), query)
	if err != nil || len(foreignPage.Items) != 0 || foreignPage.Revision != 0 {
		t.Fatal("cross-root evidence", foreignPage, err)
	}
	for _, invalid := range []session.TraceQuery{
		{RootID: owner.ID, Backward: true, After: 1, Limit: 10, MaxBytes: 4096},
		{RootID: owner.ID, Before: new(int64(1)), Limit: 10, MaxBytes: 4096},
		{RootID: owner.ID, Backward: true, Before: new(int64(-1)), Limit: 10, MaxBytes: 4096},
	} {
		if _, err := s.TracePage(t.Context(), invalid); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid direction bounds accepted", invalid, err)
		}
	}
}
