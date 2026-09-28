package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func TestRecursiveWaitReleasesWorkerAndKernelAtCommittedCell(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			leafEntered := make(chan struct{})
			releaseLeaf := make(chan struct{})
			var once sync.Once
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				prompt := request.Messages[0].Parts[0].Text
				if prompt == "leaf" && len(request.Messages) == 1 {
					once.Do(func() { close(leafEntered) })
					select {
					case <-ctx.Done():
						return model.Response{}, ctx.Err()
					case <-releaseLeaf:
					}
				}
				code := ""
				if len(request.Messages) == 1 {
					switch prompt {
					case "root", "child":
						next := "child"
						if prompt == "child" {
							next = "leaf"
						}
						code = fmt.Sprintf("marker=41\nchild=agents.spawn(prompt=%q)\nregistered=agents.wait_after_cell(input_ids=[child[\"input_id\"]])\nprint(registered[\"boundary\"])", next)
						if engine == session.QuickJS {
							code = fmt.Sprintf("var marker=41; var child=await agents.spawn({prompt:%q}); var registered=await agents.wait_after_cell({input_ids:[child.input_id]}); print(registered.boundary)", next)
						}
					case "leaf":
						code = "print(42)"
					}
				} else if prompt == "root" && len(request.Messages) == 3 {
					code = "print(marker+1)"
				}
				if code == "" {
					return model.Response{Parts: []session.Part{{Type: "text", Text: "completed " + prompt}}}, nil
				}
				raw, _ := json.Marshal(map[string]string{"code": code})
				return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: fmt.Sprintf("execute_%d", len(request.Messages)), Name: "execute", Arguments: raw}}}}, nil
			})
			directory := t.TempDir()
			if err := os.Chmod(directory, 0o700); err != nil {
				t.Fatal(err)
			}
			options := engineOptions(t)
			options.Workers = 1
			options.KernelWorkers = 1
			options.PollInterval = time.Millisecond
			r, err := Open(t.Context(), directory, provider, options)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := r.Close(); err != nil {
					t.Error(err)
				}
			})
			root := createEngineSession(t, r, engine)
			for i, capability := range []string{"agents.spawn", "agents.wait_after_cell"} {
				_, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID(fmt.Sprintf("grant_%d", i)), SessionID: root.ID, Capability: capability, Resource: string(root.TreeID)})
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := r.Start(t.Context()); err != nil {
				t.Fatal(err)
			}
			admitted := submitTest(t, r, root.ID, "root")
			select {
			case <-leafEntered:
			case <-time.After(30 * time.Second):
				t.Fatalf("descendant could not progress: %v", r.Err())
			}
			history, err := r.History(t.Context(), root.ID, 0, 100)
			if err != nil || len(history) != 3 || history[2].Role != session.Tool {
				t.Fatalf("wait preceded committed cell: %+v %v", history, err)
			}
			latest, err := r.store.LatestCell(t.Context(), root.ID)
			if err != nil || latest == nil || latest.Checkpoint == nil || latest.State != session.CellSucceeded {
				t.Fatalf("wait preceded checkpoint: %+v %v", latest, err)
			}
			sessions, err := r.Sessions(t.Context(), root.TreeID, "", 100)
			if err != nil || len(sessions) != 3 {
				t.Fatalf("recursive admission: %+v %v", sessions, err)
			}
			r.mu.Lock()
			active := len(r.active)
			r.mu.Unlock()
			if active != 3 {
				t.Fatalf("active ownership disappeared while waiting: %d", active)
			}
			close(releaseLeaf)
			finished := waitTestWithin(t, r, "root", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded || finished.Input.ID != admitted.Input.ID {
				t.Fatalf("root outcome: %+v runtime=%v", finished, r.Err())
			}
			history, err = r.History(t.Context(), root.ID, 0, 100)
			if err != nil || len(history) != 6 || !strings.Contains(history[4].Parts[0].Result.Output, `42\n`) {
				t.Fatalf("parent checkpoint was not preserved across child execution: %+v %v", history, err)
			}
			for _, current := range sessions {
				if current.ID == root.ID {
					continue
				}
				transcript, err := r.History(t.Context(), current.ID, 0, 100)
				if err != nil || len(transcript) != 4 || transcript[3].Role != session.Assistant {
					t.Fatalf("child used different transcript path: %+v %v", transcript, err)
				}
			}
		})
	}
}

func TestChildAdmissionSurvivesRestartWithoutLoadedWorker(t *testing.T) {
	directory := t.TempDir()
	r := openTest(t, directory, model.Scripted{})
	root := createTest(t, r)
	request := store.ChildRequest{ParentID: root.ID, Parts: []session.Part{{Type: "text", Text: "durable child"}}}
	identity := session.RequestIdentity{ClientID: "test", RequestID: "spawn_restart"}
	admitted, err := r.SpawnChild(t.Context(), identity, request)
	if err != nil {
		t.Fatal(err)
	}
	if admitted.Admission.Turn != nil {
		t.Fatal("spawn executed before runtime start")
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	r = openTest(t, directory, model.Scripted{})
	retried, err := r.SpawnChild(t.Context(), identity, request)
	if err != nil || retried.Session.ID != admitted.Session.ID || retried.Admission.Input.ID != admitted.Admission.Input.ID {
		t.Fatalf("child identity changed after restart: %+v %v", retried, err)
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	result := waitTest(t, r, "spawn_restart", terminal)
	if result.Turn.State != session.Succeeded {
		t.Fatalf("child work did not survive: %+v", result)
	}
	history, err := r.History(t.Context(), admitted.Session.ID, 0, 100)
	if err != nil || len(history) != 2 || history[1].Parts[0].Text != "ack: durable child" {
		t.Fatalf("child transcript: %+v %v", history, err)
	}
}
