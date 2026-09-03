package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

var recursiveKernelCommand = []string{os.Args[0], "-test.run=TestRecursiveRuntimeKernelWorker", "--"}

func TestRecursiveRuntimeKernelWorker(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		return
	}
	if err := rlm.WorkerMain(os.Args[separator+1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func openRecursiveRuntime(t *testing.T, client *llm.Client, maxWorkers int) (*session.Store, *Session, *RecursiveRuntime) {
	t.Helper()
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	var runtime *RecursiveRuntime
	owner, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
		value := agent.NewRuntime(client, "model", 1024, rlm.BuildPrompt(meta.CWD, nil), tools.NewServices())
		value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
		limits := rlm.DefaultLimits()
		limits.MaxWorkers = maxWorkers
		var runtimeErr error
		runtime, runtimeErr = NewRecursiveRuntime(RecursiveRuntimeOptions{
			Agent: value, History: history, Limits: limits, Kernels: rlm.NewManager(maxWorkers), KernelCommand: recursiveKernelCommand,
		})
		if runtimeErr != nil {
			return Components{}, runtimeErr
		}
		return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	return store, root, runtime
}

func TestRecursiveRuntimeUsesOneInterfaceAtEveryDepth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		streamText(w, "done")
	}))
	defer server.Close()
	_, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 4)
	runs := &sync.Map{}
	runtime.setRunTurnHook(observeRunTurn(runs))
	receipt, err := root.Submit(t.Context(), "root work")
	if err != nil || waitReceipt(t, receipt).Err != nil {
		t.Fatalf("root turn err=%v", err)
	}
	waitRunTurn(t, runs, root.AgentID(), 1)

	childResult, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "child", "prompt": "work"})
	if err != nil {
		t.Fatal(err)
	}
	childID := childResult.(map[string]any)["id"].(string)
	waitRunTurn(t, runs, childID, 1)
	child := runtime.agents[childID]
	waitAgentIdle(t, child)
	childRuns := runTurnCount(runs, childID)
	grandchildResult, err := child.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "grandchild", "prompt": "work"})
	if err != nil {
		t.Fatal(err)
	}
	grandchildID := grandchildResult.(map[string]any)["id"].(string)
	waitRunTurn(t, runs, grandchildID, 1)
	grandchild := runtime.agents[grandchildID]
	waitAgentIdle(t, grandchild)
	waitRunTurn(t, runs, childID, childRuns+1)
	waitAgentIdle(t, child)

	for _, node := range []*AgentSession{runtime.rootNode, child, grandchild} {
		all := node.agent.AllTools()
		if len(all) != 1 || all[0].Def.Function.Name != "rlm_exec" {
			t.Fatalf("agent %q tools = %#v", node.name, all)
		}
	}
	if _, err := grandchild.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "too-deep", "prompt": "work"}); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("third recursive edge error = %v", err)
	}
	relatives, err := runtime.root.ListAgentRelatives(t.Context(), grandchild.id)
	if err != nil {
		t.Fatal(err)
	}
	if len(relatives.Children) != 0 {
		t.Fatalf("rejected depth admission created children: %+v", relatives.Children)
	}
}

func observeRunTurn(runs *sync.Map) func(*AgentSession) {
	return func(node *AgentSession) {
		value, _ := runs.LoadOrStore(node.id, &atomic.Int32{})
		value.(*atomic.Int32).Add(1)
	}
}

func runTurnCount(runs *sync.Map, id string) int32 {
	value, ok := runs.Load(id)
	if !ok {
		return 0
	}
	return value.(*atomic.Int32).Load()
}

func waitRunTurn(t *testing.T, runs *sync.Map, id string, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runTurnCount(runs, id) >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("RunTurn hook observed %q %d times, want at least %d", id, runTurnCount(runs, id), want)
}

func waitAgentIdle(t *testing.T, node *AgentSession) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		node.mu.Lock()
		running := node.running
		node.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("agent %q did not become idle", node.id)
}

