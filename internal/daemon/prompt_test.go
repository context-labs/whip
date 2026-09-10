package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestPromptRootCWDReloadAndRestorePreserveApplicableSources(t *testing.T) {
	requests, client := promptRuntimeProvider(t)
	database := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, database)
	workspace := canonicalPromptDirectory(t, t.TempDir())
	rootID, err := store.Create(session.SessionKindAgent, workspace, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	firstDir, secondDir := filepath.Join(workspace, "first"), filepath.Join(workspace, "second")
	writeDaemonPromptFile(t, filepath.Join(workspace, "AGENTS.md"), "PARENT_RULE_BEFORE_RELOAD")
	writeDaemonPromptFile(t, filepath.Join(firstDir, "AGENTS.md"), "FIRST_SUBTREE_RULE")
	writeDaemonPromptFile(t, filepath.Join(secondDir, "AGENTS.md"), "SECOND_SUBTREE_RULE")
	parentSkill := filepath.Join(workspace, ".agents", "skills", "parent-skill", "SKILL.md")
	writeDaemonPromptFile(t, parentSkill, "---\nname: parent-skill\ndescription: PARENT_CATALOG_MARKER\n---\n")
	standing := filepath.Join(os.Getenv("WHIP_HOME"), "me.md")
	writeDaemonPromptFile(t, standing, "STANDING_BEFORE_EDIT")
	owner, root, _ := openPromptRuntime(t, store, rootID, client)

	initial := root.runner.(*AgentSession).ContextAudit()
	if !promptAuditContains(initial, "not applied yet") || promptAuditContains(initial, parentSkill) {
		t.Fatalf("constructor audit overstated applied environment: %+v", initial)
	}
	baseRequest := submitPromptRoot(t, root, requests, "initial root task")
	assertProviderPrompt(t, baseRequest, []string{"PARENT_RULE_BEFORE_RELOAD", "PARENT_CATALOG_MARKER", "STANDING_BEFORE_EDIT", "Identity: root agent"}, nil)
	assertAppliedPromptAudit(t, root.runner.(*AgentSession), baseRequest, workspace, parentSkill)

	for i, directory := range []string{firstDir, secondDir} {
		before := root.runner.(*AgentSession).ContextAudit()
		command := clientCommand(t, root, "prompt-client", fmt.Sprintf("cd-%d", i), "workspace.set", map[string]any{"path": directory})
		if command.Status != "succeeded" {
			t.Fatalf("cwd command = %+v", command)
		}
		pending := root.runner.(*AgentSession).ContextAudit()
		if pending.WorkingDirectory != before.WorkingDirectory || !promptAuditContains(pending, "changes apply next turn") || promptAuditContains(pending, filepath.Join(directory, "AGENTS.md")) {
			t.Fatalf("pending cwd was reported as already applied: before=%+v, pending=%+v", before, pending)
		}
		want, unwanted := "FIRST_SUBTREE_RULE", "SECOND_SUBTREE_RULE"
		if i == 1 {
			want, unwanted = unwanted, want
			writeDaemonPromptFile(t, standing, "STANDING_AFTER_EDIT")
		}
		request := submitPromptRoot(t, root, requests, "task after cwd change")
		assertProviderPrompt(t, request, []string{"PARENT_RULE_BEFORE_RELOAD", "PARENT_CATALOG_MARKER", want, "Working directory: " + directory}, []string{unwanted})
		if i == 1 {
			assertProviderPrompt(t, request, []string{"STANDING_AFTER_EDIT"}, []string{"STANDING_BEFORE_EDIT"})
		}
		assertAppliedPromptAudit(t, root.runner.(*AgentSession), request, directory, parentSkill, filepath.Join(directory, "AGENTS.md"))
	}

	newSkill := filepath.Join(secondDir, ".agents", "skills", "new-skill", "SKILL.md")
	writeDaemonPromptFile(t, newSkill, "---\nname: new-skill\ndescription: NEW_CATALOG_AFTER_EDIT\n---\n")
	if promptAuditContains(root.runner.(*AgentSession).ContextAudit(), newSkill) {
		t.Fatal("context audit rescanned and advertised a skill absent from the applied prompt")
	}
	writeDaemonPromptFile(t, filepath.Join(workspace, "AGENTS.md"), "PARENT_RULE_AFTER_RELOAD")
	for _, operation := range []string{"session.reload", "session.model"} {
		payload := map[string]string{}
		if operation == "session.model" {
			payload["model"], payload["provider"] = "replacement-model", "provider"
		}
		command := clientCommand(t, root, "prompt-client", operation, operation, payload)
		if command.Status != "succeeded" {
			t.Fatalf("%s = %+v", operation, command)
		}
		if audit := root.runner.(*AgentSession).ContextAudit(); !promptAuditContains(audit, "not applied yet") || promptAuditContains(audit, newSkill) {
			t.Fatalf("rebuilt runner claimed unapplied sources: %+v", audit)
		}
		request := submitPromptRoot(t, root, requests, "task after runtime replacement")
		assertProviderPrompt(t, request, []string{"PARENT_RULE_AFTER_RELOAD", "PARENT_CATALOG_MARKER", "SECOND_SUBTREE_RULE", "NEW_CATALOG_AFTER_EDIT", "STANDING_AFTER_EDIT"}, []string{"PARENT_RULE_BEFORE_RELOAD", "FIRST_SUBTREE_RULE"})
		assertAppliedPromptAudit(t, root.runner.(*AgentSession), request, secondDir, parentSkill, newSkill)
		if operation == "session.model" && request.Model != "replacement-model" {
			t.Fatalf("replacement model not used: %q", request.Model)
		}
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	store = openStore(t, database)
	_, root, _ = openPromptRuntime(t, store, rootID, client)
	request := submitPromptRoot(t, root, requests, "task after daemon restart")
	assertProviderPrompt(t, request, []string{"PARENT_RULE_AFTER_RELOAD", "PARENT_CATALOG_MARKER", "SECOND_SUBTREE_RULE", "NEW_CATALOG_AFTER_EDIT", "STANDING_AFTER_EDIT"}, []string{"FIRST_SUBTREE_RULE"})
	assertAppliedPromptAudit(t, root.runner.(*AgentSession), request, secondDir, parentSkill, newSkill)
}

func TestPromptFullAccessOutsideContextAndDowngrade(t *testing.T) {
	requests, client := promptRuntimeProvider(t)
	parent := canonicalPromptDirectory(t, t.TempDir())
	workspace, outside := filepath.Join(parent, "project"), filepath.Join(parent, "sibling")
	writeDaemonPromptFile(t, filepath.Join(parent, "AGENTS.md"), strings.Repeat("X", 128<<10))
	writeDaemonPromptFile(t, filepath.Join(workspace, "AGENTS.md"), "ORIGINAL_PROJECT_RULE")
	outsideInstructions := filepath.Join(outside, "AGENTS.md")
	outsideSkill := filepath.Join(outside, ".agents", "skills", "sibling", "SKILL.md")
	writeDaemonPromptFile(t, outsideInstructions, "SIBLING_PROJECT_RULE")
	writeDaemonPromptFile(t, outsideSkill, "---\ndescription: SIBLING_PROJECT_SKILL\n---\n")
	writeDaemonPromptFile(t, filepath.Join(os.Getenv("WHIP_HOME"), "me.md"), "GLOBAL_USER_RULE")
	writeDaemonPromptFile(t, filepath.Join(os.Getenv("WHIP_HOME"), "skills", "global", "SKILL.md"), "---\ndescription: GLOBAL_USER_SKILL\n---\n")
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID, err := store.Create(session.SessionKindAgent, workspace, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetPermissionMode(t.Context(), rootID, session.PermissionModeAutomatic); err != nil {
		t.Fatal(err)
	}
	_, root, runtime := openPromptRuntime(t, store, rootID, client)
	changed := clientCommand(t, root, "prompt-client", "external-cwd", "workspace.set", map[string]any{"path": outside})
	if changed.Status != "succeeded" {
		t.Fatalf("outside cwd change: %+v", changed)
	}
	full := submitPromptRoot(t, root, requests, "inspect the current project")
	assertProviderPrompt(t, full,
		[]string{"SIBLING_PROJECT_RULE", "SIBLING_PROJECT_SKILL", "GLOBAL_USER_RULE", "GLOBAL_USER_SKILL", "Working directory: " + outside},
		[]string{"ORIGINAL_PROJECT_RULE"},
	)
	downgraded := clientCommand(t, root, "prompt-client", "ask", "permission.mode", map[string]any{"external_permissions": true})
	if downgraded.Status != "succeeded" {
		t.Fatalf("permission downgrade: %+v", downgraded)
	}
	// Invalid denied sources prove composition omits their reads, not just
	// their presentation after parsing.
	writeDaemonPromptFile(t, outsideInstructions, strings.Repeat("X", 128<<10))
	writeDaemonPromptFile(t, outsideSkill, "malformed metadata")
	ask := submitPromptRoot(t, root, requests, "continue with global context")
	assertProviderPrompt(t, ask,
		[]string{"GLOBAL_USER_RULE", "GLOBAL_USER_SKILL", "Working directory: " + outside},
		[]string{"SIBLING_PROJECT_RULE", "SIBLING_PROJECT_SKILL", "ORIGINAL_PROJECT_RULE"},
	)
	if promptAuditContains(runtime.rootNode.ContextAudit(), outsideInstructions) || promptAuditContains(runtime.rootNode.ContextAudit(), outsideSkill) {
		t.Fatal("downgraded context audit retained denied project sources")
	}
}

func TestPromptExplicitChildScopeExcludesParentSources(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIP_HOME", t.TempDir())
	workspace := canonicalPromptDirectory(t, t.TempDir())
	allowed := filepath.Join(workspace, "allowed")
	writeDaemonPromptFile(t, filepath.Join(workspace, "AGENTS.md"), strings.Repeat("X", 128<<10))
	writeDaemonPromptFile(t, filepath.Join(workspace, ".agents", "skills", "parent", "SKILL.md"), "malformed metadata")
	writeDaemonPromptFile(t, filepath.Join(allowed, "AGENTS.md"), "ALLOWED_CHILD_RULE")
	writeDaemonPromptFile(t, filepath.Join(allowed, ".agents", "skills", "child", "SKILL.md"), "---\ndescription: ALLOWED_CHILD_SKILL\n---\n")
	writeDaemonPromptFile(t, filepath.Join(os.Getenv("WHIP_HOME"), "me.md"), "GLOBAL_USER_RULE")
	root := storeBackedInputSession(t, workspace)
	root.root.authority = root.authority
	if err := root.root.store.SetPermissionMode(t.Context(), root.id, session.PermissionModeAutomatic); err != nil {
		t.Fatal(err)
	}
	childID := "restricted-child"
	if _, err := root.root.store.AdmitAgent(t.Context(), session.AgentAdmission{
		RootID: root.id, ParentAgentID: root.id, ChildAgentID: childID,
		Capabilities: []session.CapabilityDelegation{{
			ID: "restricted-files", Issuer: root.authority.Files, AgentID: childID,
			Operations: []string{"read"}, Scopes: []string{allowed},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	child := inputTestSession(t, allowed)
	child.id, child.parentID, child.root = childID, root.id, root.root
	child.authority.Files = capability.Reference{ID: "restricted-files", Generation: 1}
	if err := child.refreshPrompt(t.Context()); err != nil {
		t.Fatalf("restricted child read denied ancestor sources: %v", err)
	}
	if !strings.Contains(child.prompt.Prompt, "ALLOWED_CHILD_RULE") || !strings.Contains(child.prompt.Prompt, "ALLOWED_CHILD_SKILL") {
		t.Fatal("restricted child lost authorized local context")
	}
	child.agent.WorkingDir = workspace
	if err := child.refreshPrompt(t.Context()); err != nil {
		t.Fatalf("restricted child read sources at denied cwd: %v", err)
	}
	if strings.Contains(child.prompt.Prompt, "ALLOWED_CHILD") || !strings.Contains(child.prompt.Prompt, "GLOBAL_USER_RULE") {
		t.Fatal("denied child cwd changed global context or retained project context")
	}
}

func TestPromptExplicitOverrideDoesNotReadAuthorityOrProjectFiles(t *testing.T) {
	node := storeBackedInputSession(t, t.TempDir())
	node.root.authority = node.authority
	node.promptOverride = "VERBATIM_ROOT_OVERRIDE"
	node.agent.WorkingDir = filepath.Join(t.TempDir(), "missing")
	if _, err := node.root.store.RevokeCapabilityFor(t.Context(), node.id, node.id, node.authority.Files.ID); err != nil {
		t.Fatal(err)
	}
	if err := node.refreshPrompt(t.Context()); err != nil {
		t.Fatalf("source lookup blocked explicit override: %v", err)
	}
	if node.prompt.Prompt != node.promptOverride || node.prompt.WorkingDirectory != node.agent.WorkingDir {
		t.Fatalf("explicit override was changed: %+v", node.prompt)
	}
}

func TestPromptRetainedChildInheritsRulesCatalogAndNextTurnEdits(t *testing.T) {
	requests, client := promptRuntimeProvider(t)
	database := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, database)
	workspace := canonicalPromptDirectory(t, t.TempDir())
	rootID, err := store.Create(session.SessionKindAgent, workspace, "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	childDir := filepath.Join(workspace, "child")
	writeDaemonPromptFile(t, filepath.Join(workspace, "AGENTS.md"), "INHERITED_PARENT_RULE")
	writeDaemonPromptFile(t, filepath.Join(childDir, "CLAUDE.md"), "CHILD_CLAUDE_RULE")
	writeDaemonPromptFile(t, filepath.Join(childDir, "AGENTS.md"), "CHILD_AGENTS_RULE")
	parentSkill := filepath.Join(workspace, ".agents", "skills", "parent-skill", "SKILL.md")
	writeDaemonPromptFile(t, parentSkill, "---\nname: parent-skill\ndescription: INHERITED_PARENT_CATALOG\n---\nINVOKED_PARENT_SKILL_BODY\n")
	childSkill := filepath.Join(childDir, ".agents", "skills", "child-skill", "SKILL.md")
	writeDaemonPromptFile(t, childSkill, "---\nname: child-skill\ndescription: CHILD_CATALOG\n---\n")
	standing := filepath.Join(os.Getenv("WHIP_HOME"), "me.md")
	writeDaemonPromptFile(t, standing, "CHILD_STANDING_BEFORE_EDIT")
	owner, _, runtime := openPromptRuntime(t, store, rootID, client)
	// A child's effective cwd may be narrower than the inherited workspace.
	// Set that environment before spawn; admission persists it for restoration.
	runtime.rootNode.SetWorkingDirectory(childDir)
	runtime.rootNode.ConfigureRun("ROOT_OVERRIDE_MUST_NOT_PROPAGATE", 0, false, "")
	spawned, err := runtime.rootNode.host.Call(t.Context(), "agents", "spawn", map[string]any{
		"name": "prompt-child", "prompt": "first child task", "report": "message",
	})
	if err != nil {
		t.Fatal(err)
	}
	childID := spawned.(map[string]any)["id"].(string)
	child := promptChild(t, runtime, childID)
	for _, stage := range []string{"spawned", "retained", "reopened"} {
		if stage == "reopened" {
			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}
			store = openStore(t, database)
			_, _, runtime = openPromptRuntime(t, store, rootID, client)
			child = promptChild(t, runtime, childID)
			if audit := child.ContextAudit(); !promptAuditContains(audit, "not applied yet") {
				t.Fatalf("restored child claimed sources before a turn: %+v", audit)
			}
		}
		standingMarker := "CHILD_STANDING_BEFORE_EDIT"
		if stage != "spawned" {
			standingMarker = "CHILD_STANDING_" + strings.ToUpper(stage)
			writeDaemonPromptFile(t, standing, standingMarker)
			if _, err := runtime.rootNode.host.Call(t.Context(), "agents", "submit", map[string]any{
				"id": childID, "text": "$parent-skill follow-up child task", "delivery": "queued",
			}); err != nil {
				t.Fatal(err)
			}
		}
		request := readDaemonPromptRequest(t, requests)
		waitAgentIdle(t, child)
		assertProviderPrompt(t, request, []string{"INHERITED_PARENT_RULE", "CHILD_CLAUDE_RULE", "CHILD_AGENTS_RULE", "INHERITED_PARENT_CATALOG", "CHILD_CATALOG", standingMarker, childID, "Working directory: " + childDir, "no completion notice when you succeed"}, []string{"ROOT_OVERRIDE_MUST_NOT_PROPAGATE"})
		assertAppliedPromptAudit(t, child, request, childDir, parentSkill, childSkill)
		if stage != "spawned" {
			if strings.Contains(request.Messages[0].Content, "CHILD_STANDING_BEFORE_EDIT") {
				t.Fatalf("%s child retained stale standing instructions", stage)
			}
			found := false
			for _, message := range request.Messages {
				if message.Role == "user" && strings.Contains(message.Content, "INVOKED_PARENT_SKILL_BODY") {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s child could not explicitly invoke inherited skill", stage)
			}
		}
	}
}

func TestPromptRunConfigurationAppliesOnlyAtTurnBoundary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIP_HOME", t.TempDir())
	standing := filepath.Join(os.Getenv("WHIP_HOME"), "me.md")
	writeDaemonPromptFile(t, standing, "OLD_STANDING_POLICY")
	requests := make(chan llm.Request, 8)
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	defer releaseOnce()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		requests <- request
		if calls.Add(1) == 1 {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"prompt-policy","type":"function","function":{"name":"rlm_exec","arguments":"{\"code\":\"1\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		streamText(w, "prompt result")
	}))
	t.Cleanup(server.Close)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	_, root, runtime := openPromptRuntime(t, store, rootID, llm.New(server.URL, "key"))
	receipt, err := root.Submit(t.Context(), "run two model rounds")
	if err != nil {
		t.Fatal(err)
	}
	first := readDaemonPromptRequest(t, requests)
	writeDaemonPromptFile(t, standing, "NEW_STANDING_POLICY")
	busy := clientCommand(t, root, "prompt-client", "busy-configure", "run.configure", map[string]any{"system": "REJECTED_BUSY_OVERRIDE"})
	if busy.Status != "failed" || !strings.Contains(busy.Error, "running") {
		t.Fatalf("run configuration during a turn must be rejected: %+v", busy)
	}
	releaseOnce()
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	second := readDaemonPromptRequest(t, requests)
	assertProviderPrompt(t, first, []string{"OLD_STANDING_POLICY"}, []string{"NEW_STANDING_POLICY", "REJECTED_BUSY_OVERRIDE"})
	assertProviderPrompt(t, second, []string{"OLD_STANDING_POLICY"}, []string{"NEW_STANDING_POLICY", "REJECTED_BUSY_OVERRIDE"})
	if first.Messages[0].Content != second.Messages[0].Content {
		t.Fatal("system prompt changed within a running turn")
	}
	const override = "IDLE_OVERRIDE_FOR_NEXT_TURN"
	idle := clientCommand(t, root, "prompt-client", "idle-configure", "run.configure", map[string]any{"system": override})
	if idle.Status != "succeeded" {
		t.Fatalf("idle run configuration = %+v", idle)
	}
	assertAppliedPromptAudit(t, runtime.rootNode, second, runtime.rootNode.agent.WorkingDir, standing)
	if runtime.rootNode.agent.MessagesSnapshot()[0].Content != second.Messages[0].Content {
		t.Fatal("pending override rewrote the previously applied model prompt")
	}
	third := submitPromptRoot(t, root, requests, "use explicit override")
	if third.Messages[0].Content != override {
		t.Fatalf("next turn did not apply exact override: %q", third.Messages[0].Content)
	}
	cleared := clientCommand(t, root, "prompt-client", "clear-configure", "run.configure", map[string]any{"system": ""})
	if cleared.Status != "succeeded" {
		t.Fatalf("clear run configuration = %+v", cleared)
	}
	fourth := submitPromptRoot(t, root, requests, "resume normal environment")
	assertProviderPrompt(t, fourth, []string{"NEW_STANDING_POLICY"}, []string{"OLD_STANDING_POLICY", override})
}

func promptRuntimeProvider(t *testing.T) (<-chan llm.Request, *llm.Client) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("WHIP_HOME", t.TempDir())
	requests := make(chan llm.Request, 16)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llm.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case requests <- request:
		case <-r.Context().Done():
			return
		}
		streamText(w, "prompt result")
	}))
	t.Cleanup(server.Close)
	client := llm.New(server.URL, "fixture-key")
	client.MaxRetries = 0
	return requests, client
}

