package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

func controlChild(t *testing.T, s *Store, parent session.SessionID, key string) ChildAdmission {
	t.Helper()
	child, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: key}, ChildRequest{ParentID: parent, Parts: []session.Part{{Type: "text", Text: key}}})
	if err != nil {
		t.Fatal(err)
	}
	return child
}

func childControl(t *testing.T, s *Store, owner session.Session, cell session.Cell, key, capability string, request any) session.Operation {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	controlGrant(t, s, owner, "grant_"+key, capability, string(owner.TreeID))
	return admitOperation(t, s, session.OperationSpec{ID: session.OperationID(key), CellID: cell.ID, RequestID: key, Capability: capability, Resource: string(owner.TreeID), Arguments: raw})
}

func controlGrant(t *testing.T, s *Store, owner session.Session, key, capability, resource string) session.Grant {
	t.Helper()
	grant := session.Grant{ID: session.GrantID(key), SessionID: owner.ID, Capability: capability, Resource: resource}
	if owner.ParentID != nil {
		parent, err := s.Session(t.Context(), *owner.ParentID)
		if err != nil {
			t.Fatal(err)
		}
		issuer := controlGrant(t, s, parent, key+"_issuer", capability, resource)
		grant.IssuerID = &issuer.ID
	}
	result, err := s.CreateGrant(t.Context(), grant)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func applyControl(t *testing.T, s *Store, op session.Operation) ChildControlResult {
	t.Helper()
	result, err := s.ApplyChildControl(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestChildControlAtomicMutationAndStableRetry(t *testing.T) {
	for _, name := range []string{"submit", "stop", "delete"} {
		t.Run(name, func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			child := controlChild(t, s, owner.ID, "child")
			grandchild := controlChild(t, s, child.Session.ID, "grandchild")
			var request any = session.ChildTarget{SessionID: child.Session.ID}
			if name == "submit" {
				request = session.ChildSubmit{SessionID: child.Session.ID, Parts: []session.Part{{Type: "text", Text: "more"}}}
			}
			op := childControl(t, s, owner, cell, "control", "agents."+name, request)
			inputs, receipts := count(t, s, "inputs"), count(t, s, "receipts")
			execTest(t, s, `CREATE TRIGGER fail_child_control BEFORE UPDATE ON operations WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'injected settlement'); END`)
			if _, err := s.ApplyChildControl(t.Context(), op.ID); err == nil {
				t.Fatal("injected settlement failure succeeded")
			}
			for _, id := range []session.SessionID{child.Session.ID, grandchild.Session.ID} {
				current, err := s.Session(t.Context(), id)
				if err != nil || current.Lifecycle != session.Active {
					t.Fatalf("mutation survived rollback: %+v %v", current, err)
				}
			}
			if count(t, s, "inputs") != inputs || count(t, s, "receipts") != receipts {
				t.Fatal("input or receipt survived rollback")
			}
			pending, err := s.Operation(t.Context(), op.ID)
			if err != nil || pending.State != session.OperationReady || pending.DispatchedAt != nil {
				t.Fatalf("dispatch survived rollback: %+v %v", pending, err)
			}
			execTest(t, s, "DROP TRIGGER fail_child_control")
			var workers sync.WaitGroup
			results := make(chan ChildControlResult, 8)
			for range 8 {
				workers.Go(func() {
					result, err := s.ApplyChildControl(t.Context(), op.ID)
					if err != nil {
						t.Error(err)
						return
					}
					results <- result
				})
			}
			workers.Wait()
			close(results)
			var first json.RawMessage
			for result := range results {
				if first == nil {
					first = result.Value
				}
				if !bytes.Equal(first, result.Value) {
					t.Fatal("retry changed outcome")
				}
			}
			switch name {
			case "submit":
				if count(t, s, "inputs") != inputs+1 || count(t, s, "receipts") != receipts+1 {
					t.Fatal("submit duplicated work")
				}
			case "stop":
				for _, item := range []ChildAdmission{child, grandchild} {
					current, err := s.Session(t.Context(), item.Session.ID)
					if err != nil || current.Lifecycle != session.Stopped {
						t.Fatalf("descendant not stopped: %+v %v", current, err)
					}
					input, err := s.Input(t.Context(), item.Admission.Input.ID)
					if err != nil || input.State != session.Queued {
						t.Fatalf("stop discarded queued input: %+v %v", input, err)
					}
				}
			case "delete":
				if _, err := s.Session(t.Context(), child.Session.ID); !errors.Is(err, ErrNotFound) {
					t.Fatalf("child survived delete: %v", err)
				}
				admitted, err := s.Admission(t.Context(), child.Admission.Receipt.RequestIdentity)
				if err != nil || admitted.Input != nil || admitted.Receipt.DeletedAt == nil {
					t.Fatalf("deleted receipt not tombstoned: %+v %v", admitted, err)
				}
			}
		})
	}
}

func TestChildControlScopeRevocationAndBusyDelete(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	child := controlChild(t, s, owner.ID, "child")
	grandchild := controlChild(t, s, child.Session.ID, "grandchild")
	_, foreign := create(t, s, nil)
	for i, target := range []session.SessionID{owner.ID, foreign.ID} {
		for _, name := range []string{"submit", "stop", "delete"} {
			var request any = session.ChildTarget{SessionID: target}
			if name == "submit" {
				request = session.ChildSubmit{SessionID: target, Parts: []session.Part{{Type: "text", Text: "forbidden"}}}
			}
			op := childControl(t, s, owner, cell, fmt.Sprintf("scope_%s_%d", name, i), "agents."+name, request)
			if _, err := s.ApplyChildControl(t.Context(), op.ID); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("invalid target accepted: %v", err)
			}
		}
	}
	op := childControl(t, s, owner, cell, "grandchild_submit", "agents.submit", session.ChildSubmit{SessionID: grandchild.Session.ID, Parts: []session.Part{{Type: "text", Text: "forbidden"}}})
	if _, err := s.ApplyChildControl(t.Context(), op.ID); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("non-direct submit: %v", err)
	}
	op = childControl(t, s, owner, cell, "revoked", "agents.inspect", session.ChildInspect{SessionID: child.Session.ID, InputID: child.Admission.Input.ID})
	if _, err := s.RevokeGrant(t.Context(), *op.GrantID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyChildControl(t.Context(), op.ID); err == nil {
		t.Fatal("revoked grant dispatched")
	}
	turn := claim(t, s, grandchild.Session.ID).Turn
	deleteOp := childControl(t, s, owner, cell, "busy", "agents.delete", session.ChildTarget{SessionID: child.Session.ID})
	if _, err := s.ApplyChildControl(t.Context(), deleteOp.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("active descendant delete: %v", err)
	}
	stopped := applyControl(t, s, childControl(t, s, owner, cell, "stop", "agents.stop", session.ChildTarget{SessionID: child.Session.ID}))
	if len(stopped.CancelTurns) != 1 || stopped.CancelTurns[0] != turn.ID {
		t.Fatalf("runtime cancellation lost: %+v", stopped)
	}
	if _, err := s.ApplyChildControl(t.Context(), deleteOp.ID); !errors.Is(err, ErrBusy) {
		t.Fatalf("cancelling descendant delete: %v", err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Cancelled, nil, nil); err != nil {
		t.Fatal(err)
	}
	applyControl(t, s, deleteOp)
}

func TestChildInspectExactInputAndImmutableBoundedSnapshot(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	child := controlChild(t, s, owner.ID, "child")
	inspect := func(key string, input session.InputID) (session.ChildOutcome, session.Operation) {
		t.Helper()
		op := childControl(t, s, owner, cell, key, "agents.inspect", session.ChildInspect{SessionID: child.Session.ID, InputID: input})
		var result session.ChildOutcome
		if err := json.Unmarshal(applyControl(t, s, op).Value, &result); err != nil {
			t.Fatal(err)
		}
		return result, op
	}
	queued, old := inspect("queued", child.Admission.Input.ID)
	if queued.InputState != session.Queued || queued.TurnState != nil || queued.Text != "" {
		t.Fatalf("queued input reported outcome: %+v", queued)
	}
	turn := claim(t, s, child.Session.ID).Turn
	running, _ := inspect("running", child.Admission.Input.ID)
	if running.TurnState == nil || *running.TurnState != session.Running {
		t.Fatalf("running input reported outcome: %+v", running)
	}
	text := strings.Repeat("界", 6000)
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{{ID: "child_result", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: text}}}}); err != nil {
		t.Fatal(err)
	}
	finished, _ := inspect("finished", child.Admission.Input.ID)
	if finished.TurnState == nil || *finished.TurnState != session.Succeeded || !finished.Truncated || len(finished.Text) > 16*1024 || !utf8.ValidString(finished.Text) {
		t.Fatalf("invalid bounded output: %+v", finished)
	}
	var retry session.ChildOutcome
	if err := json.Unmarshal(applyControl(t, s, old).Value, &retry); err != nil || retry.TurnState != nil || retry.InputState != session.Queued {
		t.Fatalf("retry changed observation: %+v %v", retry, err)
	}
	next := submit(t, s, child.Session.ID, "next")
	nextOutcome, _ := inspect("next_queued", next.Input.ID)
	if nextOutcome.Text != "" || nextOutcome.TurnState != nil {
		t.Fatal("earlier success leaked into queued input")
	}
	assembled := finished.Text
	if finished.NextOffset == nil || finished.MessageID == nil || *finished.MessageID != "child_result" || finished.TotalBytes != int64(len(text)) {
		t.Fatalf("missing page identity: %+v", finished)
	}
	pageOp := childControl(t, s, owner, cell, "page_2", "agents.inspect", session.ChildInspect{SessionID: child.Session.ID, InputID: child.Admission.Input.ID, Offset: *finished.NextOffset})
	var page session.ChildOutcome
	if err := json.Unmarshal(applyControl(t, s, pageOp).Value, &page); err != nil {
		t.Fatal(err)
	}
	assembled += page.Text
	if assembled != text || page.NextOffset != nil || page.Truncated || *page.MessageID != *finished.MessageID {
		t.Fatalf("exact-input pagination changed text: %+v", page)
	}
	badOffset := childControl(t, s, owner, cell, "mid_rune", "agents.inspect", session.ChildInspect{SessionID: child.Session.ID, InputID: child.Admission.Input.ID, Offset: 1})
	if _, err := s.ApplyChildControl(t.Context(), badOffset.ID); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("mid-rune offset: %v", err)
	}
	op := childControl(t, s, owner, cell, "wrong_input", "agents.inspect", session.ChildInspect{SessionID: child.Session.ID, InputID: "missing"})
	if _, err := s.ApplyChildControl(t.Context(), op.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing input: %v", err)
	}
}

