package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func setRuntimeMode(t *testing.T, r *Runtime, owner session.SessionID, id string, revision session.Revision, mode session.PermissionMode) session.PermissionModeEdit {
	t.Helper()
	edit, err := r.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: session.PermissionModeEditID(id), SessionID: owner, ExpectedRevision: revision, Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return edit
}

func TestPermissionModeHostDefaultsCaptureOnlyNewRootsAndForks(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	existing := createTest(t, r)
	snapshot, err := r.HostConfiguration().Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	automatic, err := r.HostConfiguration().SetDefaultPermissionMode(t.Context(), snapshot.Revision, session.PermissionAutomatic)
	if err != nil {
		t.Fatal(err)
	}
	next := createTest(t, r)
	for _, test := range []struct {
		owner session.SessionID
		mode  session.PermissionMode
	}{{existing.ID, session.PermissionPrompt}, {next.ID, session.PermissionAutomatic}} {
		policy, err := r.PermissionPolicy(t.Context(), test.owner)
		if err != nil || policy.Mode != test.mode || policy.Revision != 1 {
			t.Fatal("host change changed existing policy or missed new root", policy, err)
		}
	}
	request := store.CreateTree{PermissionMode: new(session.PermissionPrompt), Engine: session.Starlark, Definition: existing.Definition, WorkingDirectory: existing.WorkingDirectory, Overrides: session.ConfigPatch{Model: &existing.Config.Model}}
	_, explicit, err := r.CreateTree(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if policy, err := r.PermissionPolicy(t.Context(), explicit.ID); err != nil || policy.Mode != session.PermissionPrompt {
		t.Fatal("explicit Ask lost to host default", policy, err)
	}
	request.PermissionMode = new(session.PermissionMode(""))
	if _, _, err := r.CreateTree(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("explicit empty mode accepted", err)
	}
	forkRequest := session.ForkRequest{ID: "fork", SessionID: existing.ID, ExpectedHistoryRevision: 1, ExpectedConfigRevision: 1}
	fork, err := r.Fork(t.Context(), forkRequest)
	if err != nil {
		t.Fatal(err)
	}
	if policy, err := r.PermissionPolicy(t.Context(), fork.Root.ID); err != nil || policy.Mode != session.PermissionAutomatic {
		t.Fatal("fork copied source mode instead of current host default", policy, err)
	}
	if _, err := r.HostConfiguration().SetDefaultPermissionMode(t.Context(), automatic.Revision, session.PermissionPrompt); err != nil {
		t.Fatal(err)
	}
	if retry, err := r.Fork(t.Context(), forkRequest); err != nil || retry.Fork != fork.Fork {
		t.Fatal("fork receipt changed", retry, err)
	}
	if policy, err := r.PermissionPolicy(t.Context(), fork.Root.ID); err != nil || policy.Mode != session.PermissionAutomatic {
		t.Fatal("retry reselected host default", policy, err)
	}
	changed := setRuntimeMode(t, r, existing.ID, "edit", 1, session.PermissionAutomatic)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, directory, model.Scripted{})
	if policy, err := r.PermissionPolicy(t.Context(), existing.ID); err != nil || policy != changed.Policy {
		t.Fatal("restart reinterpreted tree using host default", policy, err)
	}
	if receipt, err := r.PermissionModeEdit(t.Context(), existing.ID, changed.ID); err != nil || receipt != changed {
		t.Fatal("restart lost edit receipt", receipt, err)
	}
	// Exact edits remain readable even when current host declarations cannot load.
	if err := os.WriteFile(filepath.Join(directory, config.FileName), []byte("invalid current host"), 0o600); err != nil {
		t.Fatal(err)
	}
	if retry, err := r.SetPermissionMode(t.Context(), changed.PermissionModeRequest); err != nil || retry != changed {
		t.Fatal("edit retry touched current host", retry, err)
	}
	if retry, err := r.Fork(t.Context(), forkRequest); err != nil || retry.Fork != fork.Fork {
		t.Fatal("fork retry touched current host", retry, err)
	}
}

