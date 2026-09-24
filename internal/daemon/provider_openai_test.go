package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/openaiauth"
)

func openAITestCredentials() openaiauth.Credentials {
	return openaiauth.Credentials{
		AccessToken: "private-access-token", RefreshToken: "private-refresh-token", AccountID: "account-id",
		ExpiresAt: time.Now().Add(time.Hour), Email: "account@example.com", Plan: "pro",
	}
}

func TestOpenAILoginFailureCanRetryAndCatalogFailureKeepsLogin(t *testing.T) {
	for _, failure := range []string{"authorization", "credential storage"} {
		t.Run(failure, func(t *testing.T) {
			t.Setenv("WHIP_HOME", t.TempDir())
			service := NewProviderService(t.Context(), "retry")
			t.Cleanup(service.Close)
			attempts := 0
			service.openAILogin = func(_ context.Context, onCode func(string, string)) (openaiauth.Credentials, error) {
				onCode("https://auth.openai.com/codex/device", "expired-code")
				attempts++
				if attempts == 1 {
					if failure == "authorization" {
						return openaiauth.Credentials{}, errors.New("device authorization expired")
					}
					return openaiauth.Credentials{}, nil
				}
				return openAITestCredentials(), nil
			}
			service.refreshModels = func(context.Context, string, config.Provider) error {
				return errors.New("upstream failed with private-access-token")
			}
			first, err := service.BeginProviderLogin(openaiauth.Provider)
			if err != nil {
				t.Fatal(err)
			}
			failed := waitProviderState(t, service, first.FlowID, "failed")
			if failed.Error == "" || failed.UserCode != "" {
				t.Fatalf("failed authorization retained a code or lost its error: %+v", failed)
			}
			status, err := service.ProviderStatus(openaiauth.Provider)
			if err != nil || status.AuthState != "signed_out" {
				t.Fatalf("failed login became connected: %+v %v", status, err)
			}
			retry, err := service.BeginProviderLogin(openaiauth.Provider)
			if err != nil || retry.FlowID == first.FlowID {
				t.Fatalf("failed flow cannot be retried: %+v %v", retry, err)
			}
			completed := waitProviderState(t, service, retry.FlowID, "succeeded")
			if completed.UserCode != "" || !strings.Contains(completed.Error, "Model discovery failed") || strings.Contains(completed.Error, "private-") {
				t.Fatalf("discovery failure did not preserve a safe completed login: %+v", completed)
			}
			status, err = service.ProviderStatus(openaiauth.Provider)
			if err != nil || status.AuthState != "connected" || status.Available == nil || !*status.Available {
				t.Fatalf("catalog failure invalidated saved login: %+v %v", status, err)
			}
		})
	}
}

func TestOpenAICancelledLogoutPreservesCredentials(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "cancelled-logout")
	t.Cleanup(service.Close)
	if err := service.openAI.Install(t.Context(), service.openAI.Generation(), openAITestCredentials()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := service.LogoutProvider(ctx, openaiauth.Provider); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled logout: %v", err)
	}
	credentials, err := service.openAI.Snapshot()
	if err != nil || credentials.AccessToken != openAITestCredentials().AccessToken {
		t.Fatalf("cancelled logout discarded credentials: %v", err)
	}
}

func TestOpenAILoginPreservesConcurrentConfigurationConflict(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "conflict")
	t.Cleanup(service.Close)
	service.openAILogin = func(context.Context, func(string, string)) (openaiauth.Credentials, error) {
		_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
			cfg.Providers[openaiauth.Provider] = config.Provider{API: "openai-completions", BaseURL: "https://custom.example/v1"}
			return nil
		})
		return openAITestCredentials(), err
	}
	service.refreshModels = func(context.Context, string, config.Provider) error {
		t.Error("discovery ran for a conflicting route")
		return errors.New("discovery must not run for a conflicting route")
	}
	flow, err := service.BeginProviderLogin(openaiauth.Provider)
	if err != nil {
		t.Fatal(err)
	}
	failed := waitProviderState(t, service, flow.FlowID, "failed")
	if !strings.Contains(failed.Error, "configuration could not be saved") || failed.UserCode != "" {
		t.Fatalf("partial setup needs recovery guidance: %+v", failed)
	}
	status, err := service.ProviderStatus(openaiauth.Provider)
	if err != nil || status.AuthState != "configuration_error" || status.Available == nil || *status.Available {
		t.Fatalf("conflicting setup reported ready: %+v %v", status, err)
	}
	configuration, err := config.Load()
	if err != nil || configuration.Providers[openaiauth.Provider].BaseURL != "https://custom.example/v1" {
		t.Fatalf("sign-in overwrote the concurrent route edit: %v", err)
	}
	credentials, err := service.openAI.Snapshot()
	if err != nil || credentials.AccessToken != openAITestCredentials().AccessToken {
		t.Fatalf("partial setup lost completed authorization: %v", err)
	}
}

