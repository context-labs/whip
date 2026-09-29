package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func questionSpec(t *testing.T, cell session.Cell, key string, batch bool) session.OperationSpec {
	t.Helper()
	request := session.QuestionRequest{Questions: []session.QuestionSet{{Question: "Choose", Options: []session.QuestionOption{{Label: "A", Recommended: true}, {Label: "B"}}}}, Batch: batch}
	if batch {
		request.Questions = append(request.Questions, session.QuestionSet{Question: "Anything else?", Options: []session.QuestionOption{{Label: "C"}, {Label: "D"}}, Multiple: true})
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return session.OperationSpec{ID: session.OperationID(key), CellID: cell.ID, RequestID: key, Capability: session.QuestionCapability, Resource: string(cell.SessionID), Arguments: raw}
}

func beginQuestion(t *testing.T, s *Store, cell session.Cell, key string, batch bool) session.Question {
	t.Helper()
	operation := admitOperation(t, s, questionSpec(t, cell, key, batch))
	if operation.State != session.OperationReady || operation.GrantID != nil {
		t.Fatalf("question required separate permission: %+v", operation)
	}
	question, err := s.BeginQuestion(t.Context(), operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	return question
}

func TestQuestionAdmissionSettlementAndExactRetry(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(strconv.FormatBool(batch), func(t *testing.T) {
			s := fresh(t)
			owner, cell := operationCell(t, s)
			question := beginQuestion(t, s, cell, "ask", batch)
			if question.State != session.QuestionPending || question.OperationID != "ask" || question.SessionID != owner.ID || question.CellID != cell.ID || question.TurnID != cell.TurnID || question.Deadline.Sub(question.CreatedAt) != 5*time.Minute || count(t, s, "permissions") != 0 || count(t, s, "grants") != 0 {
				t.Fatalf("pending question identity/deadline: %+v", question)
			}
			retry, err := s.BeginQuestion(t.Context(), "ask")
			if err != nil || !reflect.DeepEqual(question, retry) {
				t.Fatalf("begin retry extended deadline: %+v %v", retry, err)
			}
			next := admitOperation(t, s, questionSpec(t, cell, "next", false))
			if _, err := s.BeginQuestion(t.Context(), next.ID); !errors.Is(err, ErrBusy) {
				t.Fatalf("second question began: %v", err)
			}
			if _, err := s.AnswerQuestion(t.Context(), "foreign", "ask", []session.QuestionAnswer{{Answer: []string{"A"}}}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("foreign owner answered: %v", err)
			}
			if _, err := s.AnswerQuestion(t.Context(), owner.ID, "ask", nil); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("invalid answer settled: %v", err)
			}
			answers := []session.QuestionAnswer{{Answer: []string{"free text"}}}
			if batch {
				answers = append(answers, session.QuestionAnswer{Answer: []string{"ignored"}, Dismissed: true})
			}
			result, err := s.AnswerQuestion(t.Context(), owner.ID, "ask", answers)
			if err != nil || result.State != session.QuestionAnswered || result.ClosedAt == nil || result.CloseReason != nil || result.Answers[0].Answer[0] != "free text" {
				t.Fatalf("answer: %+v %v", result, err)
			}
			operation, err := s.Operation(t.Context(), "ask")
			if err != nil || operation.State != session.OperationSucceeded || operation.Result == nil || operation.DispatchedAt == nil || operation.GrantID != nil {
				t.Fatalf("ordinary result not atomic: %+v %v", operation, err)
			}
			want, _ := question.Request.AnswerValue(answers)
			if string(operation.Result.Value) != string(want) {
				t.Fatalf("stored answer: %s want %s", operation.Result.Value, want)
			}
			if _, err := s.CancelTurn(t.Context(), cell.TurnID); err != nil {
				t.Fatal(err)
			}
			again, err := s.AnswerQuestion(t.Context(), owner.ID, "ask", answers)
			if err != nil || !reflect.DeepEqual(result, again) {
				t.Fatalf("lost acknowledgement retry did live checks: %+v %v", again, err)
			}
			answers[0].Answer[0] = "changed"
			if _, err := s.AnswerQuestion(t.Context(), owner.ID, "ask", answers); !errors.Is(err, ErrConflict) {
				t.Fatalf("answer mutated: %v", err)
			}
			closed, err := s.CloseQuestion(t.Context(), owner.ID, "ask", session.QuestionCancelled)
			if err != nil || !reflect.DeepEqual(result, closed) {
				t.Fatalf("late cancellation overwrote answer: %+v %v", closed, err)
			}
			pending, err := s.Questions(t.Context(), owner.ID, true, "", 100)
			if err != nil || len(pending) != 0 {
				t.Fatalf("answered question remained pending: %+v %v", pending, err)
			}
		})
	}
}

