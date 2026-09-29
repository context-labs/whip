package providerhost

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcpconfig"
	"github.com/context-labs/whip/internal/session"
)

func TestCandidatesAreReadOnlyAndUseRechecksSourceAndRevision(t *testing.T) {
	f := newFixture(t)
	f.env["OPENAI_API_KEY"] = "private-fixture-key"
	f.env["UNRELATED_SECRET"] = "not-a-provider"
	f.route("command", config.Provider{Kind: "openai-chat", BaseURL: "https://fixture.test/v1", CredentialSource: "command", CredentialCommand: &config.CredentialCommand{Executable: "/must-not-execute"}})
	before := f.revision()
	value, err := f.service.Candidates(t.Context())
	if err != nil || len(value.Items) != 1 || value.Items[0].Provider != "openai" || value.Items[0].Environment != "OPENAI_API_KEY" || value.Revision != before || f.revision() != before || len(f.requests) != 0 {
		t.Fatalf("candidate discovery changed host or contacted provider: %+v %v", value, err)
	}
	raw, _ := json.Marshal(value)
	if strings.Contains(string(raw), "private-fixture-key") || strings.Contains(string(raw), "UNRELATED_SECRET") {
		t.Fatal("candidate response exposed unrelated or secret data")
	}
	delete(f.env, "OPENAI_API_KEY")
	if _, err := f.service.UseCandidate(t.Context(), before, value.Items[0]); !errors.Is(err, ErrCredentials) {
		t.Fatalf("disappeared credential accepted: %v", err)
	}
	f.env["OPENAI_API_KEY"] = "replacement-private-key"
	used, err := f.service.UseCandidate(t.Context(), before, value.Items[0])
	if err != nil || len(used.Routes) != 2 || used.Defaults.Name != "" || len(f.requests) != 0 {
		t.Fatalf("explicit use failed: %+v %v", used, err)
	}
	if _, err := f.service.UseCandidate(t.Context(), before, Candidate{Provider: "deepseek", Source: "env", Environment: "OPENAI_API_KEY"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("foreign environment source accepted: %v", err)
	}
	value, err = f.service.Candidates(t.Context())
	if err != nil || len(value.Items) != 0 {
		t.Fatalf("configured candidate remained: %+v %v", value, err)
	}
}

func TestPreferenceFormsPublishAtomicallyAndPreserveDefaultIntent(t *testing.T) {
	f := newFixture(t)
	f.route("provider", config.Provider{Kind: "openai-chat", BaseURL: "https://fixture.test/v1"})
	_, err := f.authority.Update(t.Context(), f.revision(), func(host *config.Host) error {
		host.MCP.Imports.Claude = &mcpconfig.ImportSource{Only: []string{"kept"}, Exclude: []string{"excluded"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := f.revision()
	selection := session.ModelSelection{Provider: "provider", Name: "model", Effort: "high"}
	value, err := f.service.SetPreferences(t.Context(), before, Defaults{Selection: &selection}, session.PermissionAutomatic)
	if err != nil || !value.Defaults.Equal(selection) || value.PermissionMode != session.PermissionAutomatic {
		t.Fatalf("provider form did not publish together: %+v %v", value, err)
	}
	if _, err := f.service.SetPreferences(t.Context(), before, Defaults{}, session.PermissionPrompt); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatalf("stale form published: %v", err)
	}
	before = value.Revision
	invalid := selection
	invalid.Provider = "absent"
	if _, err := f.service.SetPreferences(t.Context(), before, Defaults{Selection: &invalid}, session.PermissionPrompt); err == nil || f.revision() != before {
		t.Fatal("invalid provider form partially saved its permission mode")
	}
	preferences := ExecutionPreferences{Engine: session.QuickJS, CompactionPercent: 70, CompactionModel: Defaults{Selection: &selection}, MaxAttempts: 9, ImportClaude: false, ImportCodex: true}
	updated, err := f.service.SetExecutionPreferences(t.Context(), before, preferences)
	if err != nil || updated.Host.GoalMaxContinuations != nil || updated.Host.ExecutionDefaults().GoalMaxContinuations != 100 || updated.Host.MaxAttempts != 9 || updated.Host.Engine != session.QuickJS || updated.Host.Defaults.Compaction.Model == nil || *updated.Host.MCP.Imports.Claude.Enabled || updated.Host.MCP.Imports.Claude.Only[0] != "kept" || updated.Host.MCP.Imports.Claude.Exclude[0] != "excluded" {
		t.Fatalf("execution form did not publish together: %+v %v", updated.Host, err)
	}
	preferences.Engine = "invalid"
	if _, err := f.service.SetExecutionPreferences(t.Context(), updated.Revision, preferences); err == nil || f.revision() != updated.Revision {
		t.Fatal("invalid execution form partially saved")
	}
	preferences.Engine, preferences.MaxAttempts, preferences.GoalMaxContinuations = session.Starlark, 0, new(int64(0))
	updated, err = f.service.SetExecutionPreferences(t.Context(), updated.Revision, preferences)
	if err != nil || updated.Host.MaxAttempts != 0 || updated.Host.ExecutionDefaults().MaxAttempts != 3 || updated.Host.GoalMaxContinuations == nil || updated.Host.ExecutionDefaults().GoalMaxContinuations != 0 {
		t.Fatalf("explicit zero and default intent conflated: %+v %v", updated.Host, err)
	}
}

func TestEnableDisablePreservesDefaultsCredentialsAndRejectsStaleEdits(t *testing.T) {
	f := newFixture(t)
	f.env["KEY"] = "secret-fixture"
	f.route("provider", config.Provider{Kind: "openai-chat", BaseURL: "https://fixture.test/v1", CredentialEnv: "KEY"})
	selected := session.ModelSelection{Provider: "provider", Name: "model"}
	before, err := f.service.SetDefaults(t.Context(), f.revision(), Defaults{Selection: &selected})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := f.service.SetEnabled(t.Context(), before.Revision, "provider", false)
	if err != nil || !disabled.Routes[0].Disabled || !disabled.Defaults.Equal(selected) || disabled.Routes[0].Credential.State != "available" {
		t.Fatalf("disable lost route: %+v %v", disabled, err)
	}
	readiness, err := f.service.Readiness(t.Context(), selected)
	if err != nil || !readiness.Configured || !readiness.Disabled {
		t.Fatalf("disabled readiness: %+v %v", readiness, err)
	}
	if _, err := f.service.Refresh(t.Context(), "provider"); !errors.Is(err, ErrDisabled) || len(f.requests) != 0 {
		t.Fatalf("disabled refresh dispatched: %v", err)
	}
	if _, err := f.service.SetEnabled(t.Context(), before.Revision, "provider", true); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatalf("stale enable succeeded: %v", err)
	}
	enabled, err := f.service.SetEnabled(t.Context(), disabled.Revision, "provider", true)
	if err != nil || enabled.Routes[0].Disabled || !enabled.Defaults.Equal(selected) {
		t.Fatalf("reenable failed: %+v %v", enabled, err)
	}
}
