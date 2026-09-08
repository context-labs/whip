package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

type providerBehaviorClient interface {
	ReadConfiguration(context.Context) (RuntimeConfiguration, error)
	UpdateConfiguration(context.Context, ConfigurationUpdate) (RuntimeConfiguration, error)
	SetProviderKey(context.Context, ProviderKeySetup) (RuntimeConfiguration, error)
	BeginLogin(context.Context) (ProviderLoginStatus, error)
	LoginStatus(context.Context, string) (ProviderLoginStatus, error)
	CancelLogin(context.Context, string) (ProviderLoginStatus, error)
	SelectLoginTeam(context.Context, string, string) (ProviderLoginStatus, error)
	SelectLoginProject(context.Context, string, string) (ProviderLoginStatus, error)
	CreateLoginProject(context.Context, string, string) (ProviderLoginStatus, error)
	ListLogins(context.Context) (ProviderLoginList, error)
	ProviderStatus(context.Context, string) (ProviderStatus, error)
	LogoutProvider(context.Context, string) (ProviderStatus, error)
	RotateProviderKey(context.Context, string) (ProviderStatus, error)
}

func providerBehaviorFixture(t *testing.T) (*Server, *Client, *RootClient, string) {
	t.Helper()
	t.Setenv("WHIP_HOME", t.TempDir())
	store := openStore(t, filepath.Join(t.TempDir(), "sessions.db"))
	rootID := createRoot(t, store)
	owner, err := New(store, func(context.Context, session.Meta, []llm.Message) (Components, error) {
		return Components{Runner: &fakeRunner{}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	server, client, served := startTCPClient(t, owner, "provider-behavior")
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	root, err := NewRootClient(RootClientOptions{ClientID: "provider-behavior", RootID: rootID, Connector: func(context.Context, map[string]int64) (RootConnection, error) { return client, nil }})
	if err != nil {
		t.Fatal(err)
	}
	root.Start()
	t.Cleanup(func() { _ = root.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := root.WaitLive(ctx); err != nil {
		t.Fatal(err)
	}
	return server, client, root, rootID
}

func TestProviderClientRemoteHostsPreserveConfigurationAndRejectConflicts(t *testing.T) {
	_, client, root, _ := providerBehaviorFixture(t)
	c, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	provider := c.Providers["inference-net"]
	provider.APIKey = "private-test-key"
	c.Providers["inference-net"] = provider
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := client.ReadConfiguration(t.Context())
	if err != nil || before.RemoteHosts == nil || len(*before.RemoteHosts) != 0 {
		t.Fatalf("legacy config must advertise an empty host list: %+v %v", before, err)
	}
	hosts := []config.RemoteHost{{
		ID: "kuzco", Name: "Kuzco", URL: "ws://kuzco.test/api/v3/ws",
		RuntimeID: "remote-runtime", ConnectOnLaunch: true,
	}}
	after, err := client.UpdateConfiguration(t.Context(), ConfigurationUpdate{Revision: before.Revision, RemoteHosts: &hosts})
	if err != nil {
		t.Fatal(err)
	}
	read, err := root.ReadConfiguration(t.Context())
	if err != nil || !reflect.DeepEqual(read, after) || len(*read.RemoteHosts) != 1 {
		t.Fatalf("another client cannot read persisted profiles: %+v %v", read, err)
	}
	if (*read.RemoteHosts)[0].URL != "http://kuzco.test" {
		t.Fatalf("endpoint was not normalized: %+v", *read.RemoteHosts)
	}
	empty := []config.RemoteHost{}
	if _, err := root.UpdateConfiguration(t.Context(), ConfigurationUpdate{Revision: before.Revision, RemoteHosts: &empty}); err == nil {
		t.Fatal("stale browser overwrote host list")
	}
	persisted, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Providers["inference-net"].APIKey != provider.APIKey || persisted.DefaultModel != c.DefaultModel {
		t.Fatal("host update changed provider configuration")
	}
	encoded, err := json.Marshal(after)
	if err != nil || strings.Contains(string(encoded), provider.APIKey) {
		t.Fatalf("config response exposed a credential: %v", err)
	}
	cleared, err := root.UpdateConfiguration(t.Context(), ConfigurationUpdate{Revision: after.Revision, RemoteHosts: &empty})
	if err != nil || cleared.RemoteHosts == nil || len(*cleared.RemoteHosts) != 0 {
		t.Fatalf("cannot remove last profile: %+v %v", cleared, err)
	}
}

func TestProviderClientOnboardingPersistsSettingsWithoutJournalingSecrets(t *testing.T) {
	for _, mode := range []string{"client", "root"} {
		t.Run(mode, func(t *testing.T) {
			server, client, root, rootID := providerBehaviorFixture(t)
			var api providerBehaviorClient = client
			if mode == "root" {
				api = root
			}
			service := server.providers
			service.validate = func(_ context.Context, _, key string) ([]llm.ModelInfo, error) {
				if key != "private-api-key" {
					return nil, errors.New("bad key")
				}
				return []llm.ModelInfo{{ID: "fixture-model"}}, nil
			}
			service.login = func(_ context.Context, code func(string, string)) (providerLoginIdentity, error) {
				code("https://example.test/verify", "display-code")
				return providerLoginIdentity{token: "private-session-token", email: "person@example.test", teams: []inferencenet.Team{{ID: "team", Name: "Team"}}}, nil
			}
			service.projects = func(context.Context, string, inferencenet.Team) ([]inferencenet.Project, error) {
				return []inferencenet.Project{{ID: "existing", Name: "Existing"}}, nil
			}
			service.create = func(_ context.Context, token string, team inferencenet.Team, name string) (inferencenet.Project, error) {
				if token != "private-session-token" || team.ID != "team" || name != "Created" {
					return inferencenet.Project{}, errors.New("invalid creation identity")
				}
				return inferencenet.Project{ID: "created", Name: name}, nil
			}
			finished := make(chan inferencenet.Auth, 2)
			service.finish = func(_ context.Context, auth inferencenet.Auth) error { finished <- auth; return nil }
			before, err := api.ReadConfiguration(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			after, err := api.UpdateConfiguration(t.Context(), ConfigurationUpdate{Revision: before.Revision, ImportClaude: new(false), ImportCodex: new(false), DefaultModel: new("fixture-model"), DefaultProvider: new("openrouter"), DefaultEffort: new("high"), CompactModel: new("compact-model"), CompactProvider: new("openrouter"), CompactPercent: new(72), GoalMaxRounds: new(8), MaxRetries: new(2)})
			if err != nil {
				t.Fatal(err)
			}
			persisted, err := api.ReadConfiguration(t.Context())
			if err != nil || !reflect.DeepEqual(persisted, after) || persisted.ImportClaude || persisted.ImportCodex || persisted.DefaultModel != "fixture-model" || persisted.DefaultEffort != "high" || persisted.CompactPercent != 72 || persisted.GoalMaxRounds != 8 || persisted.MaxRetries != 2 {
				t.Fatalf("settings did not persist: %+v %v", persisted, err)
			}
			if _, err := api.UpdateConfiguration(t.Context(), ConfigurationUpdate{Revision: before.Revision, MaxRetries: new(99)}); err == nil {
				t.Fatal("stale update accepted")
			}
			after, err = api.SetProviderKey(t.Context(), ProviderKeySetup{Revision: after.Revision, Provider: "openrouter", Key: " private-api-key "})
			if err != nil {
				t.Fatal(err)
			}
			status, err := api.ProviderStatus(t.Context(), "openrouter")
			if err != nil || !status.Configured || status.KeySource != "literal" {
				t.Fatalf("provider status=%+v %v", status, err)
			}
			cfg, err := config.Load()
			if err != nil || cfg.Providers["openrouter"].APIKey != "private-api-key" {
				t.Fatalf("host key not persisted: %v", err)
			}
			for _, project := range []string{"existing", "created"} {
				flow, err := api.BeginLogin(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				waitProviderState(t, service, flow.FlowID, "choose_team")
				if _, err := api.SelectLoginTeam(t.Context(), flow.FlowID, "team"); err != nil {
					t.Fatal(err)
				}
				waitProviderState(t, service, flow.FlowID, "choose_project")
				if project == "existing" {
					_, err = api.SelectLoginProject(t.Context(), flow.FlowID, project)
				} else {
					_, err = api.CreateLoginProject(t.Context(), flow.FlowID, "  Created  ")
				}
				if err != nil {
					t.Fatal(err)
				}
				waitProviderState(t, service, flow.FlowID, "succeeded")
				result, err := api.LoginStatus(t.Context(), flow.FlowID)
				if err != nil || result.State != "succeeded" || result.ProjectID != project {
					t.Fatalf("login=%+v %v", result, err)
				}
				select {
				case auth := <-finished:
					if auth.ProjectID != project || auth.SessionToken != "private-session-token" {
						t.Fatalf("wrong provisioning identity: %+v", auth)
					}
				case <-time.After(time.Second):
					t.Fatal("provisioning did not complete")
				}
			}
			flow, err := api.BeginLogin(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			cancelled, err := api.CancelLogin(t.Context(), flow.FlowID)
			if err != nil || cancelled.State != "cancelled" {
				t.Fatalf("cancel=%+v %v", cancelled, err)
			}
			logins, err := api.ListLogins(t.Context())
			if err != nil || len(logins.Flows) != 3 {
				t.Fatalf("lost login states: %+v %v", logins, err)
			}
			if _, err := api.RotateProviderKey(t.Context(), config.InferenceNetProvider); err == nil {
				t.Fatal("unsigned account rotated a key")
			}
			if _, err := api.LogoutProvider(t.Context(), config.InferenceNetProvider); err != nil {
				t.Fatal(err)
			}
			for _, provider := range []string{"unknown", "openrouter"} {
				if _, err := api.RotateProviderKey(t.Context(), provider); err == nil {
					t.Fatal("unsupported key rotation accepted")
				}
				if _, err := api.LogoutProvider(t.Context(), provider); err == nil {
					t.Fatal("unsupported logout accepted")
				}
			}
			replay, err := client.Replay(t.Context(), ReplayParams{RootID: rootID, Limit: session.MaxEventReplay})
			if err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal([]any{after, status, logins, replay})
			if strings.Contains(string(encoded), "private-api-key") || strings.Contains(string(encoded), "private-session-token") {
				t.Fatal("credentials escaped host storage")
			}
		})
	}
}

func TestProviderConfigurationRejectsInvalidChangesAtomically(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "validation")
	defer service.Close()
	before, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change ConfigurationUpdate
	}{
		{"missing revision", ConfigurationUpdate{}},
		{"effort", ConfigurationUpdate{Revision: before.Revision, DefaultEffort: new("turbo")}},
		{"negative compact", ConfigurationUpdate{Revision: before.Revision, CompactPercent: new(-1)}},
		{"large compact", ConfigurationUpdate{Revision: before.Revision, CompactPercent: new(101)}},
		{"rounds", ConfigurationUpdate{Revision: before.Revision, GoalMaxRounds: new(-1)}},
		{"retries", ConfigurationUpdate{Revision: before.Revision, MaxRetries: new(-1)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.UpdateConfiguration(tc.change); err == nil {
				t.Fatal("invalid update accepted")
			}
			after, err := service.ReadConfiguration()
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("failed update changed configuration: %+v %v", after, err)
			}
		})
	}
	for _, p := range []ProviderKeySetup{{}, {Revision: before.Revision, Provider: "unknown"}, {Revision: before.Revision, Provider: "openrouter"}} {
		if _, err := service.SetProviderKey(t.Context(), p); err == nil {
			t.Fatal("invalid key setup accepted")
		}
	}
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		return nil, errors.New("provider leaked secret-key")
	}
	if _, err := service.SetProviderKey(t.Context(), ProviderKeySetup{Revision: before.Revision, Provider: "openrouter", Key: "key"}); err == nil || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("unsafe validation failure: %v", err)
	}
	if _, err := service.LoginStatus(""); err == nil {
		t.Fatal("empty login identity accepted")
	}
	if _, err := service.CancelLogin("missing"); err == nil {
		t.Fatal("missing login cancelled")
	}
	if _, err := service.SelectLoginTeam("missing", "team"); err == nil {
		t.Fatal("missing login selected team")
	}
	if _, err := service.SelectLoginProject("missing", "project"); err == nil {
		t.Fatal("missing login selected project")
	}
	if _, err := service.CreateLoginProject("missing", ""); err == nil {
		t.Fatal("blank project name accepted")
	}
	service.Close()
	if _, err := service.BeginLogin(); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed login service: %v", err)
	}
}

func TestRootProviderValidationUsesEphemeralConnection(t *testing.T) {
	_, _, root, _ := providerBehaviorFixture(t)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer root-secret" {
			http.Error(w, "bad credential", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"root-validated"}]}`))
	}))
	defer endpoint.Close()
	result, err := root.ValidateProvider(t.Context(), ProviderValidateParams{Name: "temporary", BaseURL: endpoint.URL, Key: "root-secret"})
	if err != nil || len(result.Models) != 1 || result.Models[0].ID != "root-validated" {
		t.Fatalf("validation=%+v %v", result, err)
	}
	if _, err := root.ValidateProvider(t.Context(), ProviderValidateParams{Name: "temporary", BaseURL: endpoint.URL, Key: "wrong"}); err == nil {
		t.Fatal("rejected key validated")
	}
	_ = root.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := root.ValidateProvider(ctx, ProviderValidateParams{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed validation=%v", err)
	}
	if _, err := root.ReadConfiguration(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed config read=%v", err)
	}
}

func TestProviderLoginFailuresAndCancellationNeverReportSuccess(t *testing.T) {
	for _, stage := range []string{"projects", "create", "finish", "cancel_provisioning", "cancel_projects"} {
		t.Run(stage, func(t *testing.T) {
			t.Setenv("WHIP_HOME", t.TempDir())
			service := NewProviderService(t.Context(), "failure")
			defer service.Close()
			entered := make(chan struct{})
			service.login = func(context.Context, func(string, string)) (providerLoginIdentity, error) {
				return providerLoginIdentity{token: "private-token", teams: []inferencenet.Team{{ID: "team"}}}, nil
			}
			service.projects = func(ctx context.Context, _ string, _ inferencenet.Team) ([]inferencenet.Project, error) {
				if stage == "projects" {
					return nil, errors.New("upstream leaked private-token")
				}
				if stage == "cancel_projects" {
					close(entered)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return []inferencenet.Project{{ID: "project"}}, nil
			}
			service.create = func(context.Context, string, inferencenet.Team, string) (inferencenet.Project, error) {
				return inferencenet.Project{}, errors.New("upstream leaked private-token")
			}
			service.finish = func(ctx context.Context, _ inferencenet.Auth) error {
				if stage == "cancel_provisioning" {
					close(entered)
					<-ctx.Done()
					return ctx.Err()
				}
				return errors.New("upstream leaked private-token")
			}
			flow, err := service.BeginLogin()
			if err != nil {
				t.Fatal(err)
			}
			waitProviderState(t, service, flow.FlowID, "choose_team")
			if _, err := service.SelectLoginTeam(flow.FlowID, "team"); err != nil {
				t.Fatal(err)
			}
			if stage != "projects" && stage != "cancel_projects" {
				waitProviderState(t, service, flow.FlowID, "choose_project")
				if stage == "create" {
					_, err = service.CreateLoginProject(flow.FlowID, "created")
				} else {
					_, err = service.SelectLoginProject(flow.FlowID, "project")
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			want := "failed"
			if strings.HasPrefix(stage, "cancel_") {
				select {
				case <-entered:
				case <-time.After(2 * time.Second):
					t.Fatal("provider operation did not begin")
				}
				if _, err := service.CancelLogin(flow.FlowID); err != nil {
					t.Fatal(err)
				}
				want = "cancelled"
				if stage == "cancel_provisioning" {
					want = "interrupted"
				}
			}
			status := waitProviderState(t, service, flow.FlowID, want)
			service.Close()
			final, err := service.LoginStatus(flow.FlowID)
			if err != nil || final.State != want || final.ProjectID != "" {
				t.Fatalf("late completion changed terminal status: %+v %v", final, err)
			}
			encoded, err := json.Marshal(status)
			if err != nil || strings.Contains(string(encoded), "private-token") {
				t.Fatalf("secret escaped terminal state: %s %v", encoded, err)
			}
			if _, err := service.SelectLoginProject(flow.FlowID, "project"); err == nil {
				t.Fatal("terminal login allowed another provisioning attempt")
			}
		})
	}
}

func TestProviderCompletionPersistsCredentialsOnlyBeforeCancellation(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "persist")
	defer service.Close()
	auth := inferencenet.Auth{UserEmail: "owner@example.test", ProjectID: "project", ProjectName: "Project", MachineKey: "machine-secret", MachineKeyName: "Host"}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := service.finish(ctx, auth); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled completion=%v", err)
	}
	before, err := inferencenet.LoadAuth()
	if err != nil || before.MachineKey != "" {
		t.Fatalf("cancelled completion persisted credentials: %+v %v", before, err)
	}
	if err := service.finish(t.Context(), inferencenet.Auth{}); err == nil {
		t.Fatal("completion without project credentials accepted")
	}
	if err := service.finish(t.Context(), auth); err != nil {
		t.Fatal(err)
	}
	loaded, err := inferencenet.LoadAuth()
	if err != nil || loaded.MachineKey != auth.MachineKey || loaded.ProjectID != auth.ProjectID {
		t.Fatalf("completion did not persist host credentials: %v", err)
	}
	status, err := service.ProviderStatus(config.InferenceNetProvider)
	if err != nil || !status.Configured || status.Email != auth.UserEmail || status.KeySource != "machine" || status.MachineKeyName != "Host" {
		t.Fatalf("safe account identity=%+v %v", status, err)
	}
	beforeConfig, err := service.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	validationCtx, stopValidation := context.WithCancel(t.Context())
	defer stopValidation()
	service.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		stopValidation()
		return []llm.ModelInfo{{ID: "valid"}}, nil
	}
	if _, err := service.SetProviderKey(validationCtx, ProviderKeySetup{Revision: beforeConfig.Revision, Provider: "openrouter", Key: "replacement-secret"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled key setup=%v", err)
	}
	afterConfig, err := service.ReadConfiguration()
	if err != nil || !reflect.DeepEqual(afterConfig, beforeConfig) {
		t.Fatalf("cancelled key setup changed config: %+v %v", afterConfig, err)
	}
}
