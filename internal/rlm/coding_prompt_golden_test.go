package rlm_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/exp/golden"

	"github.com/context-labs/whip/internal/rlm"
)

// TestCodingPromptGolden pins every byte the coding agent's system prompt is
// assembled from: the runtime guide for both engines with and without a
// context handle, the identity blocks for a root and children in each report
// mode, the fully composed environment prompt for a root and a child, and the
// engine guide digests. The agent-definition extraction must leave every one
// of these files unchanged.
// Regenerate deliberately with: go test ./internal/rlm -run TestCodingPromptGolden -update
func TestCodingPromptGolden(t *testing.T) {
	handle := &rlm.ContextHandle{ReferenceID: "history-handle", Size: 123456, Source: "history"}
	identities := []rlm.Identity{
		{AgentID: "root-id", Name: "root"},
		{AgentID: "child-id", Name: "worker", ParentID: "root-id", ParentName: "root", Depth: 1},
		{AgentID: "child-id", Name: "worker", ParentID: "root-id", ParentName: "root", Depth: 1, Report: "inline"},
		{AgentID: "grandchild-id", Name: "scout", ParentID: "child-id", ParentName: "worker", Depth: 2, Report: "message"},
	}
	for _, engine := range []string{rlm.EngineStarlark, rlm.EngineQuickJS} {
		t.Run(engine+"/guide", func(t *testing.T) {
			golden.RequireEqual(t, rlm.BuildPromptForEngine(engine, "/workspace", nil))
		})
		t.Run(engine+"/guide-with-handle", func(t *testing.T) {
			golden.RequireEqual(t, rlm.BuildPromptForEngine(engine, "/workspace", handle))
		})
		t.Run(engine+"/identity", func(t *testing.T) {
			var b strings.Builder
			for _, identity := range identities {
				b.WriteString(rlm.IdentityBlockForEngine(engine, identity))
				b.WriteString("\n---\n")
			}
			golden.RequireEqual(t, b.String())
		})
		t.Run(engine+"/composed-root", func(t *testing.T) {
			golden.RequireEqual(t, composedPrompt(t, engine, identities[0]))
		})
		t.Run(engine+"/composed-child", func(t *testing.T) {
			golden.RequireEqual(t, composedPrompt(t, engine, identities[2]))
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

// composedPrompt runs the real composer against an empty home and project so
// only the constant parts of the environment prompt remain. The canonical
// temporary directory is replaced by <cwd> to keep the output stable.
func composedPrompt(t *testing.T, engine string, identity rlm.Identity) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WHIP_HOME", filepath.Join(home, "whip"))
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := rlm.ComposePrompt(rlm.PromptOptions{
		Engine: engine, WorkingDirectory: cwd, Identity: identity,
		Now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC), Platform: "golden-platform", Username: "golden-user",
	})
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
