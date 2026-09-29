package rpc_test

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestQuestionRPCProjectsActualOperationAndStableAnswer(t *testing.T) {
	r, c := fixture(t)
	root := create(t, c).Root.ID
	if page := call[protocol.QuestionsResult](t, c, "questions.list", protocol.QuestionsParams{SessionID: root, PendingOnly: true, Limit: 100}); len(page.Items) != 0 {
		t.Fatal("question read admitted work")
	}
	call[protocol.Admission](t, c, "sessions.submit", protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "question", RequestID: "seed"}, SessionID: root, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "ask"}}})
	// Seed a real admitted turn/cell/operation through production store methods;
	// both-engine execution and crash recovery are covered by process acceptance.
	db, err := store.Open(t.Context(), filepath.Join(filepath.Dir(r.SocketPath()), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	claimed, err := db.Claim(t.Context(), session.SessionID(root))
	if err != nil {
		t.Fatal(err)
	}
	message, err := db.AppendMessage(t.Context(), claimed.Turn.ID, session.MessageDraft{ID: "question-call", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute", Name: "execute", Arguments: json.RawMessage(`{"code":"user.ask(...)"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	cell, dispatch, err := db.BeginCell(t.Context(), session.CellSpec{ID: "question-cell", TurnID: claimed.Turn.ID, CallMessageID: message.ID, CallID: "execute"})
	if err != nil || !dispatch {
		t.Fatalf("cell: %+v %v %v", cell, dispatch, err)
	}
	arguments, err := json.Marshal(session.QuestionRequest{Questions: []session.QuestionSet{{Question: "Choose", Options: []session.QuestionOption{{Label: "A", Recommended: true}, {Label: "B"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := db.AdmitOperation(t.Context(), session.OperationSpec{ID: "question-operation", CellID: cell.ID, RequestID: "ask", Capability: session.QuestionCapability, Resource: string(root), Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginQuestion(t.Context(), operation.ID); err != nil {
		t.Fatal(err)
	}
	params := protocol.QuestionParams{SessionID: root, OperationID: protocol.ID(operation.ID)}
	pending := call[protocol.Question](t, c, "questions.get", params)
	if pending.State != "pending" || pending.ClosedAt != nil || pending.CloseReason != nil || pending.CellID != protocol.ID(cell.ID) || pending.TurnID != protocol.ID(claimed.Turn.ID) || pending.Answers == nil || len(pending.Answers) != 0 || !pending.Request.Questions[0].Options[0].Recommended {
		t.Fatalf("pending question lost durable shape: %+v", pending)
	}
	page := call[protocol.QuestionsResult](t, c, "questions.list", protocol.QuestionsParams{SessionID: root, PendingOnly: true, Limit: 1})
	if len(page.Items) != 1 || !reflect.DeepEqual(page.Items[0], pending) {
		t.Fatal("pending read disagreed with exact evidence")
	}
	if permissions := call[protocol.PermissionsResult](t, c, "permissions.list", protocol.PermissionsParams{SessionID: root, Limit: 100}); len(permissions.Items) != 0 {
		t.Fatal("user.ask produced a permission dialog")
	}
	answer := protocol.AnswerQuestionParams{SessionID: root, OperationID: params.OperationID, Answers: []protocol.QuestionAnswer{{Answer: []string{"other route"}}}}
	accepted := call[protocol.Question](t, c, "questions.answer", answer)
	if accepted.State != "answered" || accepted.Answers[0].Answer[0] != "other route" || accepted.ClosedAt == nil {
		t.Fatalf("answer: %+v", accepted)
	}
	stored := call[protocol.HostOperation](t, c, "operations.get", protocol.HostOperationParams{OperationID: params.OperationID})
	if stored.State != "succeeded" || stored.GrantID != nil || stored.Result == nil || string(stored.Result.Value) != `{"answer":["other route"],"dismissed":false}` {
		t.Fatalf("answer bypassed operation result: %+v", stored)
	}
	call[protocol.Turn](t, c, "turns.cancel", protocol.TurnParams{TurnID: protocol.ID(claimed.Turn.ID)})
	if again := call[protocol.Question](t, c, "questions.answer", answer); !reflect.DeepEqual(accepted, again) {
		t.Fatal("retry performed live checks or changed evidence")
	}
	answer.Answers[0].Answer[0] = "changed"
	var rejected protocol.Question
	var remote *client.Error
	if err := c.Call(t.Context(), "questions.answer", answer, &rejected); !errors.As(err, &remote) || remote.Kind != "CONFLICT" {
		t.Fatalf("changed answer did not conflict: %v", err)
	}
	params.SessionID = "foreign"
	if err := c.Call(t.Context(), "questions.get", params, &rejected); !errors.As(err, &remote) || remote.Kind != "NOT_FOUND" {
		t.Fatalf("question escaped session scope: %v", err)
	}
	if final := call[protocol.QuestionsResult](t, c, "questions.list", protocol.QuestionsParams{SessionID: root, PendingOnly: true, Limit: 100}); len(final.Items) != 0 {
		t.Fatal("answered question retained a pending waiter")
	}
}
