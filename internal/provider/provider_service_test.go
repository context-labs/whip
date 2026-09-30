package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
)

func waitProviderState(t *testing.T, service *ProviderService, id, state string) protocol.ProviderLoginStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, err := service.LoginStatus(id)
		if err != nil {
			t.Fatal(err)
		}
		if result.State == state {
			return result
		}
		time.Sleep(time.Millisecond)
	}
	result, _ := service.LoginStatus(id)
	t.Fatalf("wanted %s, got %s", state, result.State)
	return protocol.ProviderLoginStatus{}
}

func TestProviderLoginReconnectChoicesAndSecretIsolation(t *testing.T) {
	s := NewProviderService(t.Context(), "generation-a")
	defer s.Close()
	s.login = func(ctx context.Context, code func(string, string)) (providerLoginIdentity, error) {
		code("https://example.com/verify", "public-code")
		return providerLoginIdentity{token: "secret-session-token", email: "person@example.com", teams: []inferencenet.Team{{ID: "team", Name: "Team"}, {ID: "other", Name: "Other"}}}, nil
	}
	s.projects = func(ctx context.Context, token string, team inferencenet.Team) ([]inferencenet.Project, error) {
		if token != "secret-session-token" || team.ID != "team" {
			return nil, errors.New("bad selection")
		}
		return []inferencenet.Project{{ID: "project", Name: "Project"}, {ID: "other", Name: "Other"}}, nil
	}
	finished := make(chan inferencenet.Auth, 1)
	s.finish = func(ctx context.Context, auth inferencenet.Auth) error { finished <- auth; return nil }
	started, err := s.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	status := waitProviderState(t, s, started.FlowID, "choose_team")
	if status.VerificationURL == "" || len(status.Teams) != 2 {
		t.Fatal("missing reconnect choices")
	}
	status.Teams[0].ID = "mutated"
	if _, err := s.SelectLoginTeam(started.FlowID, "invalid"); err == nil {
		t.Fatal("accepted unknown team")
	}
	if _, err := s.SelectLoginTeam(started.FlowID, "team"); err != nil {
		t.Fatal(err)
	}
	waitProviderState(t, s, started.FlowID, "choose_project")
	if _, err := s.SelectLoginProject(started.FlowID, "project"); err != nil {
		t.Fatal(err)
	}
	status = waitProviderState(t, s, started.FlowID, "succeeded")
	auth := <-finished
	if auth.SessionToken != "secret-session-token" || auth.ProjectID != "project" || auth.TeamName != "Team" {
		t.Fatal("missing host-side auth")
	}
	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "SessionToken") {
		t.Fatal("secret exposed")
	}
	s.mu.Lock()
	retained := s.flows[started.FlowID].token
	s.mu.Unlock()
	if retained != "" {
		t.Fatal("flow retained session token after completion")
	}
	if _, err := s.SelectLoginProject(started.FlowID, "project"); err == nil {
		t.Fatal("duplicate provisioning admitted")
	}
}

func TestProviderLoginCancellationExpiryAndRestart(t *testing.T) {
	for _, action := range []string{"cancel", "expire", "restart"} {
		t.Run(action, func(t *testing.T) {
			s := NewProviderService(t.Context(), "generation-a")
			s.lifetime = 20 * time.Millisecond
			s.login = func(ctx context.Context, code func(string, string)) (providerLoginIdentity, error) {
				<-ctx.Done()
				return providerLoginIdentity{}, ctx.Err()
			}
			started, err := s.BeginLogin()
			if err != nil {
				t.Fatal(err)
			}
			switch action {
			case "cancel":
				if _, err := s.CancelLogin(started.FlowID); err != nil {
					t.Fatal(err)
				}
				waitProviderState(t, s, started.FlowID, "cancelled")
			case "expire":
				waitProviderState(t, s, started.FlowID, "expired")
			case "restart":
				s.Close()
				next := NewProviderService(t.Context(), "generation-b")
				defer next.Close()
				status, err := next.LoginStatus(started.FlowID)
				if err != nil || status.State != "interrupted" {
					t.Fatalf("restart: %+v %v", status, err)
				}
			}
			s.Close()
		})
	}
}