func TestChildControlsRelativesDoNotAuthorizeSiblingOrAncestorMutation(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	actor := controlChild(t, s, root.ID, "actor")
	sibling := controlChild(t, s, root.ID, "sibling")
	child := controlChild(t, s, actor.Session.ID, "child")
	turn := claim(t, s, actor.Session.ID).Turn
	message, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: "actor_call", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute", Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := s.BeginCell(t.Context(), session.CellSpec{ID: "actor_cell", TurnID: turn.ID, CallMessageID: message.ID, CallID: "execute"})
	if err != nil || !dispatch {
		t.Fatalf("actor cell: %v %v", dispatch, err)
	}
	for _, relation := range []struct {
		name string
		want session.SessionID
	}{{"parent", root.ID}, {"siblings", sibling.Session.ID}, {"children", child.Session.ID}} {
		op := childControl(t, s, *actor.Session, cell, "list_"+relation.name, "agents.list", session.ChildList{Relation: relation.name, Limit: 1})
		var result struct {
			Items []session.RelativeMetadata `json:"items"`
		}
		if err := json.Unmarshal(applyControl(t, s, op).Value, &result); err != nil || len(result.Items) != 1 || result.Items[0].SessionID != relation.want {
			t.Fatalf("relatives %s: %+v %v", relation.name, result, err)
		}
		next := childControl(t, s, *actor.Session, cell, "after_"+relation.name, "agents.list", session.ChildList{Relation: relation.name, After: relation.want, Limit: 1})
		if err := json.Unmarshal(applyControl(t, s, next).Value, &result); err != nil || len(result.Items) != 0 {
			t.Fatalf("relatives cursor %s: %+v %v", relation.name, result, err)
		}
	}
	for i, target := range []session.SessionID{root.ID, sibling.Session.ID} {
		for _, name := range []string{"stop", "delete", "submit", "inspect"} {
			var request any = session.ChildTarget{SessionID: target}
			switch name {
			case "submit":
				request = session.ChildSubmit{SessionID: target, Parts: []session.Part{{Type: "text", Text: "forbidden"}}}
			case "inspect":
				request = session.ChildInspect{SessionID: target, InputID: sibling.Admission.Input.ID}
			}
			op := childControl(t, s, *actor.Session, cell, fmt.Sprintf("relative_%s_%d", name, i), "agents."+name, request)
			if _, err := s.ApplyChildControl(t.Context(), op.ID); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("relative %s escaped descendant scope: %v", name, err)
			}
		}
	}
	// Exact tree scope is also required even when a grant exists for another
	// resource string and the requested target is a legitimate child.
	controlGrant(t, s, *actor.Session, "wrong_resource", "agents.stop", "other_tree")
	raw, _ := json.Marshal(session.ChildTarget{SessionID: child.Session.ID})
	op := admitOperation(t, s, session.OperationSpec{ID: "wrong_tree", CellID: cell.ID, RequestID: "wrong_tree", Capability: "agents.stop", Resource: "other_tree", Arguments: raw})
	if _, err := s.ApplyChildControl(t.Context(), op.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("wrong tree-scoped resource accepted: %v", err)
	}
}