func TestOpenAILoginRecoveryPersistenceAndLogout(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "first")
	service.refreshModels = func(context.Context, string, config.Provider) error { return nil }
	t.Cleanup(service.Close)
	ready, release := make(chan struct{}), make(chan struct{})
	service.openAILogin = func(ctx context.Context, onCode func(string, string)) (openaiauth.Credentials, error) {
		onCode("https://auth.openai.com/codex/device", "public-code")
		close(ready)
		select {
		case <-ctx.Done():
			return openaiauth.Credentials{}, ctx.Err()
		case <-release:
			return openAITestCredentials(), nil
		}
	}
	first, err := service.BeginProviderLogin(openaiauth.Provider)
	if err != nil {
		t.Fatal(err)
	}
	<-ready
	second, err := service.BeginProviderLogin(openaiauth.Provider)
	if err != nil || first.FlowID != second.FlowID || second.UserCode != "public-code" {
		t.Fatalf("duplicate login was not recovered: %v", err)
	}
	if flows := service.ListLogins(); len(flows.Flows) != 1 || flows.Flows[0].Provider != openaiauth.Provider {
		t.Fatal("lost acknowledgement cannot be recovered")
	}
	close(release)
	completed := waitProviderState(t, service, first.FlowID, "succeeded")
	status, err := service.ProviderStatus(openaiauth.Provider)
	if err != nil || !status.Configured || status.AuthState != "connected" || status.Email != "account@example.com" {
		t.Fatalf("incorrect connected status: %+v %v", status, err)
	}
	for _, value := range []any{completed, status, service.ListLogins()} {
		data, err := json.Marshal(value)
		if err != nil || strings.Contains(string(data), "private-") {
			t.Fatal("credentials reached a public response")
		}
	}
	service.Close()
	restarted := NewProviderService(t.Context(), "second")
	t.Cleanup(restarted.Close)
	credentials, err := restarted.openAI.Credentials(t.Context())
	if err != nil || credentials.AccessToken != "private-access-token" {
		t.Fatalf("saved login did not survive restart: %v", err)
	}
	if old, err := restarted.LoginStatus(first.FlowID); err != nil || old.State != "interrupted" {
		t.Fatalf("old flow did not report interruption: %v", err)
	}
	status, err = restarted.LogoutProvider(t.Context(), openaiauth.Provider)
	if err != nil || status.AuthState != "signed_out" || !status.Configured {
		t.Fatalf("logout changed route or retained login: %+v %v", status, err)
	}
	if _, err := restarted.openAI.Credentials(t.Context()); !errors.Is(err, openaiauth.ErrSignInRequired) {
		t.Fatalf("credentials survived logout: %v", err)
	}
}

func TestOpenAISetupRetryUsesSavedLogin(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "generation")
	service.refreshModels = func(context.Context, string, config.Provider) error { return nil }
	t.Cleanup(service.Close)
	if err := service.openAI.Install(t.Context(), service.openAI.Generation(), openAITestCredentials()); err != nil {
		t.Fatal(err)
	}
	service.openAILogin = func(context.Context, func(string, string)) (openaiauth.Credentials, error) {
		t.Error("requested new OAuth authorization for an already saved login")
		return openaiauth.Credentials{}, errors.New("unexpected login")
	}
	status, err := service.ProviderStatus(openaiauth.Provider)
	if err != nil || status.AuthState != "setup_required" {
		t.Fatalf("partial setup was reported ready: %+v %v", status, err)
	}
	flow, err := service.BeginProviderLogin(openaiauth.Provider)
	if err != nil {
		t.Fatal(err)
	}
	waitProviderState(t, service, flow.FlowID, "succeeded")
}

func TestOpenAILogoutCancelsOnlyOwnFlows(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "generation")
	t.Cleanup(service.Close)
	service.openAILogin = func(ctx context.Context, _ func(string, string)) (openaiauth.Credentials, error) {
		<-ctx.Done()
		return openaiauth.Credentials{}, ctx.Err()
	}
	service.login = func(ctx context.Context, _ func(string, string)) (providerLoginIdentity, error) {
		<-ctx.Done()
		return providerLoginIdentity{teams: []inferencenet.Team{}}, ctx.Err()
	}
	openAI, err := service.BeginProviderLogin(openaiauth.Provider)
	if err != nil {
		t.Fatal(err)
	}
	inference, err := service.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.LogoutProvider(t.Context(), openaiauth.Provider); err != nil {
		t.Fatal(err)
	}
	waitProviderState(t, service, openAI.FlowID, "interrupted")
	status, err := service.LoginStatus(inference.FlowID)
	if err != nil || status.State != "authorizing" {
		t.Fatalf("cancelled unrelated provider: %v", err)
	}
}

