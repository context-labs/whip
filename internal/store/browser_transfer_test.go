package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func transferOperation(t *testing.T, s *Store) (session.OperationSpec, ChildTransferIntent, session.Grant) {
	t.Helper()
	owner, cell := operationCell(t, s)
	scope := browserScope()
	grant, err := s.CreateGrant(t.Context(), session.Grant{ID: "browser-standing", SessionID: owner.ID, Capability: "browser.control", Resource: scope.Resource()})
	if err != nil {
		t.Fatal(err)
	}
	spec := operationSpec(cell, "transfer-spawn")
	spec.Capability = "agents.spawn"
	spec.Resource = string(owner.TreeID)
	next := scope
	next.AttachmentID = "child-attachment"
	next.AttachmentGeneration = "child-generation"
	request := childRequest(owner.ID)
	request.BrowserAttachments = []string{scope.AttachmentID}
	intent := ChildTransferIntent{Request: request, ChildID: TransferChildID(spec.ID), Parents: []session.BrowserScope{scope}, Children: []session.BrowserScope{next}}
	spec.Arguments, _ = json.Marshal(intent)
	return spec, intent, grant
}

func TestChildTransferConsentChecksAndAtomicSettlement(t *testing.T) {
	s := fresh(t)
	spec, intent, grant := transferOperation(t, s)
	op := admitOperation(t, s, spec)
	if op.State != session.OperationWaiting {
		t.Fatal(op)
	}
	if err := s.CheckChildTransfer(t.Context(), op.ID, false); !errors.Is(err, ErrConflict) {
		t.Fatal("check before consent", err)
	}
	if _, err := s.CommitChildTransfer(t.Context(), op.ID, intent.Children); !errors.Is(err, ErrConflict) {
		t.Fatal("commit before dispatch", err)
	}
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	sessions, receipts, grants, inputs := count(t, s, "sessions"), count(t, s, "receipts"), count(t, s, "grants"), count(t, s, "inputs")
	for range 2 {
		if err := s.CheckChildTransfer(t.Context(), op.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	if count(t, s, "sessions") != sessions || count(t, s, "receipts") != receipts || count(t, s, "grants") != grants || count(t, s, "inputs") != inputs {
		t.Fatal("preflight published child facts")
	}
	if ok, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := s.CheckChildTransfer(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `CREATE TRIGGER fail_transfer_settle BEFORE UPDATE ON operations WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'settlement fault'); END`)
	if _, err := s.CommitChildTransfer(t.Context(), op.ID, intent.Children); err == nil {
		t.Fatal("settlement fault ignored")
	}
	after, err := s.Operation(t.Context(), op.ID)
	if err != nil || after.State != session.OperationDispatched || count(t, s, "sessions") != sessions || count(t, s, "receipts") != receipts || count(t, s, "grants") != grants || count(t, s, "inputs") != inputs {
		t.Fatal("partial child publication", after, err)
	}
	execTest(t, s, "DROP TRIGGER fail_transfer_settle")
	// Retrying this SQL-only method is safe; the native handoff itself must not be repeated.
	got, err := s.CommitChildTransfer(t.Context(), op.ID, intent.Children)
	if err != nil {
		t.Fatal(err)
	}
	if got.Session.ID != intent.ChildID {
		t.Fatal(got)
	}
	settled, err := s.Operation(t.Context(), op.ID)
	if err != nil || settled.State != session.OperationSucceeded {
		t.Fatal(settled, err)
	}
	value, err := ChildAdmissionValue(got)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleOperation(t.Context(), op.ID, session.OperationResult{State: session.OperationSucceeded, Value: value}); err != nil {
		t.Fatal("dispatcher exact acknowledgement", err)
	}
	delegated, err := matchingGrant(t.Context(), s.db, got.Session.ID, "browser.control", grant.Resource)
	if err != nil || delegated.IssuerID == nil || *delegated.IssuerID != grant.ID {
		t.Fatal(delegated, err)
	}
	if _, err := s.RevokeGrant(t.Context(), grant.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := s.CommitChildTransfer(t.Context(), op.ID, intent.Children)
	if err != nil || !reflect.DeepEqual(got, retry) {
		t.Fatal("exact retry read mutable authority", retry, err)
	}
	if err := s.DeleteSubtree(t.Context(), got.Session.ID); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.CommitChildTransfer(t.Context(), op.ID, intent.Children)
	if err != nil || deleted.Session != nil || deleted.Admission.Receipt.DeletedAt == nil {
		t.Fatal("retry resurrected child", deleted, err)
	}
}

func TestChildTransferRechecksScopeDelegationAndCapacity(t *testing.T) {
	for _, change := range []string{"omitted-grant", "child-without-browser", "scope-expansion", "duplicate", "capacity", "revocation"} {
		t.Run(change, func(t *testing.T) {
			s := fresh(t)
			spec, intent, grant := transferOperation(t, s)
			switch change {
			case "omitted-grant":
				intent.Request.GrantIDs = []session.GrantID{}
			case "child-without-browser":
				intent.Request.Overrides.Modules = []string{"agents"}
			case "scope-expansion":
				intent.Children[0].ProfileID = "another-profile"
			case "duplicate":
				intent.Request.BrowserAttachments = append(intent.Request.BrowserAttachments, intent.Request.BrowserAttachments[0])
			}
			spec.Arguments, _ = json.Marshal(intent)
			if change != "capacity" && change != "revocation" {
				if _, err := s.AdmitOperation(t.Context(), spec); err == nil {
					t.Fatal("invalid transfer admitted")
				}
				return
			}
			approveDispatchBrowser(t, s, spec)
			if change == "revocation" {
				if _, err := s.RevokeGrant(t.Context(), grant.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := s.SetResource(t.Context(), intent.Request.ParentID, 1, session.ResourceLimit{Kind: session.ResourceDescendants, Limit: new(int64(0))}); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.CheckChildTransfer(t.Context(), spec.ID, true); err == nil {
				t.Fatal("changed authority/capacity accepted")
			}
			if _, err := s.CommitChildTransfer(t.Context(), spec.ID, intent.Children); err == nil {
				t.Fatal("commit ignored changed authority/capacity")
			}
			if count(t, s, "sessions") != 1 {
				t.Fatal("failed transfer left child")
			}
		})
	}
}

func TestChildTransferRecoveryNeverCreatesChildOrReplaysOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	spec, intent, _ := transferOperation(t, s)
	approveDispatchBrowser(t, s, spec)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	if _, err := reopened.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	op, err := reopened.Operation(t.Context(), spec.ID)
	if err != nil || op.State != session.OperationUncertain {
		t.Fatal(op, err)
	}
	if _, err := reopened.CommitChildTransfer(t.Context(), spec.ID, intent.Children); !errors.Is(err, ErrConflict) {
		t.Fatal("recovery replayed native-dependent child", err)
	}
	if count(t, reopened, "sessions") != 1 {
		t.Fatal("recovery fabricated child")
	}
}
