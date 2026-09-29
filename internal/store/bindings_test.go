package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func bindingDefinition(t *testing.T, s *Store, name string, tools, hooks bool) session.DefinitionRevision {
	t.Helper()
	doc := session.DefinitionDocument{ID: name, Name: name, Defaults: session.ConfigPatch{Modules: []string{"files", "agents"}}}
	if tools {
		doc.Defaults.Tools = map[string]session.ToolDeclaration{"lookup": {InputSchema: json.RawMessage(`{"type":"object"}`)}}
	}
	if hooks {
		doc.Defaults.Hooks = map[string]session.HookDeclaration{"before_tool": {Operations: []string{"files.read"}}}
	}
	def, err := s.RegisterDefinition(t.Context(), doc)
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func TestBindingSourcesMixedChildForkAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	def := bindingDefinition(t, s, "root", true, true)
	_, root, err := s.CreateTree(t.Context(), CreateTree{Engine: session.QuickJS, Definition: def.Ref, WorkingDirectory: t.TempDir(), Defaults: session.Configuration{Model: session.ModelSelection{Provider: "test", Name: "model"}}})
	if err != nil {
		t.Fatal(err)
	}
	childDef := bindingDefinition(t, s, "child-tools", true, false)
	child, err := s.SpawnSession(t.Context(), SpawnSession{ParentID: root.ID, Definition: &childDef.Ref, Overrides: session.ConfigPatch{Modules: []string{"files"}}})
	if err != nil {
		t.Fatal(err)
	}
	if child.Config.ToolsDefinition == nil || *child.Config.ToolsDefinition != childDef.Ref || child.Config.HooksDefinition == nil || *child.Config.HooksDefinition != def.Ref {
		t.Fatal("child lost mixed sources", child.Config)
	}
	history := compactionHistoryTest(t, s, child.ID, "binding-source")
	fork := forkTest(t, s, forkRequestTest(t, s, child.ID, "binding-fork", history[len(history)-1].Sequence))
	if !session.SameContract(fork.Root.Config, child.Config) {
		t.Fatal("fork changed captured bindings")
	}
	if err := s.DeleteSubtree(t.Context(), root.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := openTest(t, path)
	forked, err := reopened.Session(t.Context(), fork.Root.ID)
	if err != nil || !session.SameContract(forked.Config, child.Config) {
		t.Fatal("ancestor deletion/restart lost origins", err)
	}
	narrowed, err := reopened.UpdateConfiguration(t.Context(), forked.ID, 1, session.ConfigPatch{Modules: []string{}, Tools: map[string]session.ToolDeclaration{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.UpdateConfiguration(t.Context(), forked.ID, narrowed.ConfigRevision, session.ConfigPatch{Modules: child.Config.Modules, Tools: child.Config.Tools}); err != nil {
		t.Fatal("initial subset could not be reenabled", err)
	}
	if _, err := reopened.UpdateConfiguration(t.Context(), forked.ID, narrowed.ConfigRevision+1, session.ConfigPatch{Modules: []string{"agents"}}); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("fork expanded its own initial ceiling", err)
	}
}

func TestBindingUnownedOverridesAndChildNarrowing(t *testing.T) {
	s := fresh(t)
	def := bindingDefinition(t, s, "owner", true, true)
	_, _, builtin, _ := session.CanonicalDefinition(session.Builtins()[0])
	defaults := session.Configuration{Model: session.ModelSelection{Provider: "test", Name: "model"}, Tools: def.Document.Defaults.Tools, ToolsDefinition: &def.Ref}
	_, unowned, err := s.CreateTree(t.Context(), CreateTree{Engine: session.Starlark, Definition: builtin, WorkingDirectory: t.TempDir(), Defaults: defaults, Overrides: session.ConfigPatch{Modules: []string{"files"}}})
	if err != nil || unowned.Config.ToolsDefinition != nil {
		t.Fatal("host authority was trusted", err)
	}
	_, root, err := s.CreateTree(t.Context(), CreateTree{Engine: session.Starlark, Definition: def.Ref, WorkingDirectory: t.TempDir(), Defaults: defaults})
	if err != nil {
		t.Fatal(err)
	}
	for _, patch := range []session.ConfigPatch{
		{Modules: []string{"shell"}},
		{Tools: map[string]session.ToolDeclaration{"new": {InputSchema: json.RawMessage(`{}`)}}},
		{Hooks: map[string]session.HookDeclaration{}},
	} {
		if _, err := s.SpawnSession(t.Context(), SpawnSession{ParentID: root.ID, Overrides: patch}); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("child expanded/replaced binding", err)
		}
		if _, err := s.UpdateConfiguration(t.Context(), root.ID, 1, patch); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("configuration expanded/replaced binding", err)
		}
	}
	changed := def.Document.Defaults.Tools["lookup"]
	changed.Description = "different contract"
	if _, changedRoot, err := s.CreateTree(t.Context(), CreateTree{Engine: session.Starlark, Definition: def.Ref, WorkingDirectory: t.TempDir(), Defaults: defaults, Overrides: session.ConfigPatch{Tools: map[string]session.ToolDeclaration{"lookup": changed}}}); err != nil || changedRoot.Config.ToolsDefinition != nil {
		t.Fatal("override claimed registered source for changed contract", err)
	}
	if count(t, s, "sessions") != 3 {
		t.Fatal("failed admission left partial sessions")
	}
}

func TestBindingCapturedCellAndSpawnIgnoreLaterConfiguration(t *testing.T) {
	s := fresh(t)
	owner, cell := operationCell(t, s)
	narrowed, err := s.UpdateConfiguration(t.Context(), owner.ID, 1, session.ConfigPatch{Modules: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	captured, err := s.CellSession(t.Context(), owner.ID, cell.ID)
	if err != nil || captured.ConfigRevision != 1 || !slices.Contains(captured.Config.Modules, "files") {
		t.Fatal("cell used future configuration", err, captured)
	}
	if _, err := s.CellSession(t.Context(), "wrong-session", cell.ID); !errors.Is(err, ErrConflict) {
		t.Fatal("cell owner bypass", err)
	}
	if len(narrowed.Config.Modules) != 0 {
		t.Fatal("narrowing lost")
	}
	if _, err := s.CreateGrant(t.Context(), session.Grant{ID: "spawn", SessionID: owner.ID, Capability: "agents.spawn", Resource: string(owner.TreeID)}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(childRequest(owner.ID))
	spec := session.OperationSpec{ID: "binding-spawn", CellID: cell.ID, RequestID: "binding-spawn", Capability: "agents.spawn", Resource: string(owner.TreeID), Arguments: raw}
	admitOperation(t, s, spec)
	admitted, err := s.SpawnChildOperation(t.Context(), spec.ID)
	if err != nil || !slices.Equal(admitted.Session.Config.Modules, owner.Config.Modules) {
		t.Fatal("spawn used mutable parent", err)
	}
	direct, err := s.SpawnSession(t.Context(), SpawnSession{ParentID: owner.ID})
	if err != nil || direct.Config.Modules == nil || len(direct.Config.Modules) != 0 {
		t.Fatal("direct spawn failed to use current parent", err)
	}
	if _, err := s.CancelTurn(t.Context(), cell.TurnID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CellSession(t.Context(), owner.ID, cell.ID); !errors.Is(err, ErrStopped) {
		t.Fatal("inactive cell captured config", err)
	}
}
