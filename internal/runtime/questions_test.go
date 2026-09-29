package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/engine/process"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func awaitRuntimeQuestion(t *testing.T, r *Runtime, owner session.SessionID, key string) session.Question {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		items, err := r.Questions(ctx, owner, true, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) == 1 {
			return items[0]
		}
		admission, err := r.Admission(ctx, session.RequestIdentity{ClientID: "test", RequestID: key})
		if err != nil {
			t.Fatal(err)
		}
		if terminal(admission) {
			t.Fatalf("turn ended before question: %+v runtime=%v", admission.Turn, r.Err())
		}
		select {
		case <-ctx.Done():
			t.Fatalf("timed out waiting for human question: %v", r.Err())
		case <-ticker.C:
		}
	}
}

func questionCellResult(t *testing.T, r *Runtime, question session.Question, key string) process.Result {
	t.Helper()
	finished := waitTestWithin(t, r, key, terminal, 30*time.Second)
	if finished.Turn.State != session.Succeeded {
		t.Fatalf("question turn: %+v runtime=%v", finished.Turn, r.Err())
	}
	cell, err := r.Cell(t.Context(), question.CellID)
	if err != nil || cell.State != session.CellSucceeded || cell.ResultMessageID == nil || cell.Checkpoint == nil {
		t.Fatalf("question escaped the real cell boundary: %+v %v", cell, err)
	}
	history, err := r.History(t.Context(), question.SessionID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Result process.Result `json:"result"`
	}
	if err := json.Unmarshal([]byte(history[len(history)-1].Parts[0].Text), &output); err != nil {
		t.Fatal(err)
	}
	return output.Result
}

func TestBothEnginesHumanQuestionSingleBatchAndDismissal(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{
				"single":  `answer = user.ask(question="Choose", options=[{"label":"A","recommended":True},{"label":"B"}])` + "\n" + `print(answer["answer"][0])`,
				"batch":   `answer = user.ask(questions=[{"question":"First", "options":[{"label":"A"},{"label":"B"}], "multiple":True}, {"question":"Second", "options":[{"label":"C"},{"label":"D"}]}])` + "\n" + `print(answer["answers"][0]["answer"][1])` + "\n" + `print(answer["answers"][1]["dismissed"])`,
				"dismiss": `answer = user.ask(questions=[{"question":"Only", "options":[{"label":"A"},{"label":"B"}]}])` + "\n" + `print(answer["dismissed"])` + "\n" + `print(len(answer["answers"]))`,
			}
			if engine == session.QuickJS {
				codes = map[string]string{
					"single":  `var answer = await user.ask({question:'Choose',options:[{label:'A',recommended:true},{label:'B'}]}); console.log(answer.answer[0]);`,
					"batch":   `var answer = await user.ask({questions:[{question:'First',options:[{label:'A'},{label:'B'}],multiple:true},{question:'Second',options:[{label:'C'},{label:'D'}]}]}); console.log(answer.answers[0].answer[1]); console.log(answer.answers[1].dismissed);`,
					"dismiss": `var answer = await user.ask({questions:[{question:'Only',options:[{label:'A'},{label:'B'}]}]}); console.log(answer.dismissed); console.log(answer.answers.length);`,
				}
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(codes))
			owner := createEngineSession(t, r, engine)
			for _, key := range []string{"single", "batch", "dismiss"} {
				submitTest(t, r, owner.ID, key)
				question := awaitRuntimeQuestion(t, r, owner.ID, key)
				operation, err := r.Operation(t.Context(), question.OperationID)
				if err != nil || operation.State != session.OperationDispatched || operation.GrantID != nil || operation.Resource != string(owner.ID) {
					t.Fatalf("human interaction lacked its exact intrinsic operation: %+v %v", operation, err)
				}
				permissions, err := r.Permissions(t.Context(), owner.ID, "", 100)
				if err != nil || len(permissions) != 0 {
					t.Fatalf("asking human requested permission: %+v %v", permissions, err)
				}
				answers := []session.QuestionAnswer{{Answer: []string{"custom route"}}}
				switch key {
				case "batch":
					answers = []session.QuestionAnswer{{Answer: []string{"A", "custom route"}}, {Dismissed: true}}
				case "dismiss":
					answers = []session.QuestionAnswer{{Dismissed: true}}
				}
				answered, err := r.AnswerQuestion(t.Context(), owner.ID, question.OperationID, answers)
				if err != nil || answered.ClosedAt == nil {
					t.Fatalf("answer did not settle: %+v %v", answered, err)
				}
				result := questionCellResult(t, r, question, key)
				if key != "dismiss" && !strings.Contains(result.Output, "custom route") {
					t.Fatalf("human answer did not return to guest: %+v", result)
				}
				if key == "dismiss" && (answered.State != session.QuestionDismissed || !strings.Contains(result.Output, "1")) {
					t.Fatalf("explicit batch dismissal lost shape: %+v %+v", answered, result)
				}
				if _, err := r.AnswerQuestion(t.Context(), owner.ID, question.OperationID, answers); err != nil {
					t.Fatalf("answer retry after cell finished: %v", err)
				}
			}
		})
	}
}