func TestRecursiveSpawnQueuesUnderWorkerPressure(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1)
	result, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "queued", "prompt": "work"})
	if err != nil {
		t.Fatalf("spawn error = %v", err)
	}
	if result.(map[string]any)["status"] != "queued" {
		t.Fatalf("spawn result = %#v", result)
	}
	relatives, err := store.ListAgentRelatives(t.Context(), root.ID(), root.AgentID())
	if err != nil {
		t.Fatal(err)
	}
	if len(relatives.Children) != 1 {
		t.Fatalf("queued child was not admitted: %+v", relatives.Children)
	}
}

func TestRecursiveChildrenInheritOrExplicitlyOverrideOneAgentConfiguration(t *testing.T) {
	parent := agent.NewRuntime(llm.New("https://parent.test", "key"), "parent-api-model", 512, "system", tools.NewServices())
	parent.ModelName = "parent-model"
	parent.Provider = "parent-provider"
	parent.Effort = "medium"
	parent.WorkingDir = t.TempDir()
	parent.ContextLimit = 4096
	parent.ResolveModel = func(model, provider string) (agent.ModelRoute, error) {
		if model != "child-model" || provider != "" {
			return agent.ModelRoute{}, errors.New("unexpected model override")
		}
		return agent.ModelRoute{
			Client: llm.New("https://child.test", "key"), ModelName: model, Provider: "child-provider",
			Model: "child-api-model", ContextLimit: 8192, MaxTokens: 1024, Effort: "high",
		}, nil
	}

	inherited, inheritedModel, inheritedProvider, err := cloneRuntimeAgent(parent, tools.NewServices(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer inherited.Services.Close()
	if inheritedModel != parent.ModelName || inheritedProvider != parent.Provider || inherited.Effort != parent.Effort || inherited.WorkingDir != parent.WorkingDir || inherited.ContextLimit != parent.ContextLimit {
		t.Fatalf("inherited child = model %q provider %q effort %q cwd %q context %d", inheritedModel, inheritedProvider, inherited.Effort, inherited.WorkingDir, inherited.ContextLimit)
	}

	overridden, modelName, providerName, err := cloneRuntimeAgent(parent, tools.NewServices(), map[string]any{"model": "child-model"})
	if err != nil {
		t.Fatal(err)
	}
	defer overridden.Services.Close()
	if modelName != "child-model" || providerName != "child-provider" || overridden.Model != "child-api-model" || overridden.Effort != "high" || overridden.ContextLimit != 8192 || overridden.MaxTokens != 1024 {
		t.Fatalf("overridden child = model %q provider %q api %q effort %q context %d output %d", modelName, providerName, overridden.Model, overridden.Effort, overridden.ContextLimit, overridden.MaxTokens)
	}
	if _, _, _, err := cloneRuntimeAgent(parent, tools.NewServices(), map[string]any{"provider": "child-provider"}); err == nil {
		t.Fatal("provider-only child override was accepted")
	}
}

func TestRecursiveAgentClientControlsAuthorizeAndPersist(t *testing.T) {
	store, root, runtime := openRecursiveRuntime(t, llm.New("http://127.0.0.1:1", "key"), 1)
	_, _, release, err := runtime.rootNode.kernel.AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	spawn := func(name string) string {
		t.Helper()
		result, spawnErr := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
			"name": name, "prompt": "remain queued for control testing",
		})
		if spawnErr != nil {
			t.Fatal(spawnErr)
		}
		return result.(map[string]any)["id"].(string)
	}

	stoppedID := spawn("stopped-child")
	if result := clientCommand(t, root, "tui", "list-agents", "agents.list", map[string]string{}); result.Status != "succeeded" || !strings.Contains(result.Output, stoppedID) {
		t.Fatalf("agents list = %+v", result)
	}
	if result := clientCommand(t, root, "tui", "cap-budget", "budget.cap", map[string]string{"args": stoppedID + " tokens 7"}); result.Status != "succeeded" || !strings.Contains(result.Output, `"Limit":7`) {
		t.Fatalf("agent budget = %+v", result)
	}
	if result := clientCommand(t, root, "tui", "revoke-capability", "capability.revoke", map[string]string{"args": "files:" + stoppedID}); result.Status != "succeeded" || !strings.Contains(result.Output, `"Status":"revoked"`) {
		t.Fatalf("agent capability revoke = %+v", result)
	}
	if result := clientCommand(t, root, "tui", "stop-agent", "agent.control", map[string]string{"args": stoppedID}); result.Status != "succeeded" || result.Output != "stopped" {
		t.Fatalf("agent stop = %+v", result)
	}
	relatives, err := store.ListAgentRelatives(t.Context(), root.ID(), root.AgentID())
	if err != nil || runtimeAgentStatus(relatives.Children, stoppedID) != "stopped" {
		t.Fatalf("stopped relatives = %+v, %v", relatives, err)
	}

	deletedID := spawn("deleted-child")
	if result := clientCommand(t, root, "tui", "delete-agent", "agent.delete", map[string]string{"args": deletedID}); result.Status != "succeeded" || result.Output != "deleted" {
		t.Fatalf("agent delete = %+v", result)
	}
	relatives, err = store.ListAgentRelatives(t.Context(), root.ID(), root.AgentID())
	if err != nil || runtimeAgentStatus(relatives.Children, deletedID) != "deleted" {
		t.Fatalf("deleted relatives = %+v, %v", relatives, err)
	}
}

