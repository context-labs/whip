package config

import (
	"errors"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestDefaultPermissionModeRevisionAndUnrelatedDeclarations(t *testing.T) {
	host := Default()
	host.ProjectRoots["keep"] = "/project"
	host.Providers["test"] = Provider{Kind: "openai-chat", BaseURL: "https://example.test/v1", CredentialSource: "none"}
	host.Defaults.Model = session.ModelSelection{Provider: "test", Name: "model"}
	authority, before := configAuthority(t, host)
	if mode, err := session.ResolvePermissionMode(before.Host.DefaultPermissionMode); err != nil || mode != session.PermissionPrompt {
		t.Fatal("omission is not Ask", mode, err)
	}
	automatic, err := authority.SetDefaultPermissionMode(t.Context(), before.Revision, session.PermissionAutomatic)
	if err != nil || automatic.Revision == before.Revision || automatic.Host.DefaultPermissionMode != session.PermissionAutomatic {
		t.Fatal("default edit failed", automatic, err)
	}
	expected := before.Host
	expected.DefaultPermissionMode = session.PermissionAutomatic
	if !reflect.DeepEqual(automatic.Host, expected) {
		t.Fatal("default edit changed unrelated host fields")
	}
	if _, err := authority.SetDefaultPermissionMode(t.Context(), before.Revision, session.PermissionPrompt); !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale edit wrote", err)
	}
	for _, mode := range []session.PermissionMode{"", "Prompt", "full_access", " automatic"} {
		if _, err := authority.SetDefaultPermissionMode(t.Context(), automatic.Revision, mode); !errors.Is(err, session.ErrInvalid) {
			t.Fatal("invalid mode accepted", mode, err)
		}
	}
	same, err := authority.SetDefaultPermissionMode(t.Context(), automatic.Revision, session.PermissionAutomatic)
	if err != nil || !reflect.DeepEqual(same, automatic) {
		t.Fatal("same mode changed revision", same, err)
	}
	loaded, err := Load(authority.directory)
	if err != nil || loaded.DefaultPermissionMode != session.PermissionAutomatic {
		t.Fatal("default not saved", loaded, err)
	}
	prompt, err := authority.SetDefaultPermissionMode(t.Context(), same.Revision, session.PermissionPrompt)
	if err != nil || prompt.Host.DefaultPermissionMode != session.PermissionPrompt || prompt.Revision == same.Revision {
		t.Fatal("explicit Ask not saved", prompt, err)
	}
	invalid := host
	invalid.DefaultPermissionMode = "unknown"
	if err := invalid.Validate(); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("invalid saved default silently fell back", err)
	}
}