func TestBothEnginesQuestionCancellationJoinsAndChildDenial(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			ask := `user.ask(question="Choose",options=[{"label":"A"},{"label":"B"}])`
			next := "print(7)"
			if engine == session.QuickJS {
				ask = `await user.ask({question:'Choose',options:[{label:'A'},{label:'B'}]})`
				next = "console.log(7)"
			}
			r := openEngineTest(t, t.TempDir(), cellProvider(map[string]string{"blocked": ask, "child": ask, "next": next}))
			owner := createEngineSession(t, r, engine)
			submitTest(t, r, owner.ID, "blocked")
			question := awaitRuntimeQuestion(t, r, owner.ID, "blocked")
			if err := r.DeleteSubtree(t.Context(), owner.ID); !errors.Is(err, store.ErrBusy) {
				t.Fatalf("deleted a live question cell: %v", err)
			}
			if _, err := r.CancelTurn(t.Context(), question.TurnID); err != nil {
				t.Fatal(err)
			}
			finished := waitTestWithin(t, r, "blocked", terminal, 30*time.Second)
			if finished.Turn.State != session.Cancelled {
				t.Fatalf("human wait prevented cancellation: %+v", finished.Turn)
			}
			closed, err := r.Question(t.Context(), owner.ID, question.OperationID)
			if err != nil || closed.State != session.QuestionClosed || closed.CloseReason == nil || *closed.CloseReason != session.QuestionCancelled {
				t.Fatalf("cancellation did not join question: %+v %v", closed, err)
			}
			if _, err := r.AnswerQuestion(t.Context(), owner.ID, question.OperationID, []session.QuestionAnswer{{Answer: []string{"A"}}}); !errors.Is(err, store.ErrConflict) {
				t.Fatalf("late answer revived cancelled waiter: %v", err)
			}
			// Only one kernel slot exists. A different root proves the real host
			// call joined before cancellation released the worker capacity.
			other := createEngineSession(t, r, engine)
			runCellTurn(t, r, other.ID, "next", "7\n")
			child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, store.ChildRequest{ParentID: other.ID, Parts: []session.Part{{Type: "text", Text: "child"}}})
			if err != nil {
				t.Fatal(err)
			}
			completed := waitTestWithin(t, r, "child", terminal, 30*time.Second)
			operations, err := r.Operations(t.Context(), completed.Turn.ID, "", 100)
			if err != nil || len(operations) != 1 || operations[0].State != session.OperationDenied || operations[0].Result == nil || operations[0].Result.Failure == nil || !strings.Contains(*operations[0].Result.Failure, "only the root agent") {
				t.Fatalf("child lacked explicit durable denial: %+v %v", operations, err)
			}
			questions, err := r.Questions(t.Context(), child.Session.ID, false, "", 100)
			if err != nil || len(questions) != 0 {
				t.Fatalf("child started human waiter: %+v %v", questions, err)
			}
			permissions, err := r.Permissions(t.Context(), child.Session.ID, "", 100)
			if err != nil || len(permissions) != 0 {
				t.Fatalf("child prompted user for authority: %+v %v", permissions, err)
			}
			if err := r.DeleteSubtree(t.Context(), owner.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Question(t.Context(), owner.ID, question.OperationID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("deleted cell retained question ownership: %v", err)
			}
			if err := r.Err(); err != nil {
				t.Fatalf("question cancellation faulted runtime: %v", err)
			}
		})
	}
}
