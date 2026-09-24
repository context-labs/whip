package rlm_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/rlm"
)

// TestDefinitionPromptGolden pins every byte each registered definition's
// system prompt is assembled from: the standalone prompt for both engines with
// and without a context handle, and the fully composed environment prompt for a
// root and a child. The runtime-owned identity blocks and engine guide digests
// are shared by every definition. Changing a definition's text or module list
// is the only reason a golden may change.
// Regenerate deliberately with: go test ./internal/rlm -run TestDefinitionPromptGolden -update
func TestDefinitionPromptGolden(t *testing.T) {
	handle := &rlm.ContextHandle{ReferenceID: "history-handle", Size: 123456, Source: "history"}
	identities := []rlm.Identity{
		{AgentID: "root-id", Name: "root"},
		{AgentID: "child-id", Name: "worker", ParentID: "root-id", ParentName: "root", Depth: 1},
		{AgentID: "child-id", Name: "worker", ParentID: "root-id", ParentName: "root", Depth: 1, Report: "inline"},
		{AgentID: "grandchild-id", Name: "scout", ParentID: "child-id", ParentName: "worker", Depth: 2, Report: "message"},
	}
	engines := []string{rlm.EngineStarlark, rlm.EngineQuickJS}
	for _, id := range agentdef.IDs() {
		definition, ok := agentdef.Lookup(id)
		if !ok {
			t.Fatalf("definition %q missing", id)
		}
		for _, engine := range engines {
			t.Run(id+"/"+engine+"/guide", func(t *testing.T) {
				golden.RequireEqual(t, standalonePrompt(t, definition, engine, nil))
			})
			t.Run(id+"/"+engine+"/guide-with-handle", func(t *testing.T) {
				golden.RequireEqual(t, standalonePrompt(t, definition, engine, handle))
			})
			t.Run(id+"/"+engine+"/composed-root", func(t *testing.T) {
				golden.RequireEqual(t, composedPrompt(t, definition, engine, identities[0]))
			})
			t.Run(id+"/"+engine+"/composed-child", func(t *testing.T) {
				golden.RequireEqual(t, composedPrompt(t, definition, engine, identities[2]))
			})
		}
	}
	for _, engine := range engines {
		t.Run("identity/"+engine, func(t *testing.T) {
			var b strings.Builder
			for _, identity := range identities {
				b.WriteString(rlm.IdentityBlockForEngine(engine, identity))
				b.WriteString("\n---\n")
			}
			golden.RequireEqual(t, b.String())
		})
	}
	t.Run("engine-guide-digests", func(t *testing.T) {
		var b strings.Builder
		for _, descriptor := range rlm.Engines() {
			fmt.Fprintf(&b, "%s %s\n", descriptor.ID, descriptor.GuideSHA256)
		}
		golden.RequireEqual(t, b.String())
	})
}

func standalonePrompt(t *testing.T, definition agentdef.Definition, engine string, handle *rlm.ContextHandle) string {
	t.Helper()
	prompt, err := definition.SystemPrompt(engine, "/workspace", handle)
	if err != nil {
		t.Fatal(err)
	}
	return prompt
}

// composedPrompt runs the real composer against an empty home and project so
// only the constant parts of the environment prompt remain. The canonical
// temporary directory is replaced by <cwd> to keep the output stable.
func composedPrompt(t *testing.T, definition agentdef.Definition, engine string, identity rlm.Identity) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WHIP_HOME", filepath.Join(home, "whip"))
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := rlm.ComposePrompt(definition.PromptOptions(rlm.PromptOptions{
		Engine: engine, WorkingDirectory: cwd, Identity: identity,
		Now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC), Platform: "golden-platform", Username: "golden-user",
	}))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, source := range snapshot.Sources {
		fmt.Fprintf(&b, "source kind=%s scope=%s\n", source.Kind, strings.ReplaceAll(source.Scope, cwd, "<cwd>"))
	}
	b.WriteString("\n")
	b.WriteString(strings.ReplaceAll(snapshot.Prompt, cwd, "<cwd>"))
	return b.String()
}