func TestOpenAIRejectsConflictingProfileBeforeLogin(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("WHIP_HOME", directory)
	_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers[openaiauth.Provider] = config.Provider{API: "openai-completions", BaseURL: "https://example.com"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewProviderService(t.Context(), "generation")
	t.Cleanup(service.Close)
	if _, err := service.BeginProviderLogin(openaiauth.Provider); err == nil {
		t.Fatal("accepted conflicting route")
	}
	after, err := os.ReadFile(filepath.Join(directory, "config.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("changed existing configuration")
	}
}

func TestOpenAILoginAcrossUnixAndWebSocketWithoutJournalingSecrets(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	fixture := newV2Fixture(t, &fakeRunner{})
	service := fixture.server.providers
	service.refreshModels = func(context.Context, string, config.Provider) error { return nil }
	ready, release := make(chan struct{}), make(chan struct{})
	service.openAILogin = func(ctx context.Context, onCode func(string, string)) (openaiauth.Credentials, error) {
		onCode("https://auth.openai.com/codex/device", "private-device-code")
		close(ready)
		select {
		case <-ctx.Done():
			return openaiauth.Credentials{}, ctx.Err()
		case <-release:
			return openAITestCredentials(), nil
		}
	}
	local := fixture.dial("unix", "local-login")
	flow, err := local.BeginProviderLogin(t.Context(), openaiauth.Provider)
	if err != nil {
		t.Fatal(err)
	}
	<-ready
	_ = local.Close()
	remote := fixture.dial("websocket", "remote-login")
	recovered, err := remote.BeginProviderLogin(t.Context(), openaiauth.Provider)
	if err != nil || recovered.FlowID != flow.FlowID || recovered.UserCode != "private-device-code" {
		t.Fatalf("cross-transport login recovery: %v", err)
	}
	close(release)
	waitProviderState(t, service, flow.FlowID, "succeeded")
	status, err := remote.ProviderStatus(t.Context(), openaiauth.Provider)
	if err != nil || status.AuthState != "connected" {
		t.Fatalf("remote account status: %+v %v", status, err)
	}
	if _, err := remote.RotateProviderKey(t.Context(), openaiauth.Provider); err == nil {
		t.Fatal("subscription incorrectly exposed API-key rotation")
	}
	local = fixture.dial("unix", "local-logout")
	if _, err := local.LogoutProvider(t.Context(), openaiauth.Provider); err != nil {
		t.Fatal(err)
	}
	status, err = remote.ProviderStatus(t.Context(), openaiauth.Provider)
	if err != nil || status.AuthState != "signed_out" {
		t.Fatalf("logout did not propagate to remote client: %+v %v", status, err)
	}
	replay, err := remote.Replay(t.Context(), ReplayParams{RootID: fixture.rootID, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(replay)
	if err != nil || strings.Contains(string(data), "private-") {
		t.Fatal("authentication secrets reached the durable event log")
	}
}

func TestOpenAICatalogIsScopedToConnectedAccount(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	service := NewProviderService(t.Context(), "catalog")
	t.Cleanup(service.Close)
	credentials := openAITestCredentials()
	if err := service.openAI.Install(t.Context(), service.openAI.Generation(), credentials); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	if err := cfg.UpsertOpenAICodex(); err != nil {
		t.Fatal(err)
	}
	cfg.Providers["api"] = config.Provider{BaseURL: "https://example.com", APIKey: "api-fixture-key"}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := config.SaveCatalogs(map[string]config.Catalog{
		openaiauth.Provider: {BaseURL: openaiauth.BaseURL, AccountID: credentials.AccountID},
		"api":               {BaseURL: "https://example.com"},
	}); err != nil {
		t.Fatal(err)
	}
	if len(service.Catalogs()) != 2 {
		t.Fatal("connected account's catalog disappeared")
	}
	credentials.AccountID = "replacement-account"
	if err := service.openAI.Install(t.Context(), service.openAI.Generation(), credentials); err != nil {
		t.Fatal(err)
	}
	if catalogs := service.Catalogs(); len(catalogs) != 1 || catalogs["api"].BaseURL == "" {
		t.Fatal("replacement account could see the previous subscription catalog")
	}
	if _, err := service.LogoutProvider(t.Context(), openaiauth.Provider); err != nil {
		t.Fatal(err)
	}
	if _, exists := config.LoadCatalogs()[openaiauth.Provider]; exists {
		t.Fatal("logout retained the subscription catalog on disk")
	}
}
