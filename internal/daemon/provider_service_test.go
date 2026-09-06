package daemon

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
)

func waitProviderState(t *testing.T, service *ProviderService, id, state string) ProviderLoginStatus {
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
	return ProviderLoginStatus{}
}

func TestProviderLoginReconnectChoicesAndSecretIsolation(t *testing.T) {
	s := NewProviderService(t.Context(), "generation-a")
	defer s.Close()
	s.login = func(ctx context.Context, code func(string, string)) (providerLoginIdentity, error) {
		code("https://example.com/verify", "public-code")
		return providerLoginIdentity{token: "secret-session-token", email: "person@example.com", teams: []inferencenet.Team{{ID: "team", Name: "Team"}}}, nil
	}
	s.projects = func(ctx context.Context, token string, team inferencenet.Team) ([]inferencenet.Project, error) {
		if token != "secret-session-token" || team.ID != "team" {
			return nil, errors.New("bad selection")
		}
		return []inferencenet.Project{{ID: "project", Name: "Project"}}, nil
	}
	finished := make(chan inferencenet.Auth, 1)
	s.finish = func(ctx context.Context, auth inferencenet.Auth) error { finished <- auth; return nil }
	started, err := s.BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	status := waitProviderState(t, s, started.FlowID, "choose_team")
	if status.VerificationURL == "" || len(status.Teams) != 1 {
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
	if auth.SessionToken != "secret-session-token" || auth.ProjectID != "project" {
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
	t.Setenv("WHIP_HOME", t.TempDir())
	s := NewProviderService(t.Context(), "generation")
	defer s.Close()
	s.validate = func(ctx context.Context, url, key string) ([]llm.ModelInfo, error) {
		if key != "secret-key" {
			return nil, errors.New("invalid key")
		}
		return []llm.ModelInfo{}, nil
	}
	before, err := s.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.SetProviderKey(t.Context(), ProviderKeySetup{Revision: before.Revision, Provider: "openrouter", Key: "secret-key"})
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision == before.Revision {
		t.Fatal("revision did not change")
	}
	if _, err := s.SetProviderKey(t.Context(), ProviderKeySetup{Revision: before.Revision, Provider: "openrouter", Key: "secret-key"}); !errors.Is(err, config.ErrRevisionConflict) {
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
	for range 16 {
		if _, err := s.BeginLogin(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.BeginLogin(); err == nil {
		t.Fatal("active login bound was not enforced")
	}
	result := s.ListLogins()
	if len(result.Flows) != 16 {
		t.Fatal("lost begin acknowledgements cannot be recovered")
	}
	if _, err := s.CancelLogin(result.Flows[0].FlowID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginLogin(); err != nil {
		t.Fatal("cancelled login did not release capacity")
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
