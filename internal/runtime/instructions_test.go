package runtime

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func nextInstructionRequest(t *testing.T, requests <-chan model.Request) model.Request {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("instruction request did not arrive")
		return model.Request{}
	}
}

func writeInstructionFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInstructionsCaptureOnceThenRefreshChildrenAndRestart(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		for _, automatic := range []bool{false, true} {
			mode := "standing-grant"
			if automatic {
				mode = "full-access"
			}
			t.Run(string(engine)+"/"+mode, func(t *testing.T) {
				requests := make(chan model.Request, 10)
				code := "print(42)"
				if engine == session.QuickJS {
					code = "console.log(42)"
				}
				arguments, err := json.Marshal(map[string]string{"code": code})
				if err != nil {
					t.Fatal(err)
				}
				release := make(chan struct{})
				unblock := sync.OnceFunc(func() { close(release) })
				var calls atomic.Int32
				provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
					requests <- request
					if calls.Add(1) == 1 {
						select {
						case <-release:
						case <-ctx.Done():
							return model.Response{}, ctx.Err()
						}
						return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "once", Name: "execute", Arguments: arguments}}}}, nil
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
				})
				directory := t.TempDir()
				r := openEngineTest(t, directory, provider)
				t.Cleanup(unblock)
				owner := createEngineSession(t, r, engine)
				policy := session.Instructions{Text: "configured-before", ProjectFiles: []string{"AGENTS.md"}}
				owner, err = r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(owner.WorkingDirectory, "AGENTS.md")
				writeInstructionFile(t, path, "project-before")
				var grant session.Grant
				if automatic {
					setRuntimeMode(t, r, owner.ID, "full-access", 1, session.PermissionAutomatic)
				} else {
					grant, err = r.CreateGrant(t.Context(), session.Grant{ID: "read-before", SessionID: owner.ID, Capability: "files.read", Resource: owner.WorkingDirectory})
					if err != nil {
						t.Fatal(err)
					}
				}
				submitTest(t, r, owner.ID, "capture")
				first := nextInstructionRequest(t, requests)
				if !strings.Contains(first.Instructions, "project-before") || !strings.Contains(first.Instructions, "configured-before") {
					t.Fatal("first turn did not load configured project instructions")
				}
				manifest, err := r.InstructionManifest(t.Context(), first.TurnID)
				digest := sha256.Sum256([]byte(first.Instructions))
				if err != nil || manifest == nil || manifest.SHA256 != hex.EncodeToString(digest[:]) || manifest.Bytes != int64(len(first.Instructions)) || len(manifest.Sources) != 1 || manifest.Sources[0].Path != "AGENTS.md" {
					t.Fatalf("manifest was not committed before dispatch: %+v %v", manifest, err)
				}
				writeInstructionFile(t, path, "project-after")
				policy.Text = "configured-after"
				owner, err = r.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Instructions: &policy})
				if err != nil {
					t.Fatal(err)
				}
				if automatic {
					setRuntimeMode(t, r, owner.ID, "ask", 2, session.PermissionPrompt)
				} else if _, err := r.RevokeGrant(t.Context(), grant.ID); err != nil {
					t.Fatal(err)
				}
				unblock()
				if done := waitTestWithin(t, r, "capture", terminal, 30*time.Second); done.Turn.State != session.Succeeded {
					if done.Turn.Failure != nil {
						t.Fatal(*done.Turn.Failure)
					}
					t.Fatalf("captured turn failed: %+v", done.Turn)
				}
				second := nextInstructionRequest(t, requests)
				if second.Instructions != first.Instructions {
					t.Fatal("file/configuration/revocation changed a captured turn after its cell")
				}
				submitTest(t, r, owner.ID, "revoked")
				denied := nextInstructionRequest(t, requests)
				if !strings.Contains(denied.Instructions, "configured-after") || strings.Contains(denied.Instructions, "project-after") {
					t.Fatal("next turn ignored updated configuration or retained revoked project access")
				}
				if done := waitTest(t, r, "revoked", terminal); done.Turn.State != session.Succeeded {
					t.Fatalf("denied optional sources failed turn: %+v", done.Turn)
				}
				if automatic {
					setRuntimeMode(t, r, owner.ID, "full-access-again", 3, session.PermissionAutomatic)
				} else if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "read-after", SessionID: owner.ID, Capability: "files.read", Resource: owner.WorkingDirectory}); err != nil {
					t.Fatal(err)
				}
				child, err := r.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "instruction-child"}, store.ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "child"}}})
				if err != nil {
					t.Fatal(err)
				}
				childRequest := nextInstructionRequest(t, requests)
				if childRequest.SessionID != child.Session.ID || !strings.Contains(childRequest.Instructions, "project-after") || !strings.Contains(childRequest.Instructions, "configured-after") {
					t.Fatal("child did not capture inherited policy and delegated source access")
				}
				if done := waitTest(t, r, "instruction-child", terminal); done.Turn.State != session.Succeeded {
					t.Fatalf("child failed: %+v", done.Turn)
				}
				if err := r.Close(); err != nil {
					t.Fatal(err)
				}
				writeInstructionFile(t, path, "project-restarted")
				r = openEngineTest(t, directory, provider)
				saved, err := r.InstructionManifest(t.Context(), first.TurnID)
				if err != nil || !reflect.DeepEqual(saved, manifest) {
					t.Fatalf("restart changed immutable audit: %+v %v", saved, err)
				}
				submitTest(t, r, child.Session.ID, "instruction-restart")
				restarted := nextInstructionRequest(t, requests)
				if !strings.Contains(restarted.Instructions, "project-restarted") || strings.Contains(restarted.Instructions, "project-after") {
					t.Fatal("retained child reused stale instruction bytes after restart")
				}
				if done := waitTest(t, r, "instruction-restart", terminal); done.Turn.State != session.Succeeded {
					t.Fatalf("restarted turn failed: %+v", done.Turn)
				}
			})
		}
	}
}