func TestChildStopRetryCannotCancelWorkAfterReactivation(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	child := controlChild(t, s, owner.ID, "child")
	first := claim(t, s, child.Session.ID).Turn
	op := childControl(t, s, owner, cell, "stop_retry", "agents.stop", session.ChildTarget{SessionID: child.Session.ID})
	stopped := applyControl(t, s, op)
	if len(stopped.CancelTurns) != 1 || stopped.CancelTurns[0] != first.ID {
		t.Fatalf("first cancellation lost: %+v", stopped)
	}
	if _, err := s.Finish(t.Context(), first.ID, session.Cancelled, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetLifecycle(t.Context(), child.Session.ID, session.Active); err != nil {
		t.Fatal(err)
	}
	submit(t, s, child.Session.ID, "later")
	later := claim(t, s, child.Session.ID).Turn
	if _, err := s.RevokeGrant(t.Context(), *op.GrantID); err != nil {
		t.Fatal(err)
	}
	retried := applyControl(t, s, op)
	if !bytes.Equal(stopped.Value, retried.Value) || len(retried.CancelTurns) != 0 {
		t.Fatalf("terminal stop retry affected new execution: %+v", retried)
	}
	current, err := s.Turn(t.Context(), later.ID)
	if err != nil || current.State != session.Running {
		t.Fatalf("new turn stopped: %+v %v", current, err)
	}
}

func TestChildSubmitSharesOnlyOwnedContentAndInspectOmitsReferences(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	child := controlChild(t, s, owner.ID, "child")
	reference, err := s.RegisterContent(t.Context(), contentReference(owner.ID, "owned_content", "payload"))
	if err != nil {
		t.Fatal(err)
	}
	op := childControl(t, s, owner, cell, "content_submit", "agents.submit", session.ChildSubmit{SessionID: child.Session.ID, Parts: []session.Part{{Type: "content", ReferenceID: reference.ID}}})
	var admitted session.ChildSubmission
	if err := json.Unmarshal(applyControl(t, s, op).Value, &admitted); err != nil {
		t.Fatal(err)
	}
	input, err := s.Input(t.Context(), admitted.InputID)
	if err != nil || input.Parts[0].ReferenceID == reference.ID {
		t.Fatalf("parent reference identity leaked: %+v %v", input, err)
	}
	if _, err := readContent(t.Context(), s.db, child.Session.ID, input.Parts[0].ReferenceID); err != nil {
		t.Fatal(err)
	}
	first := claim(t, s, child.Session.ID).Turn
	if _, err := s.Finish(t.Context(), first.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	turn := claim(t, s, child.Session.ID).Turn
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{{ID: "content_result", Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: "summary"}, {Type: "content", ReferenceID: input.Parts[0].ReferenceID}}}}); err != nil {
		t.Fatal(err)
	}
	inspected := applyControl(t, s, childControl(t, s, owner, cell, "content_inspect", "agents.inspect", session.ChildInspect{SessionID: child.Session.ID, InputID: input.ID}))
	var outcome session.ChildOutcome
	if err := json.Unmarshal(inspected.Value, &outcome); err != nil || outcome.Text != "summary" || outcome.OmittedParts != 1 || bytes.Contains(inspected.Value, []byte(input.Parts[0].ReferenceID)) {
		t.Fatalf("inspection exposed child reference: %s %v", inspected.Value, err)
	}
	foreign := childControl(t, s, owner, cell, "foreign_content", "agents.submit", session.ChildSubmit{SessionID: child.Session.ID, Parts: []session.Part{{Type: "content", ReferenceID: input.Parts[0].ReferenceID}}})
	if _, err := s.ApplyChildControl(t.Context(), foreign.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("child-owned content accepted as parent authority: %v", err)
	}
}
