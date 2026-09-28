package store

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func assertWriteUsage(t *testing.T, s *Store, owner session.SessionID, writes, bytes int64) {
	t.Helper()
	for kind, want := range map[session.BudgetKind]int64{session.BudgetLogicalWrites: writes, session.BudgetLogicalWriteBytes: bytes} {
		got := budgetState(t, s, owner, kind)
		if got.Used != want || got.Reserved != 0 || got.Uncertain != 0 || got.Incomplete {
			t.Fatalf("%s usage=%+v, want %d", kind, got, want)
		}
	}
}

func TestLogicalWriteDefaultsNarrowingAndConcurrentSiblings(t *testing.T) {
	for _, kind := range []session.BudgetKind{session.BudgetLogicalWrites, session.BudgetLogicalWriteBytes} {
		t.Run(string(kind), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s, other := openTest(t, path), openTest(t, path)
			_, root := create(t, s, nil)
			for budgetKind, want := range map[session.BudgetKind]int64{session.BudgetLogicalWrites: 100_000, session.BudgetLogicalWriteBytes: 1 << 30} {
				got := budgetState(t, s, root.ID, budgetKind)
				if got.Revision != 1 || got.Limit == nil || *got.Limit != want {
					t.Fatalf("root defaults: %+v", got)
				}
				if _, err := s.SetBudget(t.Context(), root.ID, got.Revision, session.BudgetLimit{Kind: budgetKind}); !errors.Is(err, session.ErrInvalid) {
					t.Fatalf("unbounded root: %v", err)
				}
			}
			left := controlChild(t, s, root.ID, "left").Session
			right := controlChild(t, s, root.ID, "right").Session
			initial := budgetState(t, s, root.ID, kind)
			amount := int64(1)
			if kind == session.BudgetLogicalWriteBytes {
				amount = 3
			}
			capped := budgetLimit(t, s, root.ID, kind, initial.Used+amount)
			if _, err := s.SetBudget(t.Context(), left.ID, 0, session.BudgetLimit{Kind: kind, Limit: new(*capped.Limit + 1)}); !errors.Is(err, ErrLimit) {
				t.Fatalf("wider child cap: %v", err)
			}
			if _, err := s.SetBudget(t.Context(), left.ID, 0, session.BudgetLimit{Kind: kind}); err != nil {
				t.Fatal(err)
			}
			results := make([]error, 2)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for i, actor := range []session.SessionID{left.ID, right.ID} {
				workers.Go(func() {
					<-start
					db := s
					if i == 1 {
						db = other
					}
					_, results[i] = db.WriteState(t.Context(), stateWrite(actor, session.TreeState, string(actor), string(actor), `123`, 0))
				})
			}
			close(start)
			workers.Wait()
			oneAccepted := (results[0] == nil && errors.Is(results[1], ErrLimit)) || (results[1] == nil && errors.Is(results[0], ErrLimit))
			if !oneAccepted {
				t.Fatalf("competing writes: %v", results)
			}
			assertWriteUsage(t, s, root.ID, 3, int64(len("left")+len("right")+3))
			if count(t, s, "state_versions") != 1 {
				t.Fatal("rejected write left evidence")
			}
			if _, err := s.SetBudget(t.Context(), root.ID, capped.Revision, session.BudgetLimit{Kind: kind, Limit: new(initial.Used)}); !errors.Is(err, ErrLimit) {
				t.Fatalf("cap dropped below committed usage: %v", err)
			}
			if _, err := s.SetBudget(t.Context(), root.ID, initial.Revision, session.BudgetLimit{Kind: kind, Limit: capped.Limit}); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale same-value edit: %v", err)
			}
		})
	}
}

