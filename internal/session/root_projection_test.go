package session

import (
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

// The root agent's model, provider, effort and cwd are session facts that
// session.model, session.effort and workspace.set write to the sessions row.
// Every agent read must report those saved values for the root while children
// keep the selection they were admitted with.
func TestRootAgentReadsDeriveSelectionFromSession(t *testing.T) {
	ctx := t.Context()
	store, err := Open(filepath.Join(t.TempDir(), "sessions.db"), capability.NewWorkspaces())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(ctx, rootID); err != nil {
		t.Fatal(err)
	}
	childCWD := t.TempDir()
	if _, err := store.AdmitAgent(ctx, AgentAdmission{
		RootID: rootID, ParentAgentID: rootID, ChildAgentID: "child",
		Model: "child-model", Provider: "child-provider", Effort: "low", CWD: childCWD,
	}); err != nil {
		t.Fatal(err)
	}
	rootCWD := t.TempDir()
	if err := store.SetModelSelection(rootID, "new-model", "new-provider", "high"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetWorkingDirectory(rootID, rootCWD); err != nil {
		t.Fatal(err)
	}

	want := map[string]RuntimeAgent{
		rootID:  {Model: "new-model", Provider: "new-provider", Effort: "high", CWD: rootCWD},
		"child": {Model: "child-model", Provider: "child-provider", Effort: "low", CWD: childCWD},
	}
	check := func(reader string, agent RuntimeAgent) {
		t.Helper()
		expected, ok := want[agent.ID]
		if !ok {
			t.Fatalf("%s returned unexpected agent %q", reader, agent.ID)
		}
		if agent.Model != expected.Model || agent.Provider != expected.Provider || agent.Effort != expected.Effort || agent.CWD != expected.CWD {
			t.Fatalf("%s %s = %s/%s/%s/%s, want %s/%s/%s/%s", reader, agent.ID,
				agent.Model, agent.Provider, agent.Effort, agent.CWD,
				expected.Model, expected.Provider, expected.Effort, expected.CWD)
		}
	}
	for _, id := range []string{rootID, "child"} {
		agent, err := store.LoadAgent(ctx, rootID, id)
		if err != nil {
			t.Fatal(err)
		}
		check("LoadAgent", agent)
	}
	snapshot, err := store.SnapshotRoot(ctx, rootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Agents) != 2 {
		t.Fatalf("snapshot agents = %d, want 2", len(snapshot.Agents))
	}
	for _, agent := range snapshot.Agents {
		check("SnapshotRoot", agent)
	}
	views, err := store.RootAgentViews(ctx, rootID)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 {
		t.Fatalf("root agent views = %d, want 2", len(views))
	}
	for _, agent := range views {
		check("RootAgentViews", agent)
	}
}
