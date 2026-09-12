package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

func TestSessionCreateFreezesEngineBeforeDefaultsChange(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	if _, _, err := config.UpdateVersioned("", func(c *config.Config) error { c.RLM.DefaultEngine = "quickjs"; return nil }); err != nil {
		t.Fatal(err)
	}
	store := openStore(t, filepath.Join(t.TempDir(), "engine.db"))
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	admission := session.CommandAdmission{ClientID: "engine", CommandID: "create", RequestDigest: "create"}
	create := CreateSession{Kind: session.SessionKindAgent, CWD: t.TempDir(), Model: "m", Provider: "p"}
	first, err := owner.control.CreateSession(t.Context(), admission, create)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := config.UpdateVersioned("", func(c *config.Config) error { c.RLM.DefaultEngine = "starlark"; return nil }); err != nil {
		t.Fatal(err)
	}
	retry, err := owner.control.CreateSession(t.Context(), admission, create)
	if err != nil || string(retry.Outcome.Inline) != string(first.Outcome.Inline) {
		t.Fatalf("retry=%+v %v", retry, err)
	}
	var result struct {
		RootID string `json:"root_id"`
	}
	if err := json.Unmarshal(first.Outcome.Inline, &result); err != nil {
		t.Fatal(err)
	}
	meta, _, err := store.Load(result.RootID)
	if err != nil || meta.ExecutionEngine != "quickjs" {
		t.Fatalf("engine=%s %v", meta.ExecutionEngine, err)
	}
	create.ExecutionEngine = "node"
	admission.CommandID = "invalid"
	admission.RequestDigest = "invalid"
	if _, err := owner.control.CreateSession(t.Context(), admission, create); err == nil {
		t.Fatal("accepted invalid engine")
	}
}

func TestRecursiveExecutionEngineInheritedAndRestored(t *testing.T) {
	for _, engine := range []string{"starlark", "quickjs"} {
		t.Run(engine, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { streamText(w, "done") }))
			defer server.Close()
			store, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 4, engine)
			runs := &sync.Map{}
			runtime.setRunTurnHook(observeRunTurn(runs))
			parent := runtime.rootNode
			nodes := []*AgentSession{parent}
			for _, name := range []string{"child", "grandchild"} {
				// Explicit reports keep completion mail from starting another model
				// turn while this test directly executes and suspends each kernel.
				result, err := parent.host.Call(t.Context(), "agents", "spawn", map[string]any{
					"name": name, "prompt": "work", "report": "message",
				})
				if err != nil {
					t.Fatal(err)
				}
				id := result.(map[string]any)["id"].(string)
				waitRunTurn(t, runs, id, 1)
				runtime.mu.RLock()
				node := runtime.agents[id]
				runtime.mu.RUnlock()
				waitExecutionAgentIdle(t, node)
				nodes = append(nodes, node)
				parent = node
			}
			for _, node := range nodes {
				waitExecutionAgentIdle(t, node)
				if node.kernel.Describe().ID != engine {
					t.Fatalf("node %s engine=%s", node.id, node.kernel.Describe().ID)
				}
				code := `memo = 41; print(memo)`
				read := `print(memo + 1)`
				if engine == "quickjs" {
					code = `var memo = 41; print(memo);`
					read = `print(memo + 1);`
				}
				result, err := node.kernel.Exec(t.Context(), code)
				if err != nil || !strings.Contains(result.Output, "41") {
					t.Fatalf("exec result=%+v %v", result, err)
				}
				if err := node.kernel.Suspend(); err != nil {
					t.Fatal(err)
				}
				result, err = node.kernel.Exec(t.Context(), read)
				if err != nil || !strings.Contains(result.Output, "42") {
					t.Fatalf("restore result=%+v %v", result, err)
				}
				if engine == "quickjs" {
					_, image, err := store.LoadAgentCheckpoint(t.Context(), root.ID(), node.id)
					if err != nil || len(image) == 0 {
						t.Fatalf("checkpoint bytes=%d %v", len(image), err)
					}
				}
			}
			for _, key := range []string{"engine", "execution_engine", "rlm_engine"} {
				if _, err := nodes[0].host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "override", "prompt": "work", key: engine}); err == nil {
					t.Fatalf("accepted child %s override", key)
				}
			}
		})
	}
}

