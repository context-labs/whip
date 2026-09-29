package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestBothEnginesPermissionDenialPersistsWithoutBlockingIntrinsicQuestions(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{"blocked": `files.write(path="denied.txt",content="forbidden")`, "question": `answer = user.ask(question="Choose",options=[{"label":"A"},{"label":"B"}])` + "\n" + `print(answer["answer"][0])`, "allowed": `files.write(path="allowed.txt",content="saved")` + "\nprint(7)"}
			if engine == session.QuickJS {
				codes = map[string]string{"blocked": `await files.write({path:'denied.txt',content:'forbidden'});`, "question": `var answer = await user.ask({question:'Choose',options:[{label:'A'},{label:'B'}]}); console.log(answer.answer[0]);`, "allowed": `await files.write({path:'allowed.txt',content:'saved'}); console.log(7);`}
			}
			directory := t.TempDir()
			provider := cellProvider(codes)
			r := openEngineTest(t, directory, provider)
			owner := createEngineSession(t, r, engine)
			setRuntimeMode(t, r, owner.ID, "automatic", 1, session.PermissionAutomatic)
			request := session.PermissionDenialRequest{ID: "Deny.Mixed", SessionID: owner.ID, ExpectedRevision: 2, DenyInteractive: true}
			before, err := r.SetPermissionDenial(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r = openEngineTest(t, directory, provider)
			if retry, err := r.SetPermissionDenial(t.Context(), request); err != nil || retry != before {
				t.Fatal(retry, err)
			}
			submitTest(t, r, owner.ID, "blocked")
			done := waitTestWithin(t, r, "blocked", terminal, 30*time.Second)
			ops, err := r.Operations(t.Context(), done.Turn.ID, "", 100)
			if err != nil || len(ops) != 1 || ops[0].State != session.OperationDenied || ops[0].DispatchedAt != nil {
				t.Fatal("denial escaped operation boundary", ops, err)
			}
			if _, err := os.Stat(filepath.Join(owner.WorkingDirectory, "denied.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("denied effect executed", err)
			}
			if items, err := r.Permissions(t.Context(), owner.ID, "", 100); err != nil || len(items) != 0 {
				t.Fatal("denial prompted human", items, err)
			}
			submitTest(t, r, owner.ID, "question")
			question := awaitRuntimeQuestion(t, r, owner.ID, "question")
			if _, err := r.AnswerQuestion(t.Context(), owner.ID, question.OperationID, []session.QuestionAnswer{{Answer: []string{"A"}}}); err != nil {
				t.Fatal(err)
			}
			questionCellResult(t, r, question, "question")
			if _, err := r.SetPermissionDenial(t.Context(), session.PermissionDenialRequest{ID: "clear", SessionID: owner.ID, ExpectedRevision: 3}); err != nil {
				t.Fatal(err)
			}
			runCellTurn(t, r, owner.ID, "allowed", "7\n")
			assertRuntimeFileBytes(t, filepath.Join(owner.WorkingDirectory, "allowed.txt"), "saved")
		})
	}
}
