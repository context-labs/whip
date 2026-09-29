package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func TestBothEnginesAutomaticModeDefaultChildReadsAndReportsWithoutGrants(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			for _, test := range []struct {
				name     string
				explicit bool
			}{{name: "omitted_grants"}, {name: "explicit_empty_grants", explicit: true}} {
				t.Run(test.name, func(t *testing.T) {
					spawn := `child=agents.spawn(prompt="read-and-report")`
					if test.explicit {
						spawn = `child=agents.spawn(prompt="read-and-report", grant_ids=[])`
					}
					codes := map[string]string{"spawn": spawn + "\nagents.wait_after_cell(input_ids=[child[\"input_id\"]])\nprint(\"child finished\")"}
					if engine == session.QuickJS {
						spawn = `var child=await agents.spawn({prompt:"read-and-report"});`
						if test.explicit {
							spawn = `var child=await agents.spawn({prompt:"read-and-report",grant_ids:[]});`
						}
						codes["spawn"] = spawn + `await agents.wait_after_cell({input_ids:[child.input_id]}); console.log("child finished");`
					}
					base := cellProvider(codes)
					provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
						last := request.Messages[len(request.Messages)-1]
						if _, scripted := codes[last.Parts[0].Text]; last.Role != session.Tool && !scripted {
							// Consume actual child completion/mail without inventing more work.
							return model.Response{Parts: []session.Part{{Type: "text", Text: "report received"}}}, nil
						}
						return base(ctx, request)
					})
					r := openEngineTest(t, t.TempDir(), provider)
					root := createEngineSession(t, r, engine)
					const proof = "disposable child workspace proof"
					if err := os.WriteFile(filepath.Join(root.WorkingDirectory, "proof.txt"), []byte(proof), 0o600); err != nil {
						t.Fatal(err)
					}
					codes["read-and-report"] = fmt.Sprintf("read=files.read(path=\"proof.txt\")\nmail.send(recipient_id=%q,subject=\"workspace proof\",body=read[\"output\"])\nprint(read[\"output\"])", root.ID)
					if engine == session.QuickJS {
						codes["read-and-report"] = fmt.Sprintf(`var read=await files.read({path:"proof.txt"}); await mail.send({recipient_id:%q,subject:"workspace proof",body:read.output}); console.log(read.output);`, root.ID)
					}
					setRuntimeMode(t, r, root.ID, "full-access", 1, session.PermissionAutomatic)
					if grants, err := r.Grants(t.Context(), root.ID, "", 100); err != nil || len(grants) != 0 {
						t.Fatal("Full Access fixture must have zero standing grants", grants, err)
					}
					submitTest(t, r, root.ID, "spawn")
					finished := waitTestWithin(t, r, "spawn", terminal, 30*time.Second)
					if finished.Turn.State != session.Succeeded {
						t.Fatalf("parent spawn/wait failed: %+v runtime=%v", finished.Turn, r.Err())
					}
					children, err := r.Sessions(t.Context(), root.TreeID, "", 100)
					if err != nil || len(children) != 2 {
						t.Fatal("expected one spawned child", children, err)
					}
					child := children[0]
					if child.ID == root.ID {
						child = children[1]
					}
					if child.ParentID == nil || *child.ParentID != root.ID || child.WorkingDirectory != root.WorkingDirectory {
						t.Fatal("spawn changed child ownership or workspace", child)
					}
					cell, err := r.store.LatestCell(t.Context(), child.ID)
					if err != nil || cell == nil {
						t.Fatal("child did not execute", cell, err)
					}
					operations, err := r.Operations(t.Context(), cell.TurnID, "", 100)
					if err != nil {
						t.Fatal(err)
					}
					if test.explicit {
						if cell.State != session.CellFailed || len(operations) != 1 || operations[0].Capability != "files.read" || operations[0].State != session.OperationDenied || operations[0].DispatchedAt != nil {
							t.Fatalf("explicit empty grants inherited automatic authority: cell=%+v operations=%+v", cell, operations)
						}
					} else {
						if cell.State != session.CellSucceeded || len(operations) != 2 {
							t.Fatalf("default child could not read and report: cell=%+v operations=%+v", cell, operations)
						}
						seen := map[string]bool{}
						for _, op := range operations {
							if op.State != session.OperationSucceeded || op.GrantID != nil || op.PermissionRevision == nil || *op.PermissionRevision != 2 || op.DispatchedAt == nil {
								t.Fatalf("child operation lost inherited policy authority: %+v", op)
							}
							seen[op.Capability] = true
						}
						if !seen["files.read"] || !seen["mail.send"] {
							t.Fatal("missing actual read or internal report operation", operations)
						}
					}
					if grants, err := r.Grants(t.Context(), child.ID, "", 100); err != nil || len(grants) != 0 {
						t.Fatal("default spawn fabricated standing grants", grants, err)
					}
					mails, err := r.ListMail(t.Context(), root.ID, "", "", 100)
					if err != nil {
						t.Fatal(err)
					}
					reports := 0
					for _, item := range mails {
						if item.Subject != "workspace proof" {
							continue
						}
						report, err := r.ReadMail(t.Context(), root.ID, item.ID)
						if err != nil || !strings.Contains(report.Body, proof) || report.State != session.MailDelivered {
							t.Fatal("parent did not receive successful child report", report, err)
						}
						reports++
					}
					wantReports := 1
					if test.explicit {
						wantReports = 0
					}
					if reports != wantReports {
						t.Fatalf("child reports=%d, want %d", reports, wantReports)
					}
				})
			}
		})
	}
}