func TestQuickJSCheckpointSurvivesDaemonRestart(t *testing.T) {
	database := filepath.Join(t.TempDir(), "restart.db")
	store := openStore(t, database)
	if _, err := store.AdmitCommand(t.Context(), session.CommandAdmission{ClientID: "engine", CommandID: "create", Scope: session.CommandScopeDaemon, RequestDigest: "create"}); err != nil {
		t.Fatal(err)
	}
	record, err := store.CreateSessionForCommandWithEngine(t.Context(), "engine", "create", session.SessionKindAgent, t.TempDir(), "model", "provider", "", "quickjs")
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		RootID string `json:"root_id"`
	}
	if err := json.Unmarshal(record.Outcome.Inline, &result); err != nil {
		t.Fatal(err)
	}
	owner, root, runtime := openPromptRuntime(t, store, result.RootID, llm.New("http://127.0.0.1:1", "key"))
	if _, err := store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: root.ID(), ParentAgentID: root.AgentID(), ChildAgentID: "child", Name: "child", Model: "model", Provider: "provider", CWD: root.meta.CWD}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.restoreChildren(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{root.AgentID(), "child"} {
		node := runtime.agents[id]
		if _, err := node.kernel.Exec(t.Context(), `var memo = {n: 41}; var increment = () => ++memo.n;`); err != nil {
			t.Fatal(err)
		}
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, database)
	_, root, runtime = openPromptRuntime(t, store, result.RootID, llm.New("http://127.0.0.1:1", "key"))
	for _, id := range []string{root.AgentID(), "child"} {
		node := runtime.agents[id]
		result, err := node.kernel.Exec(t.Context(), `print(increment());`)
		if err != nil || !strings.Contains(result.Output, "42") || node.kernel.Describe().ID != "quickjs" {
			t.Fatalf("node %s lost its image: %+v %v", id, result, err)
		}
	}
}

func TestExecutionEnginesPreserveExactStateCASAndMail(t *testing.T) {
	for _, engine := range []string{"starlark", "quickjs"} {
		t.Run(engine, func(t *testing.T) {
			store, root, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 2, engine)
			if _, err := store.AdmitAgent(t.Context(), session.AgentAdmission{RootID: root.ID(), ParentAgentID: root.AgentID(), ChildAgentID: "child", Name: "child", Model: "model", Provider: "provider", CWD: root.meta.CWD}); err != nil {
				t.Fatal(err)
			}
			if err := runtime.restoreChildren(t.Context()); err != nil {
				t.Fatal(err)
			}
			rootCode := `state.blackboard_set(key="exact", value={"n":123456789012345678901234567890})`
			childCode := fmt.Sprintf(`record = state.blackboard_get(key="exact")
state.blackboard_cas(key="exact", version=record["version"], value={"n":record["value"]["n"] + 1})
messages.send(recipient=%q, subject="exact", body=json.encode(record["value"]), delivery="next_turn")`, root.ID())
			if engine == "quickjs" {
				rootCode = `await state.blackboard_set({key:"exact",value:{n:123456789012345678901234567890n}});`
				childCode = fmt.Sprintf(`var record=await state.blackboard_get({key:"exact"});
await state.blackboard_cas({key:"exact",version:record.version,value:{n:record.value.n+1n}});
await messages.send({recipient:%q,subject:"exact",body:json.encode(record.value),delivery:"next_turn"});`, root.ID())
			}
			if _, err := runtime.rootNode.kernel.Exec(t.Context(), rootCode); err != nil {
				t.Fatal(err)
			}
			if _, err := runtime.agents["child"].kernel.Exec(t.Context(), childCode); err != nil {
				t.Fatal(err)
			}
			state, err := root.GetBlackboard(t.Context(), root.AgentID(), "exact")
			if err != nil || string(state.Payload.Inline) != `{"n":123456789012345678901234567891}` {
				t.Fatalf("CAS rounded integer: %s %v", state.Payload.Inline, err)
			}
			mail, err := root.ListMailboxMessages(t.Context(), root.AgentID(), "pending", "child", 10)
			if err != nil || len(mail) != 1 {
				t.Fatalf("mail=%+v %v", mail, err)
			}
			_, body, err := root.ReadMailboxMessage(t.Context(), root.AgentID(), mail[0].ID)
			if err != nil || string(body) != `{"n":123456789012345678901234567890}` {
				t.Fatalf("mail rounded integer: %s %v", body, err)
			}
		})
	}
}

func waitExecutionAgentIdle(t *testing.T, node *AgentSession) {
	t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		node.mu.Lock()
		running := node.running
		node.mu.Unlock()
		if !running {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("agent %s did not settle", node.id)
		}
	}
}
