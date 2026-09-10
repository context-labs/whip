package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

func juniorDeveloperDocument(t *testing.T) json.RawMessage {
	t.Helper()
	document, err := os.ReadFile(filepath.Join("..", "agentdef", "testdata", "junior-developer.json"))
	if err != nil {
		t.Fatal(err)
	}
	return document
}

// A model change rebuilds the runtime; the session's run configuration is a
// per-session override and must reach the replacement.
func TestRunConfigurationSurvivesModelReplacement(t *testing.T) {
	requests, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	_, root, _ := openPromptRuntime(t, store, rootID, client)
	const override = "OVERRIDE_SURVIVES_MODEL_CHANGE"
	if result := clientCommand(t, root, "prompt-client", "configure", "run.configure", map[string]any{"system": override, "max_turns": 3}); result.Status != "succeeded" {
		t.Fatalf("run configuration = %+v", result)
	}
	before := submitPromptRoot(t, root, requests, "first turn")
	if before.Messages[0].Content != override {
		t.Fatalf("override not applied before replacement: %q", before.Messages[0].Content)
	}
	if result := clientCommand(t, root, "prompt-client", "switch-model", "session.model", map[string]any{"model": "replacement-model"}); result.Status != "succeeded" {
		t.Fatalf("model change = %+v", result)
	}
	after := submitPromptRoot(t, root, requests, "second turn")
	if after.Model != "replacement-model" {
		t.Fatalf("replacement runtime did not take the new model: %q", after.Model)
	}
	if after.Messages[0].Content != override {
		t.Fatalf("run override lost across model replacement: %q", after.Messages[0].Content)
	}
	if runner, ok := root.runner.(*AgentSession); !ok || runner.agent.MaxTurns != 3 {
		t.Fatalf("turn cap lost across model replacement: %#v", root.runner)
	}
}

// A spawned child's effective definition is the parent's, narrowed by the spawn
// arguments; it cannot widen.
func TestChildDefinitionNarrowsParentDefinition(t *testing.T) {
	_, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	_, _, runtime := openPromptRuntime(t, store, rootID, client)
	parent := runtime.rootNode
	if !slices.Equal(parent.capabilities, agentdef.Coding().Capabilities) || parent.definition.ID != "coding" {
		t.Fatalf("root did not take the coding definition: %+v", parent.definition)
	}
	child := spawnMCPChild(t, parent, map[string]any{"name": "narrow", "capabilities": []any{"read", "mcp"}})
	if !slices.Equal(child.definition.Capabilities, []string{"mcp", "read"}) || !slices.Equal(child.capabilities, child.definition.Capabilities) {
		t.Fatalf("child capabilities = %v / %v", child.definition.Capabilities, child.capabilities)
	}
	if !slices.Equal(child.definition.Modules, parent.definition.Modules) || !reflect.DeepEqual(child.definition.Instructions, parent.definition.Instructions) {
		t.Fatalf("child did not inherit modules and instructions: %+v", child.definition)
	}
	_, err := child.host.Call(t.Context(), "agents", "spawn", map[string]any{"prompt": "must not run", "name": "wide", "capabilities": []any{"shell"}})
	if err == nil || err.Error() != `capability "shell" is not available to the parent` {
		t.Fatalf("widening spawn error = %v", err)
	}
}

