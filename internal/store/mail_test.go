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
	"time"

	"github.com/context-labs/whip/internal/session"
)

func sendMailTest(t *testing.T, s *Store, spec session.MailSpec) session.MailMetadata {
	t.Helper()
	result, err := s.SendMail(t.Context(), spec)
	if err != nil || result.Mail == nil {
		t.Fatalf("mail admission=%+v err=%v", result, err)
	}
	return *result.Mail
}

func mailSpec(owner session.SessionID, id string, delivery session.MailDelivery) session.MailSpec {
	return session.MailSpec{ID: session.MailID(id), SenderID: owner, RecipientID: owner, Delivery: delivery, Body: "body for " + id}
}

func mailStateTest(t *testing.T, s *Store, recipient session.SessionID, id session.MailID, state session.MailState, revision int64) session.Mail {
	t.Helper()
	value, err := s.ReadMail(t.Context(), recipient, id)
	if err != nil || value.State != state || value.Revision != revision {
		t.Fatalf("mail=%+v err=%v want=%s/%d", value, err, state, revision)
	}
	return value
}

func finishMailTest(t *testing.T, s *Store, turn session.TurnID, state session.TurnState) {
	t.Helper()
	if _, err := s.Finish(t.Context(), turn, state, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func mailOperation(t *testing.T, s *Store, owner session.Session, cell session.Cell, id, name string, args any) session.Operation {
	t.Helper()
	if _, err := s.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("grant_" + id), SessionID: owner.ID, Capability: "mail." + name, Resource: string(owner.TreeID)}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return admitOperation(t, s, session.OperationSpec{ID: session.OperationID(id), CellID: cell.ID, RequestID: id, Capability: "mail." + name, Resource: string(owner.TreeID), Arguments: raw})
}

func applyMailTest(t *testing.T, s *Store, operation session.Operation) json.RawMessage {
	t.Helper()
	value, err := s.ApplyMailOperation(t.Context(), operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func settleMailCell(t *testing.T, s *Store, cell session.Cell) {
	t.Helper()
	if _, err := s.SettleCell(t.Context(), cell.ID, session.CellSucceeded, session.ToolResult{CallID: cell.CallID, Output: "done"}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMailConcurrentSendRetryAndAtomicAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	_, owner := create(t, s, session.DefaultTreePolicy())
	spec := mailSpec(owner.ID, "mail", session.MailQueued)
	var workers sync.WaitGroup
	for i := range 12 {
		workers.Go(func() {
			db := s
			if i%2 != 0 {
				db = other
			}
			sendMailTest(t, db, spec)
		})
	}
	workers.Wait()
	if count(t, s, "mail") != 1 || count(t, s, "mail_revisions") != 1 || count(t, s, "inputs") != 0 {
		t.Fatal("mail admission duplicated work or created an input")
	}
	changed := spec
	changed.Body = "changed"
	if _, err := s.SendMail(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	execTest(t, s, `CREATE TRIGGER fail_mail_revision BEFORE INSERT ON mail_revisions WHEN NEW.mail_id='rollback' BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.SendMail(t.Context(), mailSpec(owner.ID, "rollback", session.MailQueued)); err == nil {
		t.Fatal("injected admission failure ignored")
	}
	if count(t, s, "mail") != 1 || count(t, s, "mail_revisions") != 1 {
		t.Fatal("failed admission left partial identity")
	}
}

func TestMailClaimsRevisionsWithoutRewritingHistoryAndFinishIsAtomic(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, session.DefaultTreePolicy())
	spec := mailSpec(owner.ID, "mail", session.MailQueued)
	sendMailTest(t, s, spec)
	turn := claim(t, s, owner.ID)
	if turn.Input != nil || count(t, s, "inputs") != 0 {
		t.Fatal("mail-only turn invented an input")
	}
	before, err := s.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(before) != 1 || before[0].Mail == nil || before[0].Mail.Revision != 1 || before[0].InputID != nil || before[0].Role != session.User {
		t.Fatalf("mail provenance missing: %+v %v", before, err)
	}
	spec.Body = "replacement"
	if _, err := s.ReplaceMail(t.Context(), spec, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReplaceMail(t.Context(), spec, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale replacement=%v", err)
	}
	finishMailTest(t, s, turn.Turn.ID, session.Succeeded)
	mailStateTest(t, s, owner.ID, spec.ID, session.MailPending, 2)
	after, err := s.History(t.Context(), owner.ID, 0, 100)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("mail replacement rewrote a prior transcript entry")
	}
	next := claim(t, s, owner.ID)
	execTest(t, s, `CREATE TRIGGER fail_mail_finish BEFORE UPDATE ON turns WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.Finish(t.Context(), next.Turn.ID, session.Succeeded, nil, nil); err == nil {
		t.Fatal("injected turn settlement failure ignored")
	}
	mailStateTest(t, s, owner.ID, spec.ID, session.MailPending, 2)
	execTest(t, s, "DROP TRIGGER fail_mail_finish")
	finishMailTest(t, s, next.Turn.ID, session.Succeeded)
	finishMailTest(t, s, next.Turn.ID, session.Succeeded)
	mailStateTest(t, s, owner.ID, spec.ID, session.MailDelivered, 2)
}

func TestMailFailureBarrierAppliesAtEveryDepthAndSurvivesReopen(t *testing.T) {
	for _, state := range []session.TurnState{session.Failed, session.Cancelled, session.Interrupted} {
		t.Run(string(state), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			_, root := create(t, s, session.DefaultTreePolicy())
			child := spawnChildTest(t, s, "child", childRequest(root.ID))
			finishMailTest(t, s, claim(t, s, child.Session.ID).Turn.ID, session.Succeeded)
			for i, id := range []session.SessionID{root.ID, child.Session.ID} {
				sendMailTest(t, s, mailSpec(id, fmt.Sprintf("first_%d", i), session.MailQueued))
				turn := claim(t, s, id)
				if state == session.Interrupted {
					if _, err := s.Recover(t.Context()); err != nil {
						t.Fatal(err)
					}
				} else {
					finishMailTest(t, s, turn.Turn.ID, state)
				}
				sendMailTest(t, s, mailSpec(id, fmt.Sprintf("new_%d", i), session.MailSteer))
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, path)
			ready, err := s.QueuedSessions(t.Context(), 100)
			if err != nil || len(ready) != 0 {
				t.Fatalf("retry barrier bypassed: %v %v", ready, err)
			}
			for i, id := range []session.SessionID{root.ID, child.Session.ID} {
				if _, err := s.Claim(t.Context(), id); !errors.Is(err, ErrNoWork) {
					t.Fatalf("blocked mail claim=%v", err)
				}
				submit(t, s, id, fmt.Sprintf("recover_%d", i))
				finishMailTest(t, s, claim(t, s, id).Turn.ID, session.Succeeded)
				mailStateTest(t, s, id, session.MailID(fmt.Sprintf("first_%d", i)), session.MailDelivered, 1)
			}
		})
	}
}

func TestMailListDoesNotDeliverAndReadRetryKeepsOriginalBody(t *testing.T) {
	for _, name := range []string{"list", "read"} {
		t.Run(name, func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			spec := mailSpec(owner.ID, "mail", session.MailQueued)
			sendMailTest(t, s, spec)
			var args any = session.MailList{Limit: 100}
			if name == "read" {
				args = session.MailRead{ID: spec.ID}
			}
			op := mailOperation(t, s, owner, cell, "observe", name, args)
			result := applyMailTest(t, s, op)
			if name == "read" {
				var value session.Mail
				if err := json.Unmarshal(result, &value); err != nil || value.Body != spec.Body {
					t.Fatalf("read body=%+v err=%v", value, err)
				}
				spec.Body = "updated"
				if _, err := s.ReplaceMail(t.Context(), spec, 1); err != nil {
					t.Fatal(err)
				}
				if again := applyMailTest(t, s, op); string(again) != string(result) {
					t.Fatal("operation retry returned a different mail revision")
				}
				stored, err := s.Operation(t.Context(), op.ID)
				if err != nil || strings.Contains(string(stored.Result.Value), "body for mail") {
					t.Fatal("operation copied canonical mail body")
				}
			}
			settleMailCell(t, s, cell)
			finishMailTest(t, s, cell.TurnID, session.Succeeded)
			revision := int64(1)
			if name == "read" {
				revision = 2
			}
			mailStateTest(t, s, owner.ID, spec.ID, session.MailPending, revision)
		})
	}
}

func TestMailHandlingRequiresObservedCurrentRevisionsAndRollsBack(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	first := sendMailTest(t, s, mailSpec(owner.ID, "first", session.MailNextTurn))
	second := sendMailTest(t, s, mailSpec(owner.ID, "second", session.MailNextTurn))
	op := mailOperation(t, s, owner, cell, "unobserved", "complete", session.MailComplete{Receipts: []session.MailReceipt{first.MailReceipt}})
	if _, err := s.ApplyMailOperation(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("unobserved handling=%v", err)
	}
	applyMailTest(t, s, mailOperation(t, s, owner, cell, "list", "list", session.MailList{Limit: 100}))
	changed := mailSpec(owner.ID, "second", session.MailNextTurn)
	changed.Body = "changed"
	if _, err := s.ReplaceMail(t.Context(), changed, 1); err != nil {
		t.Fatal(err)
	}
	stale := mailOperation(t, s, owner, cell, "stale_batch", "complete", session.MailComplete{Receipts: []session.MailReceipt{first.MailReceipt, second.MailReceipt}})
	if _, err := s.ApplyMailOperation(t.Context(), stale.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale batch=%v", err)
	}
	mailStateTest(t, s, owner.ID, first.ID, session.MailPending, 1)
	complete := mailOperation(t, s, owner, cell, "complete", "complete", session.MailComplete{Receipts: []session.MailReceipt{first.MailReceipt}})
	execTest(t, s, `CREATE TRIGGER fail_mail_operation BEFORE UPDATE ON operations WHEN NEW.id='complete' AND NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'fault'); END`)
	if _, err := s.ApplyMailOperation(t.Context(), complete.ID); err == nil {
		t.Fatal("operation fault ignored")
	}
	mailStateTest(t, s, owner.ID, first.ID, session.MailPending, 1)
	execTest(t, s, "DROP TRIGGER fail_mail_operation")
	applyMailTest(t, s, complete)
	mailStateTest(t, s, owner.ID, first.ID, session.MailDone, 1)
	applyMailTest(t, s, mailOperation(t, s, owner, cell, "read_current", "read", session.MailRead{ID: second.ID}))
	future := time.Now().Add(time.Hour)
	deferOp := mailOperation(t, s, owner, cell, "defer", "defer", session.MailDefer{Receipt: session.MailReceipt{ID: second.ID, Revision: 2}, AvailableAt: future})
	applyMailTest(t, s, deferOp)
	applyMailTest(t, s, deferOp)
	deferred := mailStateTest(t, s, owner.ID, second.ID, session.MailPending, 3)
	if deferred.AvailableAt.UnixMicro() != future.UnixMicro() {
		t.Fatal("deferral lost requested availability")
	}
}

func TestMailSenderDeletionRetainsMailAndRecipientDeletionRetainsOnlyTombstone(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, session.DefaultTreePolicy())
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	spec := mailSpec(root.ID, "mail", session.MailNextTurn)
	spec.SenderID = child.Session.ID
	sendMailTest(t, s, spec)
	if err := s.DeleteSubtree(t.Context(), child.Session.ID); err != nil {
		t.Fatal(err)
	}
	mailStateTest(t, s, root.ID, spec.ID, session.MailPending, 1)
	if _, err := s.SendMail(t.Context(), spec); err != nil {
		t.Fatal("send retry depended on deleted sender", err)
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.SendMail(t.Context(), spec)
	if err != nil || deleted.Mail != nil || deleted.DeletedAt == nil || count(t, s, "mail_revisions") != 0 {
		t.Fatalf("deleted recipient resurrected mail: %+v %v", deleted, err)
	}
}

func TestMailDeliveryClassesAvailabilityAndBounds(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, session.DefaultTreePolicy())
	next := mailSpec(owner.ID, "next", session.MailNextTurn)
	sendMailTest(t, s, next)
	future := mailSpec(owner.ID, "future", session.MailQueued)
	future.AvailableAt = new(time.Now().Add(time.Hour))
	sendMailTest(t, s, future)
	if _, err := s.Claim(t.Context(), owner.ID); !errors.Is(err, ErrNoWork) {
		t.Fatalf("future/next-turn mail started work: %v", err)
	}
	queued := mailSpec(owner.ID, "queued", session.MailQueued)
	queued.Body = strings.Repeat("é\n", 50) + strings.Repeat("界", 1000)
	sendMailTest(t, s, queued)
	turn := claim(t, s, owner.ID)
	history, err := s.History(t.Context(), owner.ID, 0, 100)
	if err != nil || len(history) != 2 {
		t.Fatalf("due classes not presented together: %+v %v", history, err)
	}
	for _, message := range history {
		if message.Mail.ID == future.ID || len(message.Parts[0].Text) > 2500 || strings.Count(message.Parts[0].Text, "\n") > 21 {
			t.Fatalf("digest exceeded presentation bounds: %+v", message)
		}
	}
	finishMailTest(t, s, turn.Turn.ID, session.Succeeded)
	mailStateTest(t, s, owner.ID, future.ID, session.MailPending, 1)
	mailStateTest(t, s, owner.ID, next.ID, session.MailDelivered, 1)
	mailStateTest(t, s, owner.ID, queued.ID, session.MailDelivered, 1)
	for i := 3; i < session.MaxMailBacklog; i++ {
		sendMailTest(t, s, mailSpec(owner.ID, fmt.Sprintf("backlog_%d", i), session.MailNextTurn))
	}
	if _, err := s.SendMail(t.Context(), mailSpec(owner.ID, "overfull", session.MailQueued)); !errors.Is(err, ErrLimit) {
		t.Fatalf("mail sender backlog not bounded: %v", err)
	}
	invalid := mailSpec(owner.ID, "oversized", session.MailQueued)
	invalid.Body = strings.Repeat("x", session.MaxMailBodyBytes+1)
	if _, err := s.SendMail(t.Context(), invalid); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("oversized body accepted: %v", err)
	}
}

func TestMailOperationSendAndObservationRollBackWithSettlement(t *testing.T) {
	for _, name := range []string{"send", "read"} {
		t.Run(name, func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			var args any = session.MailSend{RecipientID: owner.ID, Delivery: session.MailNextTurn, Body: "operation body"}
			if name == "read" {
				sendMailTest(t, s, mailSpec(owner.ID, "existing", session.MailNextTurn))
				args = session.MailRead{ID: "existing"}
			}
			before := count(t, s, "mail")
			op := mailOperation(t, s, owner, cell, "atomic", name, args)
			execTest(t, s, `CREATE TRIGGER fail_mail_operation BEFORE UPDATE ON operations WHEN NEW.id='atomic' AND NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'fault'); END`)
			if _, err := s.ApplyMailOperation(t.Context(), op.ID); err == nil {
				t.Fatal("fault ignored")
			}
			pending, err := s.Operation(t.Context(), op.ID)
			if err != nil || pending.State != session.OperationReady || count(t, s, "mail") != before || count(t, s, "turn_mail_observations") != 0 {
				t.Fatalf("partial mail mutation escaped rollback: %+v %v", pending, err)
			}
			execTest(t, s, "DROP TRIGGER fail_mail_operation")
			first := applyMailTest(t, s, op)
			if again := applyMailTest(t, s, op); string(first) != string(again) {
				t.Fatal("operation retry changed its result")
			}
		})
	}
}
