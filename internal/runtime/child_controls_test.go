package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/model"
	"github.com/context-labs/whip/internal/session"
)

func childControlCode(id int, code string) model.Response {
	raw, _ := json.Marshal(map[string]string{"code": code})
	return model.Response{Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: fmt.Sprintf("execute_%d", id), Name: "execute", Arguments: raw}}}}
}

func controlRuntime(t *testing.T, provider providerFunc, engine session.Engine, workers int) (*Runtime, session.Session) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	options := engineOptions(t)
	options.Workers = workers
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
	for _, name := range []string{"spawn", "submit", "inspect", "list", "stop", "delete", "wait_after_cell"} {
		if _, err := r.CreateGrant(t.Context(), session.Grant{ID: session.GrantID("control_" + name), SessionID: root.ID, Capability: "agents." + name, Resource: string(root.TreeID)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	return r, root
}

func TestGuestChildControlsWaitInspectSubmitAndDelete(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			provider := providerFunc(func(_ context.Context, request model.Request) (model.Response, error) {
				if request.Messages[0].Parts[0].Text != "root_controls" {
					if !strings.Contains(request.Instructions, "REVIEW_TEMPLATE") || !strings.Contains(request.Instructions, `child agent "Reviewer"`) {
						return model.Response{}, errors.New("missing child name or template instructions")
					}
					for _, v := range slices.Backward(request.Messages) {
						if v.Role == session.User {
							return model.Response{Parts: []session.Part{{Type: "text", Text: "result " + v.Parts[0].Text}}}, nil
						}
					}
				}
				var code string
				switch len(request.Messages) {
				case 1:
					code = `child=agents.spawn(prompt="first", name="Reviewer", template="review")
if child["name"] != "Reviewer": fail("missing child name")
agents.wait_after_cell(input_ids=[child["input_id"]])`
					if engine == session.QuickJS {
						code = `var child=await agents.spawn({prompt:"first",name:"Reviewer",template:"review"}); if(child.name!=="Reviewer") throw Error("missing child name"); await agents.wait_after_cell({input_ids:[child.input_id]});`
					}
				case 3:
					code = `outcome=agents.inspect(session_id=child["session_id"], input_id=child["input_id"])
if outcome["name"] != "Reviewer" or outcome["turn_state"] != "succeeded" or outcome["text"] != "result first": fail("wrong first outcome")
second=agents.submit(session_id=child["session_id"], parts=[{"type":"text","text":"second"}])
agents.wait_after_cell(input_ids=[second["input_id"]])`
					if engine == session.QuickJS {
						code = `var outcome=await agents.inspect({session_id:child.session_id,input_id:child.input_id}); if(outcome.name!=="Reviewer"||outcome.turn_state!=="succeeded"||outcome.text!=="result first") throw Error("wrong first outcome"); var second=await agents.submit({session_id:child.session_id,parts:[{type:"text",text:"second"}]}); await agents.wait_after_cell({input_ids:[second.input_id]});`
					}
				case 5:
					code = `old=agents.inspect(session_id=child["session_id"], input_id=child["input_id"])
outcome=agents.inspect(session_id=child["session_id"], input_id=second["input_id"])
if old["text"] != "result first" or outcome["text"] != "result second": fail("exact input changed")
listed=agents.list()
if len(listed["items"]) != 1 or listed["items"][0]["name"] != "Reviewer": fail("missing child")
agents.stop(session_id=child["session_id"])
deleted=agents.delete(session_id=child["session_id"])
if not deleted["deleted"]: fail("not deleted")
print("controls complete")`
					if engine == session.QuickJS {
						code = `var old=await agents.inspect({session_id:child.session_id,input_id:child.input_id}); var outcome=await agents.inspect({session_id:child.session_id,input_id:second.input_id}); if(old.text!=="result first"||outcome.text!=="result second") throw Error("exact input changed"); var listed=await agents.list({}); if(listed.items.length!==1||listed.items[0].name!=="Reviewer") throw Error("missing child"); await agents.stop({session_id:child.session_id}); var deleted=await agents.delete({session_id:child.session_id}); if(!deleted.deleted) throw Error("not deleted"); print("controls complete");`
					}
				default:
					last := request.Messages[len(request.Messages)-1]
					if last.Role != session.Tool || !strings.Contains(last.Parts[0].Result.Output, "controls complete") {
						return model.Response{}, errors.New("child-control cell did not complete")
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
				}
				return childControlCode(len(request.Messages), code), nil
			})
			r, root := controlRuntime(t, provider, engine, 1)
			definition, err := r.RegisterDefinition(t.Context(), session.DefinitionDocument{ID: "reviewer", Name: "Reviewer", Defaults: session.ConfigPatch{Instructions: &session.Instructions{Text: "REVIEW_TEMPLATE"}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := r.UpdateConfiguration(t.Context(), root.ID, root.ConfigRevision, session.ConfigPatch{Children: map[string]session.DefinitionRef{"review": definition.Ref}}); err != nil {
				t.Fatal(err)
			}
			submitTest(t, r, root.ID, "root_controls")
			finished := waitTestWithin(t, r, "root_controls", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded {
				history, historyErr := r.History(t.Context(), root.ID, 0, 100)
				evidence, _ := json.Marshal(struct {
					Turn    *session.Turn
					History []session.Message
				}{finished.Turn, history})
				t.Fatalf("control flow: %s history_error=%v runtime=%v", evidence, historyErr, r.Err())
			}
			items, err := r.Sessions(t.Context(), root.TreeID, "", 100)
			if err != nil || len(items) != 1 || items[0].ID != root.ID {
				t.Fatalf("delete left descendants: %+v %v", items, err)
			}
			r.mu.Lock()
			kernels := len(r.kernels)
			r.mu.Unlock()
			if kernels != 1 {
				t.Fatalf("deleted child kernel retained: %d", kernels)
			}
		})
	}
}

func TestGuestStopCancelsRunningChildBeforeWaitResumes(t *testing.T) {
	for _, engine := range []session.Engine{session.Starlark, session.QuickJS} {
		t.Run(string(engine), func(t *testing.T) {
			entered, cancelled := make(chan struct{}), make(chan struct{})
			var enterOnce, cancelOnce sync.Once
			provider := providerFunc(func(ctx context.Context, request model.Request) (model.Response, error) {
				if request.Messages[0].Parts[0].Text == "blocked" {
					enterOnce.Do(func() { close(entered) })
					<-ctx.Done()
					cancelOnce.Do(func() { close(cancelled) })
					return model.Response{}, ctx.Err()
				}
				switch len(request.Messages) {
				case 1:
					code := `child=agents.spawn(prompt="blocked")`
					if engine == session.QuickJS {
						code = `var child=await agents.spawn({prompt:"blocked"});`
					}
					return childControlCode(1, code), nil
				case 3:
					select {
					case <-ctx.Done():
						return model.Response{}, ctx.Err()
					case <-entered:
					}
					code := `stopped=agents.stop(session_id=child["session_id"])
if stopped["cancelling_turns"] != "1": fail("no cancellation")
agents.wait_after_cell(input_ids=[child["input_id"]])`
					if engine == session.QuickJS {
						code = `var stopped=await agents.stop({session_id:child.session_id}); if(stopped.cancelling_turns!=="1") throw Error("no cancellation"); await agents.wait_after_cell({input_ids:[child.input_id]});`
					}
					return childControlCode(3, code), nil
				case 5:
					code := `outcome=agents.inspect(session_id=child["session_id"], input_id=child["input_id"])
if outcome["turn_state"] != "cancelled": fail("not cancelled")
agents.delete(session_id=child["session_id"])
print("cancel observed")`
					if engine == session.QuickJS {
						code = `var outcome=await agents.inspect({session_id:child.session_id,input_id:child.input_id}); if(outcome.turn_state!=="cancelled") throw Error("not cancelled"); await agents.delete({session_id:child.session_id}); print("cancel observed");`
					}
					return childControlCode(5, code), nil
				default:
					if !strings.Contains(request.Messages[len(request.Messages)-1].Parts[0].Result.Output, "cancel observed") {
						return model.Response{}, errors.New("cancel inspection failed")
					}
					return model.Response{Parts: []session.Part{{Type: "text", Text: "done"}}}, nil
				}
			})
			r, root := controlRuntime(t, provider, engine, 2)
			submitTest(t, r, root.ID, "root_stop")
			finished := waitTestWithin(t, r, "root_stop", terminal, 30*time.Second)
			if finished.Turn.State != session.Succeeded {
				t.Fatalf("stop flow: %+v runtime=%v", finished.Turn, r.Err())
			}
			select {
			case <-cancelled:
			default:
				t.Fatal("stop did not interrupt live child provider")
			}
		})
	}
}