func TestProviderSetupRevisionAndSafeConfiguration(t *testing.T) {
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	s := NewProviderService(t.Context(), "generation")
	defer s.Close()
	s.validate = func(ctx context.Context, url, key string) ([]llm.ModelInfo, error) {
		if key != "secret-key" {
			return nil, errors.New("invalid key")
		}
		return []llm.ModelInfo{{ID: "fixture-chat-model"}}, nil
	}
	before, err := s.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.SetProviderKey(t.Context(), protocol.ProviderKeySetup{Revision: before.Revision, Provider: "openrouter", Key: "secret-key"})
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision == before.Revision {
		t.Fatal("revision did not change")
	}
	if _, err := s.SetProviderKey(t.Context(), protocol.ProviderKeySetup{Revision: before.Revision, Provider: "openrouter", Key: "secret-key"}); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatalf("conflict: %v", err)
	}
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Providers["openrouter"].APIKey != "secret-key" {
		t.Fatal("key not saved on host")
	}
	encoded, _ := json.Marshal(after)
	if strings.Contains(string(encoded), "secret-key") {
		t.Fatal("configuration exposed credential")
	}
}

func TestProviderLoginListRecoversLostAcknowledgementAndIsBounded(t *testing.T) {
	s := NewProviderService(t.Context(), "generation")
	defer s.Close()
	s.login = func(ctx context.Context, code func(string, string)) (providerLoginIdentity, error) {
		<-ctx.Done()
		return providerLoginIdentity{}, ctx.Err()
	}
	var first string
	for range 64 {
		flow, err := s.BeginLogin()
		if err != nil {
			t.Fatal(err)
		}
		if first == "" {
			first = flow.FlowID
			recovered, err := s.BeginLogin()
			if err != nil || recovered.FlowID != first || len(s.ListLogins().Flows) != 1 {
				t.Fatal("lost begin acknowledgement did not recover the existing flow")
			}
		}
		if _, err := s.CancelLogin(flow.FlowID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.BeginLogin(); err == nil {
		t.Fatal("retained login bound was not enforced")
	}
	result := s.ListLogins()
	if len(result.Flows) != 64 {
		t.Fatal("retained login outcomes were lost")
	}
	s.mu.Lock()
	s.flows[first].status.ExpiresAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, err := s.BeginLogin(); err != nil {
		t.Fatal("expired terminal login did not release capacity")
	}
}

func TestProviderFailureDoesNotExposeUpstreamSecret(t *testing.T) {
	s := NewProviderService(t.Context(), "generation")
	defer s.Close()
	s.login = func(ctx context.Context, code func(string, string)) (providerLoginIdentity, error) {
		return providerLoginIdentity{}, errors.New("upstream echoed secret-token")
	}
	result, err := s.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	result = waitProviderState(t, s, result.FlowID, "failed")
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "secret-token") {
		t.Fatal("upstream secret exposed to client")
	}
}

func TestProviderLoginChangesWorkspaceBeforeProvisioning(t *testing.T) {
	s := NewProviderService(t.Context(), "workspace-change")
	defer s.Close()
	s.login = func(context.Context, func(string, string)) (providerLoginIdentity, error) {
		return providerLoginIdentity{token: "test-token", teams: []inferencenet.Team{{ID: "a"}, {ID: "b"}}}, nil
	}
	release := make(chan struct{})
	s.projects = func(ctx context.Context, _ string, team inferencenet.Team) ([]inferencenet.Project, error) {
		if team.ID == "b" {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return []inferencenet.Project{{ID: team.ID + "-1"}, {ID: team.ID + "-2"}}, nil
	}
	started, err := s.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	waitProviderState(t, s, started.FlowID, "choose_team")
	if _, err := s.SelectLoginTeam(started.FlowID, "a"); err != nil {
		t.Fatal(err)
	}
	waitProviderState(t, s, started.FlowID, "choose_project")
	loading, err := s.SelectLoginTeam(started.FlowID, "b")
	if err != nil {
		t.Fatal(err)
	}
	if loading.State != "loading_projects" || loading.TeamID != "b" || len(loading.Projects) != 0 {
		t.Fatalf("retained previous workspace: %+v", loading)
	}
	if _, err := s.SelectLoginTeam(started.FlowID, "a"); err == nil {
		t.Fatal("allowed overlapping project discovery")
	}
	close(release)
	ready := waitProviderState(t, s, started.FlowID, "choose_project")
	if len(ready.Projects) != 2 || ready.Projects[0].ID != "b-1" {
		t.Fatalf("wrong workspace projects: %+v", ready.Projects)
	}
	if _, err := s.SelectLoginProject(started.FlowID, "a-1"); err == nil {
		t.Fatal("accepted previous workspace project")
	}
	if _, err := s.CancelLogin(started.FlowID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SelectLoginTeam(started.FlowID, "a"); err == nil {
		t.Fatal("resumed a cancelled login")
	}
}

func compactionConfigurationFixture(t *testing.T) (*ProviderService, *config.Config) {
	t.Helper()
	t.Setenv("WHIPCODE_HOME", t.TempDir())
	t.Setenv("WHIP_TEST_MISSING_COMPACTION_KEY", "")
	cfg := &config.Config{
		DefaultModel: "main",
		Providers: map[string]config.Provider{
			"ready":    {BaseURL: "https://ready.test/v1", Auth: "none"},
			"other":    {BaseURL: "https://other.test/v1", Auth: "none"},
			"offline":  {BaseURL: "https://offline.test/v1", APIKeyEnv: "WHIP_TEST_MISSING_COMPACTION_KEY"},
			"disabled": {BaseURL: "https://disabled.test/v1", Auth: "none"},
		},
		Models: map[string]config.Model{
			"main":             {Providers: []string{"ready"}},
			"summary":          {Providers: []string{"ready"}},
			"offline-summary":  {Providers: []string{"offline"}},
			"disabled-summary": {Providers: []string{"disabled"}},
		},
		DisabledProviders: []string{"disabled"},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCatalogs(map[string]config.Catalog{
		"ready": {BaseURL: "https://ready.test/v1", Models: []config.ModelInfoLite{
			{ID: "catalog-only"}, {ID: "ambiguous"},
		}},
		"other": {BaseURL: "https://other.test/v1", Models: []config.ModelInfoLite{{ID: "ambiguous"}}},
	}); err != nil {
		t.Fatal(err)
	}
	s := NewProviderService(t.Context(), "compaction-settings")
	t.Cleanup(s.Close)
	return s, cfg
}

func TestProviderCompactionConfigurationSelections(t *testing.T) {
	tests := []struct {
		name         string
		model        string
		provider     string
		wantProvider string
		wantErr      bool
	}{
		{name: "alias resolves provider", model: "summary", wantProvider: "ready"},
		{name: "catalog resolves provider", model: "catalog-only", wantProvider: "ready"},
		{name: "explicit route", model: "summary", provider: "ready", wantProvider: "ready"},
		{name: "automatic clears provider", provider: "stale"},
		{name: "unknown model", model: "missing", provider: "ready", wantErr: true},
		{name: "unknown provider", model: "summary", provider: "missing", wantErr: true},
		{name: "missing credentials", model: "offline-summary", wantErr: true},
		{name: "disabled provider", model: "disabled-summary", wantErr: true},
		{name: "wrong provider", model: "summary", provider: "other", wantErr: true},
		{name: "ambiguous route", model: "ambiguous", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s, _ := compactionConfigurationFixture(t)
			before, err := s.ReadConfiguration()
			if err != nil {
				t.Fatal(err)
			}
			after, err := s.UpdateConfiguration(protocol.ConfigurationUpdate{
				Revision: before.Revision, CompactModel: &test.model, CompactProvider: &test.provider,
				CompactPercent: new(60), MaxRetries: new(3),
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("update error = %v, wantErr %t", err, test.wantErr)
			}
			persisted, readErr := s.ReadConfiguration()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if test.wantErr {
				if persisted.Revision != before.Revision {
					t.Fatal("invalid route partially saved configuration")
				}
				return
			}
			if after.CompactModel != test.model || after.CompactProvider != test.wantProvider {
				t.Fatalf("compaction selection = %q %q; want %q %q",
					after.CompactModel, after.CompactProvider, test.model, test.wantProvider)
			}
			if persisted.CompactModel != after.CompactModel || persisted.CompactProvider != after.CompactProvider {
				t.Fatal("resolved model and provider were not persisted together")
			}
			if after.CompactPercent != 60 || after.MaxRetries != 3 {
				t.Fatal("valid settings were not saved atomically")
			}
		})
	}
}

func TestProviderCompactionConfigurationPreservesUnchangedLegacyOverrides(t *testing.T) {
	for _, model := range []string{"deepseek-v4-flash-0731", "missing-summary"} {
		t.Run(model, func(t *testing.T) {
			s, cfg := compactionConfigurationFixture(t)
			cfg.CompactModel, cfg.CompactProvider, cfg.CompactPct = model, "missing-provider", 95
			if err := cfg.Save(); err != nil {
				t.Fatal(err)
			}
			before, err := s.ReadConfiguration()
			if err != nil {
				t.Fatal(err)
			}
			after, err := s.UpdateConfiguration(protocol.ConfigurationUpdate{
				Revision: before.Revision, CompactModel: &before.CompactModel,
				CompactProvider: &before.CompactProvider, CompactPercent: &before.CompactPercent,
				DefaultModel: &before.DefaultModel, DefaultProvider: &before.DefaultProvider, MaxRetries: new(4),
			})
			if err != nil {
				t.Fatal(err)
			}
			if after.CompactModel != model || after.CompactProvider != "missing-provider" || after.CompactPercent != 95 || after.MaxRetries != 4 {
				t.Fatalf("legacy override was changed: %+v", after)
			}
			if _, err := s.UpdateConfiguration(protocol.ConfigurationUpdate{
				Revision: after.Revision, CompactProvider: new("different-missing-provider"),
			}); err == nil {
				t.Fatal("changed legacy route was not validated")
			}
			cleared, err := s.UpdateConfiguration(protocol.ConfigurationUpdate{
				Revision: after.Revision, CompactModel: new(""), CompactProvider: &after.CompactProvider,
			})
			if err != nil || cleared.CompactModel != "" || cleared.CompactProvider != "" {
				t.Fatalf("automatic did not clear override: %+v %v", cleared, err)
			}
		})
	}
}

func TestProviderCompactionConfigurationThresholdValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		percent int
		wantErr bool
	}{
		{name: "automatic"},
		{name: "minimum", percent: 10},
		{name: "maximum", percent: 90},
		{name: "negative", percent: -1, wantErr: true},
		{name: "below minimum", percent: 9, wantErr: true},
		{name: "above maximum", percent: 91, wantErr: true},
		{name: "over 100", percent: 101, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := compactionConfigurationFixture(t)
			before, err := s.ReadConfiguration()
			if err != nil {
				t.Fatal(err)
			}
			after, err := s.UpdateConfiguration(protocol.ConfigurationUpdate{
				Revision: before.Revision, CompactPercent: &test.percent, MaxRetries: new(5),
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("threshold error = %v, wantErr %t", err, test.wantErr)
			}
			if !test.wantErr && after.CompactPercent != test.percent {
				t.Fatalf("threshold = %d, want %d", after.CompactPercent, test.percent)
			}
			persisted, err := s.ReadConfiguration()
			if err != nil {
				t.Fatal(err)
			}
			if test.wantErr && persisted.Revision != before.Revision {
				t.Fatal("invalid threshold partially saved configuration")
			}
		})
	}
}