func runtimeAgentStatus(agents []session.RuntimeAgent, id string) string {
	for _, runtimeAgent := range agents {
		if runtimeAgent.ID == id {
			return runtimeAgent.Status
		}
	}
	return ""
}

func TestChildResponseIsLocalAndMessageBodyStaysOutOfParentContext(t *testing.T) {
	var mu sync.Mutex
	var requests []llm.Request
	var rootID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, input)
		mu.Unlock()
		last := input.Messages[len(input.Messages)-1]
		switch {
		case last.Role == "user" && strings.Contains(last.Content, "delegate now"):
			streamToolCall(w, "spawn", `agents.spawn(name="worker", prompt="do work")`)
		case last.Role == "user" && strings.Contains(last.Content, "do work"):
			code := fmt.Sprintf(`messages.send(recipient=%q, subject="done", body="private child result")`, rootID)
			streamToolCall(w, "report", code)
		default:
			streamText(w, "local completion")
		}
	}))
	defer server.Close()
	store, root, _ := openRecursiveRuntime(t, llm.New(server.URL, "key"), 4)
	rootID = root.ID()
	receipt, err := root.Submit(t.Context(), "delegate now")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}

	deadline := time.Now().Add(5 * time.Second)
	var messages []session.MailboxMessage
	for len(messages) == 0 && time.Now().Before(deadline) {
		all, err := store.ListMailboxMessages(t.Context(), rootID, rootID, "all", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		messages = messages[:0]
		for _, message := range all {
			if message.Kind == session.MessageKindMessage {
				messages = append(messages, message)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(messages) != 1 {
		t.Fatalf("root mailbox = %+v", messages)
	}
	message, err := store.ReadMailboxMessage(t.Context(), rootID, rootID, messages[0].ID)
	if err != nil || string(message.Body.Inline) != "private child result" {
		t.Fatalf("message = %+v, %v", message, err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, request := range requests {
		isParent := false
		for _, message := range request.Messages {
			isParent = isParent || strings.Contains(message.Content, "delegate now")
		}
		if isParent {
			// The body may reach the parent only as a bounded digest line, never as
			// the child's raw transcript or tool output.
			for _, message := range request.Messages {
				if strings.Contains(message.Content, "private child result") && !strings.HasPrefix(message.Content, "Mailbox digest:") {
					t.Fatalf("child body entered parent context outside the digest: %+v", message)
				}
				if message.Role != "user" && strings.Contains(message.Content, "private child result") {
					t.Fatalf("child body entered parent context as %s: %+v", message.Role, message)
				}
			}
		}
		if len(request.Tools) != 1 || request.Tools[0].Function.Name != "rlm_exec" {
			t.Fatalf("model-facing tools = %#v", request.Tools)
		}
	}
}

// TestQueuedMailWakesIdleRootWithoutHumanInput pins the doctrine the system
// prompt states: a queued message to an idle node starts a mailbox-triggered
// turn whose input is the digest, with no human submit in between.
func TestQueuedMailWakesIdleRootWithoutHumanInput(t *testing.T) {
	var mu sync.Mutex
	var digestInputs []string
	var rootID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		last := input.Messages[len(input.Messages)-1]
		switch {
		case last.Role == "user" && strings.Contains(last.Content, "delegate now"):
			streamToolCall(w, "spawn", `agents.spawn(name="worker", prompt="do work")`)
		case last.Role == "user" && strings.Contains(last.Content, "do work"):
			streamToolCall(w, "report", `messages.send(recipient="parent", subject="pong", body="worker says hello")`)
		case last.Role == "user" && strings.HasPrefix(last.Content, "Mailbox digest:"):
			mu.Lock()
			digestInputs = append(digestInputs, last.Content)
			mu.Unlock()
			streamText(w, "noted the pong")
		default:
			streamText(w, "done")
		}
	}))
	defer server.Close()
	store, root, _ := openRecursiveRuntime(t, llm.New(server.URL, "key"), 4)
	rootID = root.ID()
	receipt, err := root.Submit(t.Context(), "delegate now")
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	// The only human input was the submit above. The child's reply must be
	// delivered by a root turn the runtime starts on its own.
	deadline := time.Now().Add(5 * time.Second)
	var delivered []session.MailboxMessage
	for len(delivered) == 0 && time.Now().Before(deadline) {
		all, err := store.ListMailboxMessages(t.Context(), rootID, rootID, "all", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, message := range all {
			if message.Kind == session.MessageKindMessage && message.Status != "pending" {
				delivered = append(delivered, message)
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(delivered) != 1 || delivered[0].Subject != "pong" {
		t.Fatalf("child reply was not delivered by a runtime-started turn: %+v", delivered)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(digestInputs) != 1 || !strings.Contains(digestInputs[0], "subject=\"pong\"") || !strings.Contains(digestInputs[0], "worker says hello") {
		t.Fatalf("root digest turns = %q", digestInputs)
	}
	queued, err := store.LoadQueuedInbox(t.Context(), rootID, rootID, 0, 10)
	if err != nil || len(queued) != 0 {
		t.Fatalf("mailbox turn left inbox rows behind: %+v, %v", queued, err)
	}
}

func TestRecursiveRuntimeRestoresRetainedAgentAndTranscript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		streamText(w, "remembered child turn")
	}))
	defer server.Close()
	databasePath := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, databasePath)
	rootID := createRoot(t, store)
	makeOwner := func(store *session.Store) (*Daemon, **RecursiveRuntime) {
		var runtime *RecursiveRuntime
		owner, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
			value := agent.NewRuntime(llm.New(server.URL, "key"), "model", 1024, rlm.BuildPrompt(meta.CWD, nil), tools.NewServices())
			value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
			limits := rlm.DefaultLimits()
			var runtimeErr error
			runtime, runtimeErr = NewRecursiveRuntime(RecursiveRuntimeOptions{
				Agent: value, History: history, Limits: limits, Kernels: rlm.NewManager(limits.MaxWorkers), KernelCommand: recursiveKernelCommand,
			})
			if runtimeErr != nil {
				return Components{}, runtimeErr
			}
			return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return owner, &runtime
	}

	owner, firstRef := makeOwner(store)
	root, err := owner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	first := *firstRef
	spawned, err := first.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{"name": "retained", "prompt": "remember this"})
	if err != nil {
		t.Fatal(err)
	}
	childID := spawned.(map[string]any)["id"].(string)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		transcript, loadErr := store.LoadAgentTranscript(t.Context(), rootID, childID)
		if loadErr == nil && len(transcript) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	transcript, err := store.LoadAgentTranscript(t.Context(), rootID, childID)
	if err != nil || len(transcript) == 0 {
		t.Fatalf("first transcript = %+v, %v", transcript, err)
	}
	waitAgentIdle(t, first.agents[childID])
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	store = openStore(t, databasePath)
	secondOwner, secondRef := makeOwner(store)
	t.Cleanup(func() { _ = secondOwner.Close() })
	secondRoot, err := secondOwner.Open(root.ID())
	if err != nil {
		t.Fatal(err)
	}
	second := *secondRef
	restored := second.agents[childID]
	if restored == nil {
		t.Fatalf("retained child %q was not restored", childID)
	}
	if all := restored.agent.AllTools(); len(all) != 1 || all[0].Def.Function.Name != "rlm_exec" {
		t.Fatalf("restored tools = %#v", all)
	}
	if history := restored.agent.MessagesSnapshot(); len(history) < 2 || !strings.Contains(history[len(history)-1].Content, "remembered child turn") {
		t.Fatalf("restored history = %+v", history)
	}
	runs := &sync.Map{}
	second.setRunTurnHook(observeRunTurn(runs))
	if _, err := secondRoot.SendMailboxMessage(t.Context(), root.ID(), childID, session.MailboxSend{Subject: "restored", Body: "status: restored"}); err != nil {
		t.Fatal(err)
	}
	waitRunTurn(t, runs, childID, 1)
	waitAgentIdle(t, restored)
}

func TestQueuedInitialAgentPromptSurvivesRestartExactlyOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// Count only the child's prompt turn; the root's mailbox turn for the
		// child's completion notice is a separate, expected call.
		if strings.Contains(input.Messages[len(input.Messages)-1].TextContent(), "run once after restart") {
			calls.Add(1)
		}
		streamText(w, "restored queued prompt")
	}))
	defer server.Close()

	databasePath := filepath.Join(t.TempDir(), "sessions.db")
	workspace := t.TempDir()
	store := openStore(t, databasePath)
	rootID, err := store.Create(session.SessionKindAgent, workspace, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	makeOwner := func(store *session.Store) (*Daemon, **RecursiveRuntime) {
		var runtime *RecursiveRuntime
		owner, ownerErr := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
			value := agent.NewRuntime(llm.New(server.URL, "key"), "model", 1024, rlm.BuildPrompt(meta.CWD, nil), tools.NewServices())
			value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
			limits := rlm.DefaultLimits()
			limits.MaxWorkers = 1
			var runtimeErr error
			runtime, runtimeErr = NewRecursiveRuntime(RecursiveRuntimeOptions{
				Agent: value, History: history, Limits: limits, Kernels: rlm.NewManager(1), KernelCommand: recursiveKernelCommand,
			})
			if runtimeErr != nil {
				return Components{}, runtimeErr
			}
			return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind}, nil
		})
		if ownerErr != nil {
			t.Fatal(ownerErr)
		}
		return owner, &runtime
	}

	firstOwner, firstRuntimeRef := makeOwner(store)
	if _, err := firstOwner.Open(rootID); err != nil {
		t.Fatal(err)
	}
	firstRuntime := *firstRuntimeRef
	_, _, release, err := firstRuntime.rootNode.kernel.AcquireTurn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	spawned, err := firstRuntime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
		"name": "queued-across-restart", "prompt": "run once after restart",
	})
	if err != nil {
		t.Fatal(err)
	}
	childID := spawned.(map[string]any)["id"].(string)
	queued, err := store.LoadQueuedInbox(t.Context(), rootID, childID, 0, session.MaxInboxBatch)
	if err != nil || len(queued) != 1 || !strings.Contains(string(queued[0].Payload.Inline), "run once after restart") || !strings.HasPrefix(string(queued[0].Payload.Inline), "[task from parent root") {
		t.Fatalf("queued prompt=%+v err=%v", queued, err)
	}
	if calls.Load() != 0 {
		t.Fatalf("provider calls before restart=%d", calls.Load())
	}
	if err := firstOwner.Close(); err != nil {
		t.Fatal(err)
	}
	release()

	store = openStore(t, databasePath)
	secondOwner, secondRuntimeRef := makeOwner(store)
	t.Cleanup(func() { _ = secondOwner.Close() })
	if _, err := secondOwner.Open(rootID); err != nil {
		t.Fatal(err)
	}
	secondRuntime := *secondRuntimeRef
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		transcript, loadErr := store.LoadAgentTranscript(t.Context(), rootID, childID)
		if loadErr == nil && len(transcript) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	transcript, err := store.LoadAgentTranscript(t.Context(), rootID, childID)
	if err != nil || len(transcript) < 2 || !strings.Contains(transcript[len(transcript)-1].Content, "restored queued prompt") {
		t.Fatalf("restored transcript=%+v err=%v", transcript, err)
	}
	if secondRuntime.agents[childID] == nil || calls.Load() != 1 {
		t.Fatalf("restored child=%v provider calls=%d", secondRuntime.agents[childID] != nil, calls.Load())
	}
	queued, err = store.LoadQueuedInbox(t.Context(), rootID, childID, 0, session.MaxInboxBatch)
	if err != nil || len(queued) != 0 {
		t.Fatalf("prompt replay queue=%+v err=%v", queued, err)
	}
}

