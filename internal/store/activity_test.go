package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

func TestActivityDistinguishesQueuedWaitingAndSettledWithoutClaiming(t *testing.T) {
	s := fresh(t)
	_, root := create(t, s, nil)
	child := controlChild(t, s, root.ID, "child")
	owner := child.Session.ID
	input := submit(t, s, owner, "later")
	before := count(t, s, "turns")
	activity, err := s.Activity(t.Context(), owner)
	if err != nil || activity.ActiveTurn != nil || activity.QueuedInputCount != 2 || activity.ExecutionPermit || count(t, s, "turns") != before {
		t.Fatal(activity, err)
	}
	active := claim(t, s, owner)
	activity, err = s.Activity(t.Context(), owner)
	if err != nil || activity.ActiveTurn == nil || activity.ActiveTurn.ID != active.Turn.ID || activity.ActiveTurn.ConfigRevision != active.Turn.ConfigRevision || activity.ActiveInputID == nil || *activity.ActiveInputID != active.Input.ID || activity.QueuedInputCount != 1 || !activity.ExecutionPermit {
		t.Fatal(activity, err)
	}
	if err := s.YieldTurn(t.Context(), active.Turn.ID); err != nil {
		t.Fatal(err)
	}
	activity, err = s.Activity(t.Context(), owner)
	if err != nil || activity.ActiveTurn == nil || activity.ActiveTurn.State != session.Running || activity.ExecutionPermit {
		t.Fatal("waiting work appeared idle", activity, err)
	}
	if _, err := s.Finish(t.Context(), active.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CancelInput(t.Context(), input.Input.ID); err != nil {
		t.Fatal(err)
	}
	activity, err = s.Activity(t.Context(), owner)
	if err != nil || activity.ActiveTurn != nil || activity.QueuedInputCount != 0 || activity.ExecutionPermit {
		t.Fatal(activity, err)
	}
	if _, err := s.Activity(t.Context(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestActivityCountsExactOwnerHumanDecisionsForCellAndDirectWork(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	pending := admitOperation(t, s, operationSpec(cell, "pending"))
	question := beginQuestion(t, s, cell, "question", false)
	activity, err := s.Activity(t.Context(), owner.ID)
	if err != nil || activity.PendingPermissionCount != 1 || activity.PendingQuestionCount != 1 {
		t.Fatal(activity, err)
	}
	_, other := create(t, s, nil)
	if value, err := s.Activity(t.Context(), other.ID); err != nil || value.PendingPermissionCount != 0 || value.PendingQuestionCount != 0 {
		t.Fatal(value, err)
	}
	if _, err := s.ResolvePermission(t.Context(), pending.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AnswerQuestion(t.Context(), owner.ID, question.OperationID, []session.QuestionAnswer{{Answer: []string{"A"}}}); err != nil {
		t.Fatal(err)
	}
	if value, err := s.Activity(t.Context(), owner.ID); err != nil || value.PendingPermissionCount != 0 || value.PendingQuestionCount != 0 {
		t.Fatal(value, err)
	}
	admission, err := s.AdmitHostOperation(t.Context(), session.RequestIdentity{ClientID: "human", RequestID: "direct"}, other.ID, session.HostOperation{Module: "shell", Name: "run", Arguments: json.RawMessage(`{"command":"echo example"}`)})
	if err != nil {
		t.Fatal(err)
	}
	turn := claim(t, s, other.ID).Turn
	admitOperation(t, s, session.OperationSpec{ID: "direct-op", DirectTurnID: turn.ID, RequestID: string(admission.Input.ID), Capability: "shell.run", Resource: "workspace", Arguments: json.RawMessage(`{"command":"echo example"}`)})
	if value, err := s.Activity(t.Context(), other.ID); err != nil || value.PendingPermissionCount != 1 || value.ActiveTurn == nil || value.ActiveTurn.Kind != session.HostOperationInputKind || value.ActiveInputID == nil || *value.ActiveInputID != admission.Input.ID {
		t.Fatal(value, err)
	}
}

func TestInputPagesBoundPreviewPreserveOrdinalAndRequireExplicitBodyRead(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	const base = int64(9007199254740992)
	execTest(t, s, "INSERT INTO sqlite_sequence(name,seq) VALUES('inputs',?)", base)
	text := strings.Repeat("界", 4000)
	admission, err := s.Admit(t.Context(), session.RequestIdentity{ClientID: "reader", RequestID: "large"}, Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: text}, {Type: "text", Text: "second part"}}})
	if err != nil {
		t.Fatal(err)
	}
	second := submit(t, s, owner.ID, "second")
	first, err := s.InputPage(t.Context(), owner.ID, "queued", 0, 1)
	if err != nil || len(first.Items) != 1 || first.NextCursor == nil || *first.NextCursor != base+1 || first.Items[0].Ordinal != base+1 || !first.Items[0].PreviewTruncated || utf8.RuneCountInString(first.Items[0].TextPreview) != 512 {
		t.Fatal(first, err)
	}
	raw, _ := json.Marshal(first)
	if len(raw) > 4096 {
		t.Fatal("page retained full input", len(raw))
	}
	tail, err := s.InputPage(t.Context(), owner.ID, "queued", *first.NextCursor, 1)
	if err != nil || len(tail.Items) != 1 || tail.Items[0].ID != second.Input.ID || tail.NextCursor != nil {
		t.Fatal(tail, err)
	}
	full, err := s.SessionInput(t.Context(), owner.ID, admission.Input.ID)
	if err != nil || full.Parts[0].Text != text || len(full.Parts) != 2 {
		t.Fatal("explicit payload changed", err)
	}
	if _, err := s.SessionInput(t.Context(), "foreign", admission.Input.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.CancelInput(t.Context(), second.Input.ID); err != nil {
		t.Fatal(err)
	}
	filtered, err := s.InputPage(t.Context(), owner.ID, "queued", *first.NextCursor, 10)
	if err != nil || len(filtered.Items) != 0 {
		t.Fatal(filtered, err)
	}
	all, err := s.InputPage(t.Context(), owner.ID, "all", *first.NextCursor, 10)
	if err != nil || len(all.Items) != 1 || all.Items[0].State != session.InputCancelled {
		t.Fatal(all, err)
	}
	if _, err := s.InputPage(t.Context(), "missing", "all", 0, 10); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