func TestBothEnginesPermissionModeUsesSavedPolicyAndRetiresWaitingPrompt(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			codes := map[string]string{
				"waiting":   `files.write(path="blocked.txt", content="forbidden")`,
				"automatic": "files.write(path=\"automatic.txt\", content=\"saved\")\nprint(7)",
				"restart":   "files.write(path=\"restart.txt\", content=\"saved\")\nprint(8)",
				"ask":       `files.write(path="ask.txt", content="unapproved")`,
				"question":  `answer = user.ask(question="Choose", options=[{"label":"A"},{"label":"B"}])` + "\n" + `print(answer["answer"][0])`,
			}
			if engine == session.QuickJS {
				codes = map[string]string{
					"waiting":   `await files.write({path:'blocked.txt',content:'forbidden'});`,
					"automatic": `await files.write({path:'automatic.txt',content:'saved'}); console.log(7);`,
					"restart":   `await files.write({path:'restart.txt',content:'saved'}); console.log(8);`,
					"ask":       `await files.write({path:'ask.txt',content:'unapproved'});`,
					"question":  `var answer = await user.ask({question:'Choose',options:[{label:'A'},{label:'B'}]}); console.log(answer.answer[0]);`,
				}
			}
			directory := t.TempDir()
			provider := cellProvider(codes)
			r := openEngineTest(t, directory, provider)
			owner := createEngineSession(t, r, engine)
			submitTest(t, r, owner.ID, "waiting")
			waiting := awaitRuntimeFilePermission(t, r, owner.ID, "waiting", "files.write")
			setRuntimeMode(t, r, owner.ID, "enable", 1, session.PermissionAutomatic)
			waitTestWithin(t, r, "waiting", terminal, 30*time.Second)
			if op, err := r.Operation(t.Context(), waiting.ID); err != nil || op.State != session.OperationDenied || op.DispatchedAt != nil {
				t.Fatal("mode change did not retire waiting operation", op, err)
			}
			if _, err := os.Stat(filepath.Join(owner.WorkingDirectory, "blocked.txt")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("obsolete approval produced an effect", err)
			}
			runCellTurn(t, r, owner.ID, "automatic", "7\n")
			assertRuntimeFileBytes(t, filepath.Join(owner.WorkingDirectory, "automatic.txt"), "saved")
			admission, err := r.Admission(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "automatic"})
			if err != nil {
				t.Fatal(err)
			}
			operations, err := r.Operations(t.Context(), admission.Turn.ID, "", 100)
			if err != nil || len(operations) != 2 {
				t.Fatal("file and optional diagnostic evidence missing", operations, err)
			}
			for _, operation := range operations {
				if operation.PermissionRevision == nil || *operation.PermissionRevision != 2 || operation.GrantID != nil || operation.State != session.OperationSucceeded {
					t.Fatal("automatic evidence missing", operation)
				}
			}
			if err := r.Close(); err != nil {
				t.Fatal(err)
			}
			r = openEngineTest(t, directory, provider)
			runCellTurn(t, r, owner.ID, "restart", "8\n")
			assertRuntimeFileBytes(t, filepath.Join(owner.WorkingDirectory, "restart.txt"), "saved")
			submitTest(t, r, owner.ID, "question")
			question := awaitRuntimeQuestion(t, r, owner.ID, "question")
			setRuntimeMode(t, r, owner.ID, "disable", 2, session.PermissionPrompt)
			if _, err := r.AnswerQuestion(t.Context(), owner.ID, question.OperationID, []session.QuestionAnswer{{Answer: []string{"A"}}}); err != nil {
				t.Fatal("mode change interrupted intrinsic human question", err)
			}
			questionCellResult(t, r, question, "question")
			submitTest(t, r, owner.ID, "ask")
			ask := awaitRuntimeFilePermission(t, r, owner.ID, "ask", "files.write")
			denyRuntimeFilePermission(t, r, ask, "ask")
			if grants, err := r.Grants(t.Context(), owner.ID, "", 100); err != nil || len(grants) != 0 {
				t.Fatal("mode fabricated grants", grants, err)
			}
		})
	}
}