func TestLogicalWriteChargesActionsNotDerivedRows(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	reference := contentReference(root.ID, "body", "bytes")
	for range 2 {
		if _, err := s.RegisterContent(t.Context(), reference); err != nil {
			t.Fatal(err)
		}
	}
	request := ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "child"}, {Type: "content", ReferenceID: reference.ID}, {Type: "content", ReferenceID: reference.ID}}}
	child := spawnChildTest(t, s, "child", request).Session
	if count(t, s, "content_references") != 2 {
		t.Fatal("duplicate attachment should share one child alias")
	}
	assertWriteUsage(t, s, root.ID, 2, 10)
	assertWriteUsage(t, s, child.ID, 0, 0)
	subscription := subscribeStateTest(t, s, child.ID, "watch", "x", 0)
	subscribeStateTest(t, s, child.ID, "watch", "x", 0)
	putStateTest(t, s, stateWrite(root.ID, session.TreeState, "state1", "x", `"ok"`, 0))
	putStateTest(t, s, stateWrite(root.ID, session.TreeState, "state2", "x", `null`, 1))
	if notice := stateMailTest(t, s, child.ID); notice.Revision != 2 {
		t.Fatal("notification did not coalesce")
	}
	assertWriteUsage(t, s, root.ID, 5, 19)
	assertWriteUsage(t, s, child.ID, 1, 1)
	spec := session.MailSpec{ID: "explicit", SenderID: child.ID, RecipientID: root.ID, Delivery: session.MailQueued, Subject: "s", Body: "body"}
	sendMailTest(t, s, spec)
	sendMailTest(t, s, spec)
	spec.Body = "next"
	if _, err := s.ReplaceMail(t.Context(), spec, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceMail(t.Context(), spec, 1); !errors.Is(err, ErrConflict) {
		t.Fatal("replacement retry did not preserve revision", err)
	}
	if _, err := s.UnsubscribeState(t.Context(), child.ID, subscription.ID); err != nil {
		t.Fatal(err)
	}
	assertWriteUsage(t, s, root.ID, 7, 29)
	assertWriteUsage(t, s, child.ID, 3, 11)
}

func TestLogicalWriteFailureRollsBackEveryAction(t *testing.T) {
	for _, action := range []string{"content", "mail", "spawn", "state", "subscription", "submit"} {
		t.Run(action, func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			child := controlChild(t, s, owner.ID, "child").Session
			reference := contentReference(owner.ID, "shared", "bytes")
			if _, err := s.RegisterContent(t.Context(), reference); err != nil {
				t.Fatal(err)
			}
			var apply func() error
			switch action {
			case "content":
				apply = func() error {
					_, err := s.RegisterContent(t.Context(), contentReference(owner.ID, "new", "new"))
					return err
				}
			case "mail":
				apply = func() error {
					_, err := s.SendMail(t.Context(), mailSpec(owner.ID, "new", session.MailQueued))
					return err
				}
			case "spawn":
				apply = func() error {
					_, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "new"}, ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "content", ReferenceID: reference.ID}}})
					return err
				}
			case "state":
				apply = func() error {
					_, err := s.WriteState(t.Context(), stateWrite(owner.ID, session.TreeState, "new", "new", `null`, 0))
					return err
				}
			case "subscription":
				apply = func() error {
					_, err := s.SubscribeState(t.Context(), owner.ID, "new", session.StateSubscribe{Key: "key", Delivery: session.MailQueued})
					return err
				}
			case "submit":
				op := childControl(t, s, owner, cell, "submit", "agents.submit", session.ChildSubmit{SessionID: child.ID, Parts: []session.Part{{Type: "text", Text: "more"}, {Type: "content", ReferenceID: reference.ID}}})
				apply = func() error { _, err := s.ApplyChildControl(t.Context(), op.ID); return err }
			}
			before := map[string]int{}
			for _, table := range []string{"sessions", "inputs", "receipts", "content_bodies", "content_references", "mail", "mail_revisions", "state_versions", "state_subscriptions", "logical_writes", "logical_write_ancestors"} {
				before[table] = count(t, s, table)
			}
			execTest(t, s, `CREATE TRIGGER fail_charge BEFORE INSERT ON logical_write_ancestors BEGIN SELECT RAISE(ABORT,'injected charge failure'); END`)
			if err := apply(); err == nil {
				t.Fatal("write ignored charge failure")
			}
			for table, expected := range before {
				if got := count(t, s, table); got != expected {
					t.Fatalf("partial %s: got %d want %d", table, got, expected)
				}
			}
			execTest(t, s, "DROP TRIGGER fail_charge")
			if err := apply(); err != nil {
				t.Fatal(err)
			}
			if count(t, s, "logical_writes") != before["logical_writes"]+1 {
				t.Fatal("action was charged more than once")
			}
		})
	}
}

