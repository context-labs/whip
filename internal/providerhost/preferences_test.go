package providerhost

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferenceauth"
	"github.com/context-labs/whip/internal/mcpconfig"
	"github.com/context-labs/whip/internal/session"
)

func TestEnvironmentImportUsesPresetFallbacksAndPreservesSavedConfiguration(t *testing.T) {
	f := newFixture(t)
	f.route("openai", noAuth())
	f.route("openrouter", config.Provider{Kind: "openai-chat", BaseURL: "https://saved.test/v1", Disabled: true, CredentialEnv: "SAVED_KEY"})
	f.route("command", config.Provider{Kind: "openai-chat", BaseURL: "https://fixture.test/v1", CredentialSource: "command", CredentialCommand: &config.CredentialCommand{Executable: "/must-not-execute"}})
	selection := session.ModelSelection{Provider: "openai", Name: "saved-model"}
	if _, err := f.service.SetDefaults(t.Context(), f.revision(), Defaults{Selection: &selection}); err != nil {
		t.Fatal(err)
	}
	before, err := f.authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f.env["OPENAI_API_KEY"], f.env["OPENROUTER_API_KEY"] = "ignored-private-key", "ignored-private-key"
	f.env["DEEPINFRA_API_KEY"], f.env["DEEPINFRA_TOKEN"] = "invalid\nkey", "private-fallback-key"
	f.env["XAI_API_KEY"], f.env["GROQ_API_KEY"] = "private-key", "  "
	f.env["UNRELATED_SECRET"] = "not-a-provider"
	service, err := New(t.Context(), f.authority, f.service.http, f.service.lookup, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	value, err := service.List(t.Context())
	if err != nil || len(value.Routes) != 5 || !value.Defaults.Equal(selection) || f.count() != 0 {
		t.Fatalf("environment import: %+v %v", value, err)
	}
	for _, id := range []string{"deepinfra", "xai"} {
		ready, err := service.Readiness(t.Context(), session.ModelSelection{Provider: id, Name: "model"})
		if err != nil || !ready.Configured || ready.CredentialState != "available" {
			t.Fatalf("imported provider is not usable: %+v %v", ready, err)
		}
	}
	after, err := f.authority.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if p := after.Host.Providers["deepinfra"]; p.CredentialSource != "env" || p.CredentialEnv != "DEEPINFRA_TOKEN" || p.BaseURL != "https://api.deepinfra.com/v1/openai" {
		t.Fatalf("preset fallback was not saved: %+v", p)
	}
	delete(after.Host.Providers, "deepinfra")
	delete(after.Host.Providers, "xai")
	if !reflect.DeepEqual(before.Host, after.Host) {
		t.Fatal("import changed saved configuration")
	}
	stored, err := os.ReadFile(filepath.Join(f.directory, config.FileName))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(value)
	if strings.Contains(string(stored)+string(raw), "private-") || strings.Contains(string(stored)+string(raw), "UNRELATED_SECRET") {
		t.Fatal("import copied secret contents or unrelated environment")
	}
	candidates, err := service.Candidates(t.Context())
	if err != nil || len(candidates.Items) != 0 {
		t.Fatalf("environment credentials remained candidates: %+v %v", candidates, err)
	}
	restarted, err := New(t.Context(), f.authority, f.service.http, f.service.lookup, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if f.revision() != value.Revision || f.count() != 0 {
		t.Fatal("restart republished configuration or contacted a provider")
	}
}

func TestAccountCandidatesAreReadOnlyAndUseRechecksSourceAndRevision(t *testing.T) {
	f := newFixture(t)
	manager, err := inferenceauth.New(t.Context(), f.directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	f.service.inference = manager
	credential := inferenceauth.Credentials{Scope: inferenceauth.Scope{TeamID: "team", ProjectID: "project"}, MachineKey: inferenceauth.MachineKey{ID: "key", Value: "private-machine-key"}}
	if err := manager.Install(t.Context(), manager.Generation(), credential); err != nil {
		t.Fatal(err)
	}
	before := f.revision()
	value, err := f.service.Candidates(t.Context())
	if err != nil || len(value.Items) != 1 || value.Items[0].Provider != "inference-net" || value.Items[0].Source != "inference-net" || value.Revision != before || f.revision() != before || f.count() != 0 {
		t.Fatalf("candidate discovery changed host or contacted provider: %+v %v", value, err)
	}
	raw, _ := json.Marshal(value)
	if strings.Contains(string(raw), credential.MachineKey.Value) {
		t.Fatal("candidate response exposed secret data")
	}
	if _, err := manager.Logout(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.UseCandidate(t.Context(), before, value.Items[0]); !errors.Is(err, ErrCredentials) {
		t.Fatalf("disappeared credential accepted: %v", err)
	}
	if err := manager.Install(t.Context(), manager.Generation(), credential); err != nil {
		t.Fatal(err)
	}
	f.route("other", noAuth())
	if _, err := f.service.UseCandidate(t.Context(), before, value.Items[0]); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatalf("stale candidate revision accepted: %v", err)
	}
	used, err := f.service.UseCandidate(t.Context(), f.revision(), value.Items[0])
	if err != nil || len(used.Routes) != 2 || used.Defaults.Name != "" || f.count() != 0 {
		t.Fatalf("explicit use failed: %+v %v", used, err)
	}
	if _, err := f.service.UseCandidate(t.Context(), before, Candidate{Provider: "openai", Source: "env"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("retired environment candidate accepted: %v", err)
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