func TestConcurrentChildTurnPressureKeepsPromptQueued(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var firstOnce, secondOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		last := input.Messages[len(input.Messages)-1].TextContent()
		switch {
		case strings.Contains(last, "first child prompt"):
			firstOnce.Do(func() { close(firstStarted) })
			<-releaseFirst
		case strings.Contains(last, "second child prompt"):
			secondOnce.Do(func() { close(secondStarted) })
		}
		streamText(w, "done")
	}))
	defer server.Close()
	store, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 4)
	if err := store.SetBudgetLimit(t.Context(), root.ID(), "", session.BudgetConcurrentChildTurns, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
		"name": "first", "prompt": "first child prompt",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("first child did not start")
	}
	spawned, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
		"name": "second", "prompt": "second child prompt",
	})
	if err != nil {
		t.Fatal(err)
	}
	secondID := spawned.(map[string]any)["id"].(string)
	time.Sleep(100 * time.Millisecond)
	select {
	case <-secondStarted:
		t.Fatal("second child ran while the concurrent-child budget was exhausted")
	default:
	}
	queued, err := store.LoadQueuedInbox(t.Context(), root.ID(), secondID, 0, session.MaxInboxBatch)
	if err != nil || len(queued) != 1 || queued[0].Status != "queued" {
		t.Fatalf("second child prompt=%+v err=%v", queued, err)
	}
	close(releaseFirst)
	select {
	case <-secondStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("queued child did not start after capacity was released")
	}
	waitAgentIdle(t, runtime.agents[secondID])
}

