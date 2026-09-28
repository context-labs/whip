package session

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestWorkspaceValuesKeepHostIdentityPrivateAndValidateExactScope(t *testing.T) {
	binding := WorkspaceBinding{Worktree: "/repo", GitDirectory: "/repo/.git", CommonDirectory: "/repo/.git", Scope: ".", WorktreeIdentity: "1:1", GitIdentity: "1:2", CommonIdentity: "1:2", ScopeIdentity: "1:1"}
	if err := binding.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*WorkspaceBinding)
	}{
		{"outside scope", func(b *WorkspaceBinding) { b.Scope = "../outside" }},
		{"absolute scope", func(b *WorkspaceBinding) { b.Scope = "/outside" }},
		{"unclean root", func(b *WorkspaceBinding) { b.Worktree = "/repo/../other" }},
		{"missing gitdir identity", func(b *WorkspaceBinding) { b.GitIdentity = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := binding
			test.change(&value)
			if err := value.Validate(); !errors.Is(err, ErrInvalid) {
				t.Fatal(err)
			}
		})
	}
	object := strings.Repeat("a", 40)
	value := WorkspaceSnapshot{ID: "opaque", SessionID: "session", Binding: binding, ObjectID: &object, Semantics: WorkspaceSemantics}
	raw, err := json.Marshal(value)
	if err != nil || strings.Contains(string(raw), "/repo") || strings.Contains(string(raw), object) || strings.Contains(string(raw), "1:2") {
		t.Fatal("host identity leaked", string(raw), err)
	}
	for _, invalid := range []string{"HEAD", "--option", strings.Repeat("g", 40), strings.Repeat("a", 39)} {
		if err := ValidateWorkspaceObject(invalid); !errors.Is(err, ErrInvalid) {
			t.Fatal(invalid, err)
		}
	}
	for _, valid := range []string{strings.Repeat("a", 40), strings.Repeat("b", 64)} {
		if err := ValidateWorkspaceObject(valid); err != nil {
			t.Fatal(err)
		}
	}
}
