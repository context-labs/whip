package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func publicTransfer(t *testing.T, s *Store) (session.Session, session.RequestIdentity, ChildRequest) {
	t.Helper()
	_, owner := create(t, s, nil)
	scope := browserScope()
	if _, err := s.CreateGrant(t.Context(), session.Grant{ID: "browser-standing", SessionID: owner.ID, Capability: "browser.control", Resource: scope.Resource()}); err != nil {
		t.Fatal(err)
	}
	request := childRequest(owner.ID)
	request.BrowserAttachments = []string{scope.AttachmentID}
	return owner, session.RequestIdentity{ClientID: "public", RequestID: "child-transfer"}, request
}

func publicTransferOperation(t *testing.T, s *Store, owner session.Session, request ChildRequest) (session.OperationSpec, ChildTransferIntent) {
	t.Helper()
	claim, err := s.Claim(t.Context(), owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	scope := browserScope()
	next := scope
	next.AttachmentID = "next"
	next.AttachmentGeneration = "next-generation"
	spec := session.OperationSpec{ID: "public-transfer", DirectTurnID: claim.Turn.ID, RequestID: string(claim.Input.ID), Capability: "agents.spawn", Resource: string(owner.TreeID)}
	intent := ChildTransferIntent{Request: request, ChildID: TransferChildID(spec.ID), Parents: []session.BrowserScope{scope}, Children: []session.BrowserScope{next}}
	spec.Arguments, err = json.Marshal(intent)
	if err != nil {
		t.Fatal(err)
	}
	return spec, intent
}

func TestPublicChildTransferPrivateIdentityPendingAndExactPublication(t *testing.T) {
	s := fresh(t)
	owner, identity, request := publicTransfer(t, s)
	accepted, err := s.BeginChildTransfer(t.Context(), identity, request)
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Receipt.RequestIdentity == identity || accepted.Receipt.ClientID != childTransferClient || accepted.Input.SessionID != owner.ID || accepted.Input.Kind != session.HostOperationInputKind {
		t.Fatal(accepted)
	}
	if _, err := s.Admission(t.Context(), identity); !errors.Is(err, ErrNotFound) {
		t.Fatal("invented public child receipt", err)
	}
	if _, err := s.MatchChild(t.Context(), identity, request); !errors.Is(err, ErrBusy) {
		t.Fatal("accepted private work reported missing", err)
	}
	retry, err := s.BeginChildTransfer(t.Context(), identity, request)
	if err != nil || !reflect.DeepEqual(retry, accepted) {
		t.Fatal("admission replay", retry, err)
	}
	changed := request
	changed.Parts = []session.Part{{Type: "text", Text: "different"}}
	for _, read := range []func() error{
		func() error { _, err := s.BeginChildTransfer(t.Context(), identity, changed); return err },
		func() error { _, err := s.MatchChild(t.Context(), identity, changed); return err },
		func() error {
			_, err := s.Admit(t.Context(), identity, Submission{SessionID: owner.ID, Source: session.UserInput, Parts: changed.Parts})
			return err
		},
	} {
		if err := read(); !errors.Is(err, ErrConflict) {
			t.Fatal("different payload occupied identity", err)
		}
	}
	if _, err := s.Admit(t.Context(), accepted.Receipt.RequestIdentity, Submission{SessionID: owner.ID, Source: session.UserInput, Parts: request.Parts}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("public used private identity", err)
	}
	if _, err := s.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "public", RequestID: "forged"}, owner.ID, *accepted.Input.HostOperation); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("generic host call forged private admission", err)
	}
	executed := request
	executed.Parts = []session.Part{{Type: "text", Text: "rewritten by hook"}}
	spec, intent := publicTransferOperation(t, s, owner, executed)
	op := admitOperation(t, s, spec)
	if _, err := s.ResolvePermission(t.Context(), op.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckChildTransfer(t.Context(), op.ID, false); err != nil {
		t.Fatal("consented dry run", err)
	}
	if _, err := s.Admission(t.Context(), identity); !errors.Is(err, ErrNotFound) {
		t.Fatal("dry run published receipt", err)
	}
	if ok, err := s.DispatchOperation(t.Context(), op.ID); err != nil || !ok {
		t.Fatal(ok, err)
	}
	created, err := s.CommitChildTransfer(t.Context(), op.ID, intent.Children)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created.Admission.Input.Parts, executed.Parts) {
		t.Fatal("lost canonical rewritten input", created)
	}
	if created.Session == nil || created.Session.ID != intent.ChildID || created.Admission.Receipt.RequestIdentity != identity {
		t.Fatal(created)
	}
	result, err := s.ChildTransferResult(t.Context(), identity, request)
	if err != nil || !reflect.DeepEqual(result, created) {
		t.Fatal(result, err)
	}
	matched, err := s.MatchChild(t.Context(), identity, request)
	if err != nil || matched.Input.ID != created.Admission.Input.ID {
		t.Fatal(matched, err)
	}
	private, err := s.Admission(t.Context(), accepted.Receipt.RequestIdentity)
	if err != nil || private.Input.ID != accepted.Input.ID {
		t.Fatal("private receipt reassigned", private, err)
	}
	if err := s.DeleteSubtree(t.Context(), created.Session.ID); err != nil {
		t.Fatal(err)
	}
	tombstone, err := s.ChildTransferResult(t.Context(), identity, request)
	if err != nil || tombstone.Session != nil || tombstone.Admission.Receipt.DeletedAt == nil {
		t.Fatal("deleted child recreated", tombstone, err)
	}
}

func TestPublicChildTransferTerminalWorkIsNeverMissingOrReplayed(t *testing.T) {
	for _, outcome := range []string{"cancelled", "deleted", "restart-before-dispatch", "restart-after-dispatch", "failed", "uncertain"} {
		t.Run(outcome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.db")
			s := openTest(t, path)
			owner, identity, request := publicTransfer(t, s)
			admitted, err := s.BeginChildTransfer(t.Context(), identity, request)
			if err != nil {
				t.Fatal(err)
			}
			want := ErrTransferCancelled
			switch outcome {
			case "cancelled":
				if _, err := s.CancelInput(t.Context(), admitted.Input.ID); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
					t.Fatal(err)
				}
				want = ErrTransferDeleted
			default:
				spec, _ := publicTransferOperation(t, s, owner, request)
				if outcome == "restart-before-dispatch" {
					want = ErrTransferInterrupted
				} else {
					approveDispatchBrowser(t, s, spec)
					if outcome == "failed" || outcome == "uncertain" {
						state := session.OperationFailed
						want = ErrTransferFailed
						if outcome == "uncertain" {
							state = session.OperationUncertain
							want = ErrTransferUncertain
						}
						if _, err := s.SettleOperation(t.Context(), spec.ID, session.OperationResult{State: state, Failure: new("fake native outcome")}); err != nil {
							t.Fatal(err)
						}
					} else {
						want = ErrTransferUncertain
					}
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s = openTest(t, path)
			if _, err := s.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := s.ChildTransferResult(t.Context(), identity, request); !errors.Is(err, want) {
					t.Fatal("terminal result", err, "want", want)
				}
				if _, err := s.MatchChild(t.Context(), identity, request); !errors.Is(err, want) {
					t.Fatal("terminal match", err, "want", want)
				}
				retried, err := s.BeginChildTransfer(t.Context(), identity, request)
				if err != nil || retried.Receipt.RequestIdentity != admitted.Receipt.RequestIdentity {
					t.Fatal("retry replaced accepted work", retried, err)
				}
			}
			if count(t, s, "sessions") > 1 {
				t.Fatal("terminal work created child")
			}
		})
	}
}