func TestQuestionAuthorityCannotBeWidenedByGrantsOrDispatchEntryPoint(t *testing.T) {
	s := fresh(t)
	root, cell := operationCell(t, s)
	bad := questionSpec(t, cell, "wrong-scope", false)
	bad.Resource = string(root.TreeID)
	if _, err := s.AdmitOperation(t.Context(), bad); !errors.Is(err, ErrConflict) {
		t.Fatalf("question accepted tree/foreign scope: %v", err)
	}
	bad = questionSpec(t, cell, "bad-request", false)
	bad.Arguments = json.RawMessage(`{"questions":[],"batch":true}`)
	if _, err := s.AdmitOperation(t.Context(), bad); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("invalid intent got intrinsic authority: %v", err)
	}
	op := admitOperation(t, s, questionSpec(t, cell, "dispatch", false))
	if allowed, err := s.DispatchOperation(t.Context(), op.ID); allowed || !errors.Is(err, ErrConflict) {
		t.Fatalf("generic dispatch omitted pending evidence: %v %v", allowed, err)
	}
	question, err := s.BeginQuestion(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleOperation(t.Context(), op.ID, session.OperationResult{State: session.OperationSucceeded, Value: json.RawMessage(`{}`)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("generic settlement forged answer: %v", err)
	}
	if _, err := s.CloseQuestion(t.Context(), root.ID, question.OperationID, session.QuestionCancelled); err != nil {
		t.Fatal(err)
	}
	child := spawnChildTest(t, s, "child", childRequest(root.ID))
	childCell := childOperationCell(t, s, child.Session.ID)
	issuer, err := s.CreateGrant(t.Context(), session.Grant{ID: "issuer", SessionID: root.ID, Capability: session.QuestionCapability, Resource: string(child.Session.ID)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateGrant(t.Context(), session.Grant{ID: "delegated", SessionID: child.Session.ID, Capability: issuer.Capability, Resource: issuer.Resource, IssuerID: &issuer.ID}); err != nil {
		t.Fatal(err)
	}
	denied := admitOperation(t, s, questionSpec(t, childCell, "child-ask", false))
	if denied.State != session.OperationDenied || denied.GrantID != nil || denied.Result == nil || denied.Result.Failure == nil || *denied.Result.Failure != "only the root agent can ask the user; send your parent a message instead" {
		t.Fatalf("child delegated grant bypassed root-only restriction: %+v", denied)
	}
	if _, err := s.BeginQuestion(t.Context(), denied.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("child question began: %v", err)
	}
	if count(t, s, "questions") != 1 || count(t, s, "permissions") != 0 {
		t.Fatal("child asked human or requested another permission")
	}
	stopped := admitOperation(t, s, questionSpec(t, cell, "stopped", false))
	if _, err := s.CancelTurn(t.Context(), cell.TurnID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginQuestion(t.Context(), stopped.ID); !errors.Is(err, ErrStopped) {
		t.Fatalf("dispatch did not recheck live cell: %v", err)
	}
}

func TestQuestionTransactionsRollbackAndBoundAdmissions(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	op := admitOperation(t, s, questionSpec(t, cell, "ask", false))
	execTest(t, s, `CREATE TRIGGER fail_question BEFORE INSERT ON questions BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := s.BeginQuestion(t.Context(), op.ID); err == nil {
		t.Fatal("injected admission failure succeeded")
	}
	current, _ := s.Operation(t.Context(), op.ID)
	if current.State != session.OperationReady || current.DispatchedAt != nil || count(t, s, "questions") != 0 {
		t.Fatalf("failed begin committed dispatch: %+v", current)
	}
	execTest(t, s, "DROP TRIGGER fail_question")
	question, err := s.BeginQuestion(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	execTest(t, s, `CREATE TRIGGER fail_answer BEFORE UPDATE ON operations WHEN NEW.state IN ('succeeded','failed') BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if _, err := s.AnswerQuestion(t.Context(), owner.ID, op.ID, []session.QuestionAnswer{{Dismissed: true}}); err == nil {
		t.Fatal("injected answer failure succeeded")
	}
	if _, err := s.CloseQuestion(t.Context(), owner.ID, op.ID, session.QuestionCancelled); err == nil {
		t.Fatal("injected closure failure succeeded")
	}
	currentQuestion, err := s.Question(t.Context(), owner.ID, op.ID)
	if err != nil || !reflect.DeepEqual(question, currentQuestion) {
		t.Fatalf("failed settlement partially closed question: %+v %v", currentQuestion, err)
	}
	execTest(t, s, "DROP TRIGGER fail_answer")
	dismissed, err := s.AnswerQuestion(t.Context(), owner.ID, op.ID, []session.QuestionAnswer{{Dismissed: true}})
	if err != nil || dismissed.State != session.QuestionDismissed {
		t.Fatalf("dismissal failed: %+v %v", dismissed, err)
	}
	for i := 1; i < session.MaxQuestionsPerTurn; i++ {
		spec := questionSpec(t, cell, fmt.Sprintf("question-%02d", i), false)
		admitOperation(t, s, spec)
	}
	if _, err := s.AdmitOperation(t.Context(), questionSpec(t, cell, "over-limit", false)); !errors.Is(err, ErrLimit) {
		t.Fatalf("question count unbounded: %v", err)
	}
	if retry := admitOperation(t, s, op.OperationSpec); retry.State != session.OperationSucceeded {
		t.Fatal("question count prevented exact admission retry")
	}
}

func TestQuestionCancellationAnswerRaceAndReadSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s, other := openTest(t, path), openTest(t, path)
	owner, cell := operationCell(t, s)
	for i := range 12 {
		question := beginQuestion(t, s, cell, fmt.Sprintf("race-%02d", i), false)
		var group sync.WaitGroup
		group.Go(func() {
			_, err := other.AnswerQuestion(t.Context(), owner.ID, question.OperationID, []session.QuestionAnswer{{Answer: []string{"A"}}})
			if err != nil && !errors.Is(err, ErrConflict) {
				t.Error(err)
			}
		})
		group.Go(func() {
			if _, err := s.CloseQuestion(t.Context(), owner.ID, question.OperationID, session.QuestionCancelled); err != nil {
				t.Error(err)
			}
		})
		for range 3 {
			items, err := s.Questions(t.Context(), owner.ID, false, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if (item.State == session.QuestionPending) != (item.ClosedAt == nil) || (item.State == session.QuestionAnswered) != (len(item.Answers) != 0) || (item.State == session.QuestionClosed) != (item.CloseReason != nil) {
					t.Fatalf("torn question projection: %+v", item)
				}
			}
		}
		group.Wait()
		final, err := s.Question(t.Context(), owner.ID, question.OperationID)
		if err != nil || (final.State != session.QuestionAnswered && final.State != session.QuestionClosed) {
			t.Fatalf("unresolved race: %+v %v", final, err)
		}
	}
}

func TestQuestionExpiryAndRecoveryNeverResumeWaiters(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	expired := beginQuestion(t, s, cell, "expired", false)
	if _, err := s.CloseQuestion(t.Context(), owner.ID, expired.OperationID, session.QuestionExpired); !errors.Is(err, ErrConflict) {
		t.Fatalf("early expiration succeeded: %v", err)
	}
	// Backdate only this test fixture. Production deadlines are immutable and
	// derived from the dispatch transaction, never supplied by a client.
	execTest(t, s, "DROP TRIGGER question_transition")
	past := time.Now().Add(-6 * time.Minute).UnixMicro()
	execTest(t, s, "UPDATE questions SET created_at=?,deadline=? WHERE operation_id=?", past, past+session.QuestionWait.Microseconds(), expired.OperationID)
	if _, err := s.AnswerQuestion(t.Context(), owner.ID, expired.OperationID, []session.QuestionAnswer{{Answer: []string{"A"}}}); !errors.Is(err, ErrStopped) {
		t.Fatalf("late answer accepted: %v", err)
	}
	closed, err := s.Question(t.Context(), owner.ID, expired.OperationID)
	if err != nil || closed.CloseReason == nil || *closed.CloseReason != session.QuestionExpired || closed.State != session.QuestionClosed {
		t.Fatalf("expiration not committed: %+v %v", closed, err)
	}
	answered := beginQuestion(t, s, cell, "answered", false)
	answer := []session.QuestionAnswer{{Answer: []string{"A"}}}
	settled, err := s.AnswerQuestion(t.Context(), owner.ID, answered.OperationID, answer)
	if err != nil {
		t.Fatal(err)
	}
	interrupted := beginQuestion(t, s, cell, "interrupted", false)
	standingGrant(t, s, owner.ID, "other-operation-grant")
	ordinary := admitOperation(t, s, operationSpec(cell, "ordinary"))
	if allowed, err := s.DispatchOperation(t.Context(), ordinary.ID); !allowed || err != nil {
		t.Fatalf("ordinary dispatch: %v %v", allowed, err)
	}
	if _, err := s.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Question(t.Context(), owner.ID, interrupted.OperationID)
	if err != nil || recovered.State != session.QuestionClosed || recovered.CloseReason == nil || *recovered.CloseReason != session.QuestionInterrupted || recovered.ClosedAt == nil {
		t.Fatalf("question recovery: %+v %v", recovered, err)
	}
	if _, err := s.AnswerQuestion(t.Context(), owner.ID, interrupted.OperationID, []session.QuestionAnswer{{Answer: []string{"A"}}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("recovered waiter was resumed: %v", err)
	}
	unchanged, err := s.Operation(t.Context(), ordinary.ID)
	if err != nil || unchanged.State != session.OperationUncertain {
		t.Fatalf("question recovery changed other effects: %+v %v", unchanged, err)
	}
	pending, err := s.Questions(t.Context(), owner.ID, true, "", 100)
	if err != nil || len(pending) != 0 {
		t.Fatalf("recovery left pending waiter: %+v %v", pending, err)
	}
	if again, err := s.AnswerQuestion(t.Context(), owner.ID, answered.OperationID, answer); err != nil || !reflect.DeepEqual(settled, again) {
		t.Fatalf("recovery lost exact answer acknowledgement: %+v %v", again, err)
	}
	if again, err := s.BeginQuestion(t.Context(), interrupted.OperationID); err != nil || !reflect.DeepEqual(recovered, again) {
		t.Fatalf("begin retry revived recovered waiter: %+v %v", again, err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "questions") != 0 {
		t.Fatal("deleted operations left question metadata")
	}
}
