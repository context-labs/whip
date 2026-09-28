package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestContentOwnerIdentityRetryChargesDeletionAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	_, root := create(t, s, nil)
	left := controlChild(t, s, root.ID, "left").Session
	right := controlChild(t, s, root.ID, "right").Session
	beforeWrites := budgetState(t, s, root.ID, session.BudgetLogicalWrites).Used
	beforeBytes := budgetState(t, s, root.ID, session.BudgetLogicalWriteBytes).Used
	drafts := []session.ContentReference{
		contentReference(root.ID, "opaque:handle", "shared bytes"),
		contentReference(left.ID, "opaque:handle", "shared bytes"),
		contentReference(right.ID, "opaque:handle", "different private bytes"),
	}
	stored, errs := make([]session.ContentReference, len(drafts)), make([]error, len(drafts))
	var workers sync.WaitGroup
	for i, draft := range drafts {
		workers.Go(func() {
			db := s
			if i == 1 {
				db = other
			}
			stored[i], errs[i] = db.RegisterContent(t.Context(), draft)
		})
	}
	workers.Wait()
	var total int64
	for i, err := range errs {
		if err != nil {
			t.Fatalf("owner %s could not register its opaque handle: %v", drafts[i].SessionID, err)
		}
		total += drafts[i].Size
		for range 2 {
			retry, err := other.RegisterContent(t.Context(), drafts[i])
			if err != nil || !reflect.DeepEqual(retry, stored[i]) {
				t.Fatalf("owner retry changed metadata: %+v %v", retry, err)
			}
		}
		changed := contentReference(drafts[i].SessionID, drafts[i].ID, "replacement")
		if _, err := s.RegisterContent(t.Context(), changed); !errors.Is(err, ErrConflict) {
			t.Fatal("owner replaced an immutable reference", err)
		}
	}
	if count(t, s, "content_references") != 3 || count(t, s, "content_bodies") != 2 {
		t.Fatal("reference ownership or shared body deduplication was lost")
	}
	assertWriteUsage(t, s, root.ID, beforeWrites+3, beforeBytes+total)
	assertWriteUsage(t, s, left.ID, 1, drafts[1].Size)
	assertWriteUsage(t, s, right.ID, 1, drafts[2].Size)
	if err := s.DeleteSubtree(t.Context(), left.ID); err != nil {
		t.Fatal(err)
	}
	assertWriteUsage(t, s, root.ID, beforeWrites+3, beforeBytes+total)
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if _, err := s.RegisterContent(t.Context(), drafts[1]); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted owner resurrected a reference", err)
	}
	for _, i := range []int{0, 2} {
		retry, err := s.RegisterContent(t.Context(), drafts[i])
		if err != nil || !reflect.DeepEqual(retry, stored[i]) {
			t.Fatalf("owner deletion or restart changed another owner's handle: %+v %v", retry, err)
		}
	}
	if err := s.PruneUnusedContent(t.Context()); err != nil || count(t, s, "content_bodies") != 2 {
		t.Fatal("collection discarded surviving bodies", err)
	}
	assertWriteUsage(t, s, root.ID, beforeWrites+3, beforeBytes+total)
}

func TestContentOwnerChargesCannotBeBypassedByAnotherOwnersReference(t *testing.T) {
	s := fresh(t)
	_, first := create(t, s, nil)
	_, second := create(t, s, nil)
	evidenceReference(t, s, first.ID, "same", "already paid")
	budgetLimit(t, s, second.ID, session.BudgetLogicalWrites, 0)
	draft := contentReference(second.ID, "same", "new bytes")
	if _, err := s.RegisterContent(t.Context(), draft); !errors.Is(err, ErrLimit) {
		t.Fatal("other owner's write identity bypassed the allowance", err)
	}
	if count(t, s, "content_bodies") != 1 || count(t, s, "content_references") != 1 {
		t.Fatal("rejected charge left a body or reference")
	}
	assertWriteUsage(t, s, second.ID, 0, 0)
	budgetLimit(t, s, second.ID, session.BudgetLogicalWrites, 1)
	if _, err := s.RegisterContent(t.Context(), draft); err != nil {
		t.Fatal(err)
	}
	assertWriteUsage(t, s, first.ID, 1, int64(len("already paid")))
	assertWriteUsage(t, s, second.ID, 1, draft.Size)
}