func openPromptRuntime(t *testing.T, store *session.Store, rootID string, client *llm.Client) (*Daemon, *Session, *RecursiveRuntime) {
	t.Helper()
	var runtime *RecursiveRuntime
	owner, err := New(store, func(_ context.Context, meta session.Meta, history []llm.Message) (Components, error) {
		value := agent.NewRuntime(client, meta.Model, 128, "", tools.NewServices())
		value.ModelName, value.Provider, value.WorkingDir = meta.Model, meta.Provider, meta.CWD
		value.ContextLimit = 65536
		limits := rlm.DefaultLimits()
		var err error
		runtime, err = NewRecursiveRuntime(RecursiveRuntimeOptions{
			Engine: meta.ExecutionEngine, Agent: value, History: history, Limits: limits, Kernels: rlm.NewManager(limits.MaxWorkers), KernelCommand: recursiveKernelCommand,
		})
		if err != nil {
			return Components{}, err
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
	return owner, root, runtime
}

func writeDaemonPromptFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func submitPromptRoot(t *testing.T, root *Session, requests <-chan llm.Request, input string) llm.Request {
	t.Helper()
	receipt, err := root.Submit(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if completion := waitReceipt(t, receipt); completion.Err != nil {
		t.Fatal(completion.Err)
	}
	return readDaemonPromptRequest(t, requests)
}

func readDaemonPromptRequest(t *testing.T, requests <-chan llm.Request) llm.Request {
	t.Helper()
	select {
	case request := <-requests:
		return request
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not receive request")
		return llm.Request{}
	}
}

func assertProviderPrompt(t *testing.T, request llm.Request, wanted, unwanted []string) {
	t.Helper()
	if len(request.Messages) == 0 || request.Messages[0].Role != "system" {
		t.Fatalf("request has no system prompt: %+v", request.Messages)
	}
	prompt := request.Messages[0].Content
	for _, marker := range wanted {
		if !strings.Contains(prompt, marker) {
			t.Errorf("provider prompt missing %q", marker)
		}
	}
	for _, marker := range unwanted {
		if strings.Contains(prompt, marker) {
			t.Errorf("provider prompt retained inapplicable %q", marker)
		}
	}
}

func assertAppliedPromptAudit(t *testing.T, node *AgentSession, request llm.Request, cwd string, sourcePaths ...string) {
	t.Helper()
	cwd = canonicalPromptDirectory(t, cwd)
	audit := node.ContextAudit()
	if audit.WorkingDirectory != cwd {
		t.Errorf("audit cwd %q differs from applied cwd %q", audit.WorkingDirectory, cwd)
	}
	for _, path := range sourcePaths {
		if !promptAuditContains(audit, path) {
			t.Errorf("audit omitted applied source %q: %+v", path, audit)
		}
	}
	for _, row := range audit.Rows {
		if row.Label == "RLM system prompt" && row.Bytes == len(request.Messages[0].Content) {
			return
		}
	}
	t.Errorf("audit prompt bytes do not match actual request: %+v", audit)
}

func promptAuditContains(audit ContextAuditResult, text string) bool {
	for _, row := range audit.Rows {
		if strings.Contains(row.Note, text) {
			return true
		}
	}
	return false
}

func promptChild(t *testing.T, runtime *RecursiveRuntime, id string) *AgentSession {
	t.Helper()
	runtime.mu.RLock()
	child := runtime.agents[id]
	runtime.mu.RUnlock()
	if child == nil {
		t.Fatalf("child %q missing", id)
	}
	return child
}

func canonicalPromptDirectory(t *testing.T, directory string) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