func TestLogicalWriteSpendSurvivesDeletionRestartAndRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, root := create(t, s, nil)
	request := childRequest(root.ID)
	child := spawnChildTest(t, s, "child", request).Session
	state := stateWrite(child.ID, session.TreeState, "state", "shared", `[1]`, 0)
	putStateTest(t, s, state)
	spec := session.MailSpec{ID: "mail", SenderID: child.ID, RecipientID: root.ID, Delivery: session.MailNextTurn, Body: "hi"}
	sendMailTest(t, s, spec)
	reference := contentReference(child.ID, "content", "data")
	if _, err := s.RegisterContent(t.Context(), reference); err != nil {
		t.Fatal(err)
	}
	budgetLimit(t, s, root.ID, session.BudgetLogicalWrites, 4)
	budgetLimit(t, s, root.ID, session.BudgetLogicalWriteBytes, 19)
	putStateTest(t, s, state)
	sendMailTest(t, s, spec)
	spawnChildTest(t, s, "child", request)
	if _, err := s.RegisterContent(t.Context(), reference); err != nil {
		t.Fatal(err)
	}
	changed := state
	changed.SubmittedBytes++
	if _, err := s.WriteState(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatal("state retry changed accounting evidence", err)
	}
	if _, err := s.WriteState(t.Context(), stateWrite(child.ID, session.SessionState, "extra", "extra", `0`, 0)); !errors.Is(err, ErrLimit) {
		t.Fatal("exhausted write admitted", err)
	}
	if err := s.DeleteSubtree(t.Context(), child.ID); err != nil {
		t.Fatal(err)
	}
	assertWriteUsage(t, s, root.ID, 4, 19)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	assertWriteUsage(t, s, root.ID, 4, 19)
	deleted, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "spawn-test", RequestID: "child"}, request)
	if err != nil || deleted.Session != nil || deleted.Admission.Receipt.DeletedAt == nil {
		t.Fatalf("deleted retry=%+v %v", deleted, err)
	}
	sendMailTest(t, s, spec)
	if _, err := s.State(t.Context(), root.ID, session.TreeState, "shared"); err != nil {
		t.Fatal("author deletion removed shared state", err)
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "logical_writes") != 0 || count(t, s, "logical_write_ancestors") != 0 {
		t.Fatal("tree deletion retained accounting")
	}
}

func TestLogicalWriteExhaustionAllowsObservationAndSettlement(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	spec := mailSpec(owner.ID, "mail", session.MailQueued)
	mail := sendMailTest(t, s, spec)
	budgetLimit(t, s, owner.ID, session.BudgetLogicalWrites, 1)
	budgetLimit(t, s, owner.ID, session.BudgetLogicalWriteBytes, int64(len(spec.Body)))
	read := mailOperation(t, s, owner, cell, "read", "read", session.MailRead{ID: mail.ID})
	applyMailTest(t, s, read)
	deferOp := mailOperation(t, s, owner, cell, "defer", "defer", session.MailDefer{Receipt: mail.MailReceipt, AvailableAt: time.Now().Add(time.Hour)})
	applyMailTest(t, s, deferOp)
	settleMailCell(t, s, cell)
	finishMailTest(t, s, cell.TurnID, session.Succeeded)
	submit(t, s, owner.ID, "after-exhaustion")
	turn := claim(t, s, owner.ID).Turn.ID
	attempt := reserveTest(t, s, budgetRequest(turn, "model"))
	dispatchTest(t, s, attempt.ID)
	if _, err := s.SettleModelAttempt(t.Context(), attempt.ID, session.ModelAttemptResult{State: session.AttemptSucceeded, ElapsedMillis: new(int64(1))}, &session.MessageDraft{ID: "final", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "done"}}}); err != nil {
		t.Fatal(err)
	}
	finishMailTest(t, s, turn, session.Succeeded)
	assertWriteUsage(t, s, owner.ID, 1, int64(len(spec.Body)))
}

func TestLogicalWriteOverflowFailsClosedWithoutChangingEvidence(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	budgetLimit(t, s, root.ID, session.BudgetLogicalWriteBytes, math.MaxInt64)
	if err := s.write(t.Context(), func(tx *sql.Tx) error {
		return chargeWrite(t.Context(), tx, root.ID, "content", "huge", 0, math.MaxInt64)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RegisterContent(t.Context(), contentReference(root.ID, "one_more", "x")); !errors.Is(err, ErrLimit) {
		t.Fatalf("overflow accepted: %v", err)
	}
	assertWriteUsage(t, s, root.ID, 1, math.MaxInt64)
	// Corrupt/external evidence cannot wrap inspection or permit further writes.
	if _, err := s.db.ExecContext(t.Context(), "INSERT INTO logical_writes VALUES ('overflow',?,?,'content','external',0,1,0)", root.TreeID, root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(t.Context(), "INSERT INTO logical_write_ancestors VALUES ('overflow',?)", root.ID); err != nil {
		t.Fatal(err)
	}
	state := budgetState(t, s, root.ID, session.BudgetLogicalWriteBytes)
	if state.Used != math.MaxInt64 || !state.Incomplete {
		t.Fatalf("overflow projection=%+v", state)
	}
	if err := s.write(context.Background(), func(tx *sql.Tx) error { return chargeWrite(t.Context(), tx, root.ID, "content", "zero", 0, 0) }); !errors.Is(err, ErrLimit) {
		t.Fatalf("incomplete evidence admitted new write: %v", err)
	}
}