func TestInstructionCaptureFailureNeverDispatchesModel(t *testing.T) {
	for _, mode := range []string{"invalid source", "audit write"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			directory := t.TempDir()
			r := openTest(t, directory, providerFunc(func(context.Context, model.Request) (model.Response, error) {
				calls.Add(1)
				return model.Response{Parts: []session.Part{{Type: "text", Text: "unexpected"}}}, nil
			}))
			owner := createTest(t, r)
			if mode == "invalid source" {
				writeInstructionFile(t, filepath.Join(owner.WorkingDirectory, "AGENTS.md"), string([]byte{0xff}))
				if _, err := r.CreateGrant(t.Context(), session.Grant{ID: "read", SessionID: owner.ID, Capability: "files.read", Resource: owner.WorkingDirectory}); err != nil {
					t.Fatal(err)
				}
			} else {
				db, err := sql.Open("sqlite", filepath.Join(directory, "state.db")+"?_pragma=busy_timeout(5000)")
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.ExecContext(t.Context(), "CREATE TRIGGER reject_instruction_audit BEFORE INSERT ON turn_instruction_manifests BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END"); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, owner.ID, "broken-instructions")
			done := waitTest(t, r, "broken-instructions", terminal)
			attempts, err := r.ModelAttempts(t.Context(), done.Turn.ID, "", 100)
			if err != nil || done.Turn.State != session.Failed || calls.Load() != 0 || len(attempts) != 0 {
				t.Fatalf("capture failure dispatched work: turn=%+v calls=%d attempts=%+v err=%v", done.Turn, calls.Load(), attempts, err)
			}
			manifest, err := r.InstructionManifest(t.Context(), done.Turn.ID)
			if err != nil || manifest != nil {
				t.Fatalf("failed capture retained a success manifest: %+v %v", manifest, err)
			}
		})
	}
}