// Surface flags gate root-only behaviors around turns.
func TestDefinitionSurfaceDisablesAutomaticTitle(t *testing.T) {
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	runner := &titleRunner{fakeRunner: &fakeRunner{}, title: "Never Applied", finished: make(chan struct{})}
	definition := agentdef.Coding()
	definition.Surface.AutoTitle = false
	value, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: runner, Definition: definition}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.Close() })
	root, err := value.Open(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if result := clientCommand(t, root, "tui", "autotitle", "session.autotitle", protocol.EmptyParams{}); result.Status != "succeeded" {
		t.Fatalf("enable automatic title=%+v", result)
	}
	receipt, err := root.Submit(t.Context(), "Investigate flaky workers")
	if err != nil {
		t.Fatal(err)
	}
	waitReceipt(t, receipt)
	select {
	case <-runner.finished:
		t.Fatal("title generated although the definition disables it")
	case <-time.After(200 * time.Millisecond):
	}
	meta, _, err := store.Load(rootID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(meta.Title, runner.title) {
		t.Fatalf("title applied: %q", meta.Title)
	}
}

// A definition's model defaults fill an omitted route before host defaults; an
// explicit request still wins.
func TestSessionDefaultsPreferDefinitionModel(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	cfg := config.Default()
	cfg.DefaultModel, cfg.DefaultProvider = "host-alias", "host"
	cfg.Models["host-alias"] = config.Model{Providers: []string{"host"}}
	cfg.Models["agent-alias"] = config.Model{Providers: []string{"agent"}}
	cfg.Providers["host"] = config.Provider{BaseURL: "http://localhost:1"}
	cfg.Providers["agent"] = config.Provider{BaseURL: "http://localhost:2"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	definition := agentdef.Coding()
	definition.Model = agentdef.ModelDefaults{Model: "agent-alias", Provider: "agent"}
	got, err := sessionDefaults(CreateSession{Kind: session.SessionKindAgent}, definition)
	if err != nil || got.Model != "agent-alias" || got.Provider != "agent" || got.ExecutionEngine != "starlark" {
		t.Fatalf("definition defaults not applied: %+v %v", got, err)
	}
	got, err = sessionDefaults(CreateSession{Kind: session.SessionKindAgent, Model: "host-alias"}, definition)
	if err != nil || got.Model != "host-alias" || got.Provider != "host" {
		t.Fatalf("explicit model lost to definition defaults: %+v %v", got, err)
	}
	got, err = sessionDefaults(CreateSession{Kind: session.SessionKindAgent}, agentdef.Coding())
	if err != nil || got.Model != "host-alias" || got.Provider != "host" {
		t.Fatalf("coding must fall back to host defaults: %+v %v", got, err)
	}
	if _, ok, err := DefinitionFor(t.Context(), nil, session.Meta{Kind: session.SessionKindToolHost}); ok || err != nil {
		t.Fatal("tool hosts have no agent definition")
	}
	if _, _, err := DefinitionFor(t.Context(), nil, session.Meta{ID: "root", Kind: session.SessionKindAgent, Definition: "architect"}); err == nil || !strings.Contains(err.Error(), "junior-developer") {
		t.Fatalf("unknown definition error = %v", err)
	}
}

// createDefinitionRoot creates an agent session through the daemon's command
// path so the store records its definition and, for a registered definition,
// the pinned revision.
func createDefinitionRoot(t *testing.T, store *session.Store, definition string) string {
	t.Helper()
	create, err := resolveSessionDefaults(t.Context(), store, CreateSession{Kind: session.SessionKindAgent, Model: "model", Provider: "provider", Definition: definition})
	if err != nil {
		t.Fatal(err)
	}
	id := session.NewAgentID()
	if _, err := store.AdmitCommand(t.Context(), session.CommandAdmission{ClientID: "definitions", CommandID: id, Scope: session.CommandScopeDaemon, RequestDigest: id}); err != nil {
		t.Fatal(err)
	}
	record, err := store.CreateSessionForCommandWithDefinition(t.Context(), "definitions", id, session.SessionKindAgent, t.TempDir(), "model", "provider", "", "", create.Definition, create.DefinitionRevision)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		RootID string `json:"root_id"`
	}
	if err := json.Unmarshal(record.Outcome.Inline, &result); err != nil {
		t.Fatal(err)
	}
	return result.RootID
}

// A JuniorDeveloper root receives only its definition's capabilities and
// modules, at the host boundary and in the ledger, and keeps them across a
// daemon restart.
func TestJuniorDeveloperSessionIsConstrained(t *testing.T) {
	requests, client := promptRuntimeProvider(t)
	path := filepath.Join(t.TempDir(), "sessions.db")
	store := openStore(t, path)
	rootID := createDefinitionRoot(t, store, "junior-developer")
	owner, root, runtime := openPromptRuntime(t, store, rootID, client)
	node := runtime.rootNode
	if node.definition.ID != "junior-developer" || !slices.Equal(node.capabilities, []string{"read", "write", "shell"}) {
		t.Fatalf("root definition = %+v capabilities = %v", node.definition.ID, node.capabilities)
	}
	if snapshot, err := root.Snapshot(t.Context()); err != nil || snapshot.Meta.Definition != "junior-developer" {
		t.Fatalf("snapshot definition=%q error=%v", snapshot.Meta.Definition, err)
	}
	if _, err := node.host.Call(t.Context(), "mcp", "list_servers", nil); err == nil || err.Error() != `module "mcp" is not available to this agent` {
		t.Fatalf("unselected module reached the host: %v", err)
	}
	if _, err := node.agent.Services.Invoke(t.Context(), "browser_exec", json.RawMessage(`{"code":"noop"}`)); err == nil || !(errors.Is(err, capability.ErrDenied) || strings.Contains(err.Error(), "denied")) {
		t.Fatalf("browser operation not denied by the ledger: %v", err)
	}
	prompt := submitPromptRoot(t, root, requests, "hello")
	system := prompt.Messages[0].Content
	for _, absent := range []string{"browser.run", "Messaging and delegation", "mcp.list_servers", "agents.spawn", "models.call"} {
		if strings.Contains(system, absent) {
			t.Fatalf("junior prompt advertises %q", absent)
		}
	}
	for _, present := range []string{"junior developer", "files.read", "shell.run", "user.ask"} {
		if !strings.Contains(system, present) {
			t.Fatalf("junior prompt lacks %q", present)
		}
	}
	if result := clientCommand(t, root, "junior", "goal", "goal.run", map[string]any{"text": "finish the task"}); result.Status != "failed" || !strings.Contains(result.Error, "does not run goals") {
		t.Fatalf("goal accepted by a definition without the goal loop: %+v", result)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openStore(t, path)
	_, _, restored := openPromptRuntime(t, reopened, rootID, client)
	if restored.rootNode.definition.ID != "junior-developer" || !slices.Equal(restored.rootNode.capabilities, []string{"read", "write", "shell"}) {
		t.Fatalf("restart lost the definition: %+v", restored.rootNode.definition)
	}
}

func TestCodingSessionKeepsGoalsAndUnknownDefinitionsAreRejected(t *testing.T) {
	requests, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	_, root, runtime := openPromptRuntime(t, store, rootID, client)
	if runtime.rootNode.definition.ID != "coding" || !slices.Equal(runtime.rootNode.capabilities, agentdef.Coding().Capabilities) {
		t.Fatalf("legacy root did not resolve to coding: %+v", runtime.rootNode.definition)
	}
	if result := clientCommand(t, root, "coding", "goal", "goal.set", map[string]any{"text": "keep going"}); result.Status != "succeeded" {
		t.Fatalf("coding goal rejected: %+v", result)
	}
	_ = requests
	if _, err := resolveSessionDefaults(t.Context(), store, CreateSession{Kind: session.SessionKindAgent, Definition: "architect"}); err == nil || !strings.Contains(err.Error(), "coding, junior-developer") {
		t.Fatalf("unknown definition accepted at creation: %v", err)
	}
	if _, err := resolveSessionDefaults(t.Context(), store, CreateSession{Kind: session.SessionKindToolHost, CWD: "/", Definition: "coding"}); err == nil {
		t.Fatal("tool host accepted an agent definition")
	}
}

// A registered definition is resolved by id at creation, pinned by revision,
// and composes its own persona; describing and listing expose built-ins and
// registrations together.
func TestRegisteredDefinitionRunsAndResolves(t *testing.T) {
	requests, client := promptRuntimeProvider(t)
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	document := juniorDeveloperDocument(t)
	registered, err := registerDefinition(t.Context(), store, document, "test-client")
	if err != nil || !registered.Created || registered.ID != "junior-developer-ts" || len(registered.Revision) != 64 {
		t.Fatalf("registration = %+v %v", registered, err)
	}
	again, err := registerDefinition(t.Context(), store, document, "other-client")
	if err != nil || again.Created || again.Revision != registered.Revision {
		t.Fatalf("repeat registration = %+v %v", again, err)
	}
	if _, err := registerDefinition(t.Context(), store, []byte(`{"id":"coding","modules":["context"]}`), "test-client"); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("built-in id accepted: %v", err)
	}
	create, err := resolveSessionDefaults(t.Context(), store, CreateSession{Kind: session.SessionKindAgent, Model: "model", Provider: "provider", Definition: "junior-developer-ts"})
	if err != nil || create.DefinitionRevision != registered.Revision {
		t.Fatalf("creation did not pin the revision: %+v %v", create, err)
	}
	if _, err := resolveSessionDefaults(t.Context(), store, CreateSession{Kind: session.SessionKindAgent, Model: "model", Provider: "provider", Definition: "missing"}); err == nil || !strings.Contains(err.Error(), "junior-developer-ts") {
		t.Fatalf("unknown id did not list registered ids: %v", err)
	}
	id := session.NewAgentID()
	if _, err := store.AdmitCommand(t.Context(), session.CommandAdmission{ClientID: "definitions", CommandID: id, Scope: session.CommandScopeDaemon, RequestDigest: id}); err != nil {
		t.Fatal(err)
	}
	record, err := store.CreateSessionForCommandWithDefinition(t.Context(), "definitions", id, session.SessionKindAgent, t.TempDir(), "model", "provider", "", "", create.Definition, create.DefinitionRevision)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		RootID string `json:"root_id"`
	}
	if err := json.Unmarshal(record.Outcome.Inline, &result); err != nil {
		t.Fatal(err)
	}
	_, root, runtime := openPromptRuntime(t, store, result.RootID, client)
	if runtime.rootNode.definition.ID != "junior-developer-ts" || !slices.Equal(runtime.rootNode.capabilities, []string{"read", "write", "shell"}) {
		t.Fatalf("registered definition not applied: %+v", runtime.rootNode.definition)
	}
	prompt := submitPromptRoot(t, root, requests, "hello")
	if !strings.HasPrefix(prompt.Messages[0].Content, "You are a junior developer working under review.") {
		t.Fatalf("registered persona missing: %q", prompt.Messages[0].Content[:80])
	}
	if snapshot, err := root.Snapshot(t.Context()); err != nil || snapshot.Meta.DefinitionRevision != registered.Revision {
		t.Fatalf("snapshot revision = %q error=%v", snapshot.Meta.DefinitionRevision, err)
	}
	if _, _, err := DefinitionFor(t.Context(), store, session.Meta{ID: "x", Kind: session.SessionKindAgent, Definition: "junior-developer-ts", DefinitionRevision: strings.Repeat("0", 64)}); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("missing revision accepted: %v", err)
	}
	described, err := describeDefinition(t.Context(), store, protocol.DefinitionParams{ID: "junior-developer-ts"})
	if err != nil || described.Revision != registered.Revision || described.RegisteredBy != "test-client" || described.BuiltIn || described.Definition.Instructions.Persona == "" {
		t.Fatalf("describe = %+v %v", described, err)
	}
	builtIn, err := describeDefinition(t.Context(), store, protocol.DefinitionParams{ID: "coding"})
	if err != nil || !builtIn.BuiltIn || builtIn.Revision != "" || builtIn.Definition.ID != "coding" {
		t.Fatalf("describe built-in = %+v %v", builtIn, err)
	}
	list, err := listDefinitions(t.Context(), store)
	if err != nil || len(list.Items) != 3 || !list.Items[0].BuiltIn || list.Items[2].ID != "junior-developer-ts" || list.Items[2].Revision != registered.Revision {
		t.Fatalf("list = %+v %v", list, err)
	}
}