func TestContentOwnerMailEvidenceUsesRecipientAndImmutableRevision(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	child := controlChild(t, s, root.ID, "child").Session
	rootRef := evidenceReference(t, s, root.ID, "same", "root bytes")
	childRef := evidenceReference(t, s, child.ID, "same", "child bytes")
	for _, reference := range []session.ContentReference{rootRef, childRef} {
		mail := sendMailTest(t, s, session.MailSpec{ID: session.MailID("self_" + string(reference.SessionID)), SenderID: reference.SessionID, RecipientID: reference.SessionID, Delivery: session.MailNextTurn, EvidenceRef: &reference.ID})
		assertMailEvidence(t, s, mail, reference)
	}
	mail := sendMailTest(t, s, session.MailSpec{ID: "child_to_root", SenderID: child.ID, RecipientID: root.ID, Delivery: session.MailQueued, EvidenceRef: &childRef.ID})
	assertMailEvidence(t, s, mail, childRef)
	// The parent's same-named reference is neither substituted nor overwritten.
	if got, err := s.ContentReference(t.Context(), root.ID, childRef.ID); err != nil || !reflect.DeepEqual(got, rootRef) {
		t.Fatal("mail sharing rebound a recipient handle", got, err)
	}
	if err := s.DeleteSubtree(t.Context(), child.ID); err != nil {
		t.Fatal("another recipient's same-named evidence blocked child deletion", err)
	}
	assertMailEvidence(t, s, mail, childRef)
	if _, err := s.db.ExecContext(t.Context(), "DELETE FROM content_references WHERE owner_session_id=? AND reference_id=?", root.ID, rootRef.ID); err == nil {
		t.Fatal("an immutable self-mail revision lost its evidence")
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal("recipient deletion retained composite evidence ownership", err)
	}
}

func TestContentOwnerMailForeignKeysCannotBorrowRecipientOrReference(t *testing.T) {
	s := fresh(t)
	_, recipient := create(t, s, nil)
	_, foreign := create(t, s, nil)
	ref := evidenceReference(t, s, foreign.ID, "foreign", "secret")
	mail := sendMailTest(t, s, session.MailSpec{ID: "mail", SenderID: recipient.ID, RecipientID: recipient.ID, Delivery: session.MailNextTurn, Body: "no evidence"})
	for _, test := range []struct {
		owner session.SessionID
		ref   *string
	}{{recipient.ID, &ref.ID}, {foreign.ID, &ref.ID}, {foreign.ID, nil}} {
		err := s.write(t.Context(), func(tx *sql.Tx) error {
			return insertMailRevision(t.Context(), tx, session.MailSpec{ID: mail.ID, RecipientID: test.owner, Delivery: session.MailNextTurn, EvidenceRef: test.ref}, 2, now())
		})
		if err == nil || count(t, s, "mail_revisions") != 1 {
			t.Fatal("SQL accepted foreign evidence or a forged revision recipient", test.owner, err)
		}
	}
	rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN DELETE FROM content_references WHERE owner_session_id=? AND reference_id=?", foreign.ID, ref.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	indexed := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		indexed = indexed || strings.Contains(detail, "mail_revision_evidence")
	}
	if err := rows.Err(); err != nil || !indexed {
		t.Fatal("composite evidence FK cleanup lacks an index", err)
	}
}

func TestContentOwnerAutomaticEvidenceCannotBeOccupiedByAnotherOwner(t *testing.T) {
	t.Run("completion", func(t *testing.T) {
		s := fresh(t)
		_, parent := create(t, s, nil)
		_, stranger := create(t, s, nil)
		child := controlChild(t, s, parent.ID, "child").Session
		completion := finishCompletionTest(t, s, parent.ID, child.ID, "actual child output")
		expected := completionReference(t, completion)
		foreign := evidenceReference(t, s, stranger.ID, expected.ID, "unrelated bytes")
		writes := count(t, s, "logical_writes")
		mail := publishCompletionTest(t, s, completion)
		assertMailEvidence(t, s, mail, expected)
		if got, err := s.ContentReference(t.Context(), stranger.ID, expected.ID); err != nil || !reflect.DeepEqual(got, foreign) {
			t.Fatal("publication changed another owner's reference", got, err)
		}
		if count(t, s, "logical_writes") != writes {
			t.Fatal("automatic evidence charged a logical write")
		}
	})
	t.Run("helper", func(t *testing.T) {
		s := fresh(t)
		owner, cell := operationCell(t, s)
		operation := helperOperation(t, s, owner, cell, "helper", "models.call", `{"prompt":"question"}`, true)
		attempt := successfulHelperContentAttempt(t, s, operation, 0)
		body := contentReference(owner.ID, "ignored", "provider bytes")
		ref, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, body.Digest, body.Size)
		if err != nil {
			t.Fatal(err)
		}
		_, stranger := create(t, s, nil)
		foreign := evidenceReference(t, s, stranger.ID, ref.ID, "unrelated bytes")
		writes := count(t, s, "logical_writes")
		retry, err := s.RegisterModelHelperContent(t.Context(), attempt.ID, body.Digest, body.Size)
		if err != nil || !reflect.DeepEqual(retry, ref) || count(t, s, "logical_writes") != writes {
			t.Fatal("helper retry changed ownership or charges", retry, err)
		}
		if err := s.DeleteSubtree(t.Context(), stranger.ID); err != nil {
			t.Fatal(err)
		}
		if got, err := s.ContentReference(t.Context(), owner.ID, foreign.ID); err != nil || !reflect.DeepEqual(got, ref) {
			t.Fatal("foreign owner deletion removed helper evidence", got, err)
		}
	})
}
