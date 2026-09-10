package daemon

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

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
