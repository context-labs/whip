package session

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/context-labs/whip/internal/capability"
)

// The tools grant holds a definition's custom tool operations. It is issued at
// bootstrap, delegated by name to children, never widened, and added with no
// operations to roots that predate it.
func TestToolsGrantIssuesDelegatesAndNeverWidens(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "tools.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rootID, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	authority, err := store.EnsureRootAuthority(t.Context(), rootID, RootGrants{Files: []string{"read"}, Tools: []string{"tools.lookup", "tools.other"}})
	if err != nil {
		t.Fatal(err)
	}
	if authority.Tools.ID != "tools:"+rootID || authority.Tools.Generation != 1 {
		t.Fatalf("tools reference = %+v", authority.Tools)
	}
	if err := store.AuthorizeCapability(t.Context(), rootID, rootID, authority.Tools, "tools.lookup", ""); err != nil {
		t.Fatalf("declared tool denied: %v", err)
	}
	if err := store.AuthorizeCapability(t.Context(), rootID, rootID, authority.Tools, "tools.missing", ""); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("undeclared tool authorized: %v", err)
	}
	loaded, names, err := store.LoadAgentAuthority(t.Context(), rootID, rootID)
	if err != nil || loaded.Tools != authority.Tools || !slices.Equal(names, []string{"read"}) {
		t.Fatalf("loaded authority = %+v names = %v err = %v", loaded, names, err)
	}
	if tools, err := store.LoadAgentTools(t.Context(), rootID, rootID); err != nil || !slices.Equal(tools, []string{"lookup", "other"}) {
		t.Fatalf("root tools = %v %v", tools, err)
	}
	admitTestChild(t, store, rootID, rootID, "child")
	if _, err := store.DelegateCapability(t.Context(), rootID, rootID, CapabilityDelegation{ID: "tools:child", Issuer: authority.Tools, AgentID: "child", Operations: []string{"tools.lookup"}}); err != nil {
		t.Fatalf("tool delegation failed: %v", err)
	}
	for name, delegation := range map[string]CapabilityDelegation{
		"undeclared":  {ID: "tools:child-2", Issuer: authority.Tools, AgentID: "child", Operations: []string{"tools.missing"}},
		"scoped":      {ID: "tools:child-3", Issuer: authority.Tools, AgentID: "child", Operations: []string{"tools.other"}, Scopes: []string{"."}},
		"inherit":     {ID: "tools:child-4", Issuer: authority.Tools, AgentID: "child", Operations: []string{"tools.other"}, InheritScope: true},
		"wrong-grant": {ID: "tools:child-5", Issuer: authority.Files, AgentID: "child", Operations: []string{"tools.other"}},
	} {
		if _, err := store.DelegateCapability(t.Context(), rootID, rootID, delegation); !errors.Is(err, capability.ErrDenied) {
			t.Fatalf("%s delegation = %v", name, err)
		}
	}
	child, _, err := store.LoadAgentAuthority(t.Context(), rootID, "child")
	if err != nil || child.Tools.ID != "tools:child" {
		t.Fatalf("child authority = %+v %v", child, err)
	}
	if tools, err := store.LoadAgentTools(t.Context(), rootID, "child"); err != nil || !slices.Equal(tools, []string{"lookup"}) {
		t.Fatalf("child tools = %v %v", tools, err)
	}
	if err := store.AuthorizeCapability(t.Context(), rootID, "child", child.Tools, "tools.other", ""); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("child used a tool its parent narrowed away: %v", err)
	}
	// Reopening with a different tool list keeps the original grant.
	reopened, err := store.EnsureRootAuthority(t.Context(), rootID, RootGrants{Tools: []string{"tools.lookup", "tools.other", "tools.wider"}})
	if err != nil || reopened.Tools != authority.Tools {
		t.Fatalf("reopened tools reference = %+v %v", reopened.Tools, err)
	}
	if err := store.AuthorizeCapability(t.Context(), rootID, rootID, reopened.Tools, "tools.wider", ""); !errors.Is(err, capability.ErrDenied) {
		t.Fatalf("reopen widened the tools grant: %v", err)
	}
	// A root from before tools grants existed gets an empty grant, not tools.
	legacy, err := store.Create(SessionKindAgent, t.TempDir(), "model", "provider")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureAuthority(t.Context(), legacy); err != nil {
		t.Fatal(err)
	}
	exec(t, store, `DELETE FROM capabilities WHERE id='tools:`+legacy+`'`)
	restored, err := store.EnsureRootAuthority(t.Context(), legacy, RootGrants{Tools: []string{"tools.lookup"}})
	if err != nil || restored.Tools.Generation != 1 {
		t.Fatalf("legacy root tools reference = %+v %v", restored.Tools, err)
	}
	if err := store.AuthorizeCapability(t.Context(), legacy, legacy, restored.Tools, "tools.lookup", ""); err != nil {
		t.Fatalf("legacy root did not receive the definition's tools: %v", err)
	}
}