func TestSuspendedKernelNoticeIsEphemeral(t *testing.T) {
	var mu sync.Mutex
	var requests []llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, input)
		mu.Unlock()
		streamText(w, "done")
	}))
	defer server.Close()
	_, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 1)
	first, err := root.Submit(t.Context(), "first")
	if err != nil || waitReceipt(t, first).Err != nil {
		t.Fatalf("first turn err=%v", err)
	}
	if err := runtime.rootNode.kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	second, err := root.Submit(t.Context(), "second")
	if err != nil || waitReceipt(t, second).Err != nil {
		t.Fatalf("second turn err=%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	encoded, _ := json.Marshal(requests[1].Messages)
	if !strings.Contains(string(encoded), "globals were cleared") {
		t.Fatalf("restart notice missing from provider request: %s", encoded)
	}
	history := runtime.rootNode.agent.MessagesSnapshot()
	encoded, _ = json.Marshal(history)
	if strings.Contains(string(encoded), "globals were cleared") {
		t.Fatalf("restart notice entered durable history: %s", encoded)
	}
}

// TestSuspendedKernelRestoresScratchWithEphemeralNotice pins the doctrine:
// globals defined in one turn survive a worker suspension, the next turn's
// requests carry a notice naming what was restored, and nothing about the
// restore enters durable history.
func TestSuspendedKernelRestoresScratchWithEphemeralNotice(t *testing.T) {
	var mu sync.Mutex
	var requests []llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, input)
		mu.Unlock()
		// The ephemeral restart notice is appended after the user message, so
		// key on the last message that is not a system notice.
		last := input.Messages[len(input.Messages)-1]
		for index := len(input.Messages) - 1; index >= 0 && last.Role == "system"; index-- {
			last = input.Messages[index]
		}
		switch {
		case last.Role == "user" && strings.Contains(last.Content, "remember"):
			streamToolCall(w, "define", "kids = [1, 2]\ndef pick(i):\n    return kids[i]")
		case last.Role == "user" && strings.Contains(last.Content, "recall"):
			streamToolCall(w, "use", "pick(1)")
		default:
			streamText(w, "done")
		}
	}))
	defer server.Close()
	store, root, runtime := openRecursiveRuntime(t, llm.New(server.URL, "key"), 1)
	first, err := root.Submit(t.Context(), "remember")
	if err != nil || waitReceipt(t, first).Err != nil {
		t.Fatalf("first turn err=%v", err)
	}
	program, _, err := store.LoadAgentScratch(t.Context(), root.ID(), root.AgentID())
	if err != nil || !strings.Contains(program, "kids = [1, 2]") || !strings.Contains(program, "def pick(i):") {
		t.Fatalf("stored scratch = %q err=%v", program, err)
	}
	if err := runtime.rootNode.kernel.Suspend(); err != nil {
		t.Fatal(err)
	}
	second, err := root.Submit(t.Context(), "recall")
	if err != nil || waitReceipt(t, second).Err != nil {
		t.Fatalf("second turn err=%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	noticed, answered := false, false
	for _, request := range requests {
		for _, message := range request.Messages {
			if message.Role == "system" && strings.Contains(message.Content, "Scratch restored: kids, pick (2)") {
				noticed = true
			}
			if message.Role == "tool" && strings.Contains(message.Content, `"value":2`) {
				answered = true
			}
		}
	}
	if !noticed || !answered {
		encoded, _ := json.Marshal(requests[len(requests)-1].Messages)
		t.Fatalf("noticed=%v answered=%v requests=%d last=%s", noticed, answered, len(requests), encoded)
	}
	encoded, _ := json.Marshal(runtime.rootNode.agent.MessagesSnapshot())
	if strings.Contains(string(encoded), "Scratch restored") {
		t.Fatalf("restore notice entered durable history: %s", encoded)
	}
	// The restore is also durable as an actor event, recorded off the kernel
	// lock, so poll briefly.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events, _, err := store.ReplayEvents(t.Context(), root.ID(), 0, session.MaxEventReplay)
		if err != nil {
			t.Fatal(err)
		}
		for _, envelope := range events {
			if envelope.Kind != "scratch.restored" {
				continue
			}
			var event session.LifecycleEvent
			if err := json.Unmarshal(envelope.Payload.Inline, &event); err != nil {
				t.Fatal(err)
			}
			if event.AgentID != root.AgentID() || strings.Join(event.Restored, ",") != "kids,pick" || len(event.NotRestored) != 0 {
				t.Fatalf("scratch.restored event = %+v", event)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("scratch.restored event was not recorded")
}

func streamToolCall(w http.ResponseWriter, id, code string) {
	w.Header().Set("Content-Type", "text/event-stream")
	arguments, _ := json.Marshal(map[string]string{"code": code})
	event := map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"tool_calls": []any{map[string]any{
			"index": 0, "id": id, "type": "function", "function": map[string]any{"name": "rlm_exec", "arguments": string(arguments)},
		}}},
		"finish_reason": "tool_calls",
	}}}
	body, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", body)
}

func streamText(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "text/event-stream")
	event := map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": text}}}}
	body, _ := json.Marshal(event)
	fmt.Fprintf(w, "data: %s\n\n", body)
}

// TestChildScratchSurvivesDaemonRestart proves the store-backed path end to
// end: a child's globals written under one daemon owner are revived by the
// fresh worker a second owner starts for the restored child.
func TestChildScratchSurvivesDaemonRestart(t *testing.T) {
	var mu sync.Mutex
	var requests []llm.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var input llm.Request
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, input)
		mu.Unlock()
		last := input.Messages[len(input.Messages)-1]
		for index := len(input.Messages) - 1; index >= 0 && last.Role == "system"; index-- {
			last = input.Messages[index]
		}
		switch {
		case last.Role == "user" && strings.Contains(last.Content, "delegate now"):
			streamToolCall(w, "spawn", `agents.spawn(name="keeper", prompt="remember the memo")`)
		case last.Role == "user" && strings.Contains(last.Content, "remember the memo"):
			streamToolCall(w, "define", `memo = "kept"`)
		case last.Role == "user" && strings.HasPrefix(last.Content, "Mailbox digest:") && strings.Contains(last.Content, "wake"):
			streamToolCall(w, "use", `memo`)
		default:
			streamText(w, "done")
		}
	}))
	defer server.Close()

	databasePath := filepath.Join(t.TempDir(), "sessions.db")
	workspace := t.TempDir()
	store := openStore(t, databasePath)
	rootID, err := store.Create(session.SessionKindAgent, workspace, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	makeOwner := func(store *session.Store) (*Daemon, **RecursiveRuntime) {
		var runtime *RecursiveRuntime
		owner, ownerErr := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
			value := agent.NewRuntime(llm.New(server.URL, "key"), "model", 1024, rlm.BuildPrompt(meta.CWD, nil), tools.NewServices())
			value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
			var runtimeErr error
			runtime, runtimeErr = NewRecursiveRuntime(RecursiveRuntimeOptions{
				Agent: value, History: history, Limits: rlm.DefaultLimits(), Kernels: rlm.NewManager(2), KernelCommand: recursiveKernelCommand,
			})
			if runtimeErr != nil {
				return Components{}, runtimeErr
			}
			return Components{Runner: runtime.RootSession(), Runtime: runtime, Bind: runtime.Bind}, nil
		})
		if ownerErr != nil {
			t.Fatal(ownerErr)
		}
		return owner, &runtime
	}

	firstOwner, firstRef := makeOwner(store)
	firstRoot, err := firstOwner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := firstRoot.Submit(t.Context(), "delegate now")
	if err != nil || waitReceipt(t, receipt).Err != nil {
		t.Fatalf("delegation err=%v", err)
	}
	var childID string
	deadline := time.Now().Add(5 * time.Second)
	for childID == "" && time.Now().Before(deadline) {
		if agents, err := store.LoadRetainedAgents(t.Context(), rootID); err == nil {
			for _, record := range agents {
				if record.Name == "keeper" {
					childID = record.ID
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childID == "" {
		t.Fatal("child was not admitted")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if program, _, err := store.LoadAgentScratch(t.Context(), rootID, childID); err == nil && strings.Contains(program, `memo = "kept"`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if program, _, err := store.LoadAgentScratch(t.Context(), rootID, childID); err != nil || !strings.Contains(program, `memo = "kept"`) {
		t.Fatalf("child scratch = %q err=%v", program, err)
	}
	waitAgentIdle(t, (*firstRef).agents[childID])
	if err := firstOwner.Close(); err != nil {
		t.Fatal(err)
	}

	store = openStore(t, databasePath)
	secondOwner, secondRef := makeOwner(store)
	t.Cleanup(func() { _ = secondOwner.Close() })
	secondRoot, err := secondOwner.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	restored := (*secondRef).agents[childID]
	if restored == nil {
		t.Fatal("child was not restored")
	}
	runs := &sync.Map{}
	(*secondRef).setRunTurnHook(observeRunTurn(runs))
	if _, err := secondRoot.SendMailboxMessage(t.Context(), secondRoot.AgentID(), childID, session.MailboxSend{Subject: "wake", Body: "wake up"}); err != nil {
		t.Fatal(err)
	}
	waitRunTurn(t, runs, childID, 1)
	waitAgentIdle(t, restored)
	mu.Lock()
	defer mu.Unlock()
	noticed, answered := false, false
	for _, request := range requests {
		for _, message := range request.Messages {
			if message.Role == "system" && strings.Contains(message.Content, "Scratch restored: memo (1)") {
				noticed = true
			}
			if message.Role == "tool" && strings.Contains(message.Content, `"value":"kept"`) {
				answered = true
			}
		}
	}
	if !noticed || !answered {
		encoded, _ := json.Marshal(requests[len(requests)-1].Messages)
		t.Fatalf("noticed=%v answered=%v requests=%d last=%s", noticed, answered, len(requests), encoded)
	}
}
