package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
)

func customProviderService(t *testing.T) *ProviderService {
	t.Helper()
	t.Setenv("WHIP_HOME", t.TempDir())
	s := NewProviderService(t.Context(), "provider-configuration")
	t.Cleanup(s.Close)
	return s
}

func customProviderParams(t *testing.T, s *ProviderService) protocol.ProviderCreateParams {
	t.Helper()
	cfg, err := s.ReadConfiguration()
	if err != nil {
		t.Fatal(err)
	}
	return protocol.ProviderCreateParams{
		Revision: cfg.Revision, Provider: "custom-test",
		Definition: protocol.ProviderDefinition{Name: "My endpoint", BaseURL: "https://example.test/v1", API: "openai-completions"},
		Credential: protocol.ProviderCredential{Mode: "api_key", Key: "private-fixture-key"},
	}
}

func TestProviderConfigurationCreatePersistAndNoAuthRequests(t *testing.T) {
	s := customProviderService(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "" {
			t.Error("unauthenticated discovery sent an Authorization header")
		}
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"fixture-model"}]}`))
	}))
	defer server.Close()
	p := customProviderParams(t, s)
	p.Definition.BaseURL, p.Credential = server.URL+"/v1/", protocol.ProviderCredential{Mode: "none"}
	before, _ := config.Load()
	created, err := s.CreateProvider(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 1 || created.Discovery.Status != "loaded" || created.Credential.Mode != "none" || created.Credential.Available == nil || !*created.Credential.Available {
		t.Fatalf("incorrect discovery/readiness: %+v, requests %d", created, requests)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers[p.Provider].Auth != "none" || cfg.DefaultModel != before.DefaultModel || cfg.DefaultProvider != before.DefaultProvider || !reflect.DeepEqual(cfg.Models, before.Models) {
		t.Fatal("provider save did not preserve authentication/defaults/models")
	}
	restarted := NewProviderService(t.Context(), "new-generation")
	defer restarted.Close()
	read, err := restarted.ReadProvider(p.Provider)
	if err != nil || read.Revision != created.Revision || read.Credential.Mode != "none" {
		t.Fatalf("restart: %+v %v", read, err)
	}
	client, err := restarted.ModelClient(cfg.Providers[p.Provider])
	if err != nil || client.APIKey != "" {
		t.Fatalf("no-auth model client: %v", err)
	}
	if _, err := s.CreateProvider(t.Context(), p); !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatalf("stale create: %v", err)
	}
	p.Revision = read.Revision
	if _, err := s.CreateProvider(t.Context(), p); err == nil {
		t.Fatal("duplicate provider ID accepted")
	}
}

func TestProviderConfigurationRedactedReadAndMetadataUpdate(t *testing.T) {
	s := customProviderService(t)
	marker := filepath.Join(t.TempDir(), "must-not-execute")
	_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers["custom-test"] = config.Provider{Name: "Original", BaseURL: "https://example.test/v1", APIKey: "!touch " + marker}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		t.Fatal("metadata-only save performed discovery")
		return nil, nil
	}
	read, err := s.ReadProvider("custom-test")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(read)
	if strings.Contains(string(body), marker) || strings.Contains(string(body), "touch") || read.Credential.Mode != "command" {
		t.Fatalf("secret reference exposed: %s", body)
	}
	updated, err := s.UpdateProvider(t.Context(), protocol.ProviderUpdateParams{
		Revision: read.Revision, Provider: read.Provider, Name: new("Renamed"),
		ManualModel: &protocol.ProviderManualModel{Alias: "custom-test/private-model", ID: "private-model", Context: 4096, MaxOutput: 1024},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Discovery.Status != "not_checked" || len(updated.Models) != 1 || updated.Models[0].ID != "private-model" {
		t.Fatalf("incorrect alias save: %+v", updated)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("read or metadata-only update executed the secret command")
	}
	cfg, _ := config.Load()
	if cfg.Providers[read.Provider].APIKey != "!touch "+marker {
		t.Fatal("name edit replaced the existing credential")
	}
	for _, credential := range []*protocol.ProviderCredential{nil, {Mode: "keep"}} {
		_, err := s.UpdateProvider(t.Context(), protocol.ProviderUpdateParams{Revision: updated.Revision, Provider: read.Provider, BaseURL: new("https://other.test/v1"), Credential: credential})
		if err == nil || !strings.Contains(err.Error(), "explicit credential choice") {
			t.Fatalf("endpoint edit forwarded old credential: %v", err)
		}
	}
}

func TestProviderConfigurationUnverifiedAndAuthenticationFailures(t *testing.T) {
	for _, outcome := range []string{"empty", "404", "transport", "401", "403", "missing environment"} {
		t.Run(outcome, func(t *testing.T) {
			s := customProviderService(t)
			s.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
				switch outcome {
				case "empty":
					return nil, nil
				case "transport":
					return nil, errors.New("private-fixture-key inside URL error")
				case "missing environment":
					t.Fatal("attempted request with absent environment credential")
				}
				return nil, &llm.HTTPError{Status: outcome + " failure", Body: "private-fixture-key echoed by upstream"}
			}
			p := customProviderParams(t, s)
			if outcome == "missing environment" {
				t.Setenv("WHIP_MISSING_PROVIDER_TEST_KEY", "")
				p.Credential = protocol.ProviderCredential{Mode: "environment", EnvironmentVariable: "WHIP_MISSING_PROVIDER_TEST_KEY"}
			}
			if _, err := s.CreateProvider(t.Context(), p); err == nil || strings.Contains(err.Error(), "private-fixture-key") {
				t.Fatalf("normal save should fail safely: %v", err)
			}
			p.AllowUnverified = true
			p.ManualModel = &protocol.ProviderManualModel{Alias: "custom-test/exact-model", ID: "exact-model"}
			result, err := s.CreateProvider(t.Context(), p)
			if outcome == "401" || outcome == "403" {
				if err == nil {
					t.Fatal("explicit save bypassed a known authentication rejection")
				}
				if _, readErr := s.ReadProvider(p.Provider); readErr == nil {
					t.Fatal("rejected credentials were persisted")
				}
				return
			}
			if err != nil || result.Discovery.Status != "unverified" || len(result.Models) != 1 {
				t.Fatalf("unverified save: %+v %v", result, err)
			}
			if outcome == "missing environment" && (result.Credential.Available == nil || *result.Credential.Available) {
				t.Fatal("missing environment credential is ready")
			}
		})
	}
}

func TestProviderConfigurationValidationAndRemoval(t *testing.T) {
	s := customProviderService(t)
	s.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) { return []llm.ModelInfo{{ID: "m"}}, nil }
	p := customProviderParams(t, s)
	for _, id := range []string{"inference-net", "inference", "openrouter", "openai-codex", "../escape", "Invalid ID"} {
		p.Provider = id
		if _, err := s.CreateProvider(t.Context(), p); err == nil {
			t.Fatalf("accepted ID %q", id)
		}
	}
	p.Provider = "custom-test"
	for _, endpoint := range []string{"https://key:secret@example.test/v1", "https://example.test/v1?secret=value", "https://example.test/v1#fragment", "file:///tmp/model", "https://example.test/v1/chat/completions"} {
		p.Definition.BaseURL = endpoint
		if _, err := s.CreateProvider(t.Context(), p); err == nil {
			t.Fatalf("accepted endpoint %q", endpoint)
		}
	}
	p.Definition.BaseURL = "https://example.test/v1"
	for _, key := range []string{"!touch /tmp/whip-test-must-not-execute", "$WHIP_KEY", "${WHIP_KEY}"} {
		p.Credential.Key = key
		if _, err := s.CreateProvider(t.Context(), p); err == nil {
			t.Fatalf("accepted reference as a literal key %q", key)
		}
	}
	p.Credential.Key = "private-fixture-key"
	result, err := s.CreateProvider(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RemoveProvider(t.Context(), protocol.ProviderRemoveParams{Provider: "inference-net", Revision: result.Revision})
	if err == nil {
		t.Fatal("removed a built-in provider")
	}
	_, err = s.RemoveProvider(t.Context(), protocol.ProviderRemoveParams{Provider: p.Provider, Revision: p.Revision})
	if !errors.Is(err, config.ErrRevisionConflict) {
		t.Fatalf("stale removal: %v", err)
	}
	removed, err := s.RemoveProvider(t.Context(), protocol.ProviderRemoveParams{Provider: p.Provider, Revision: result.Revision})
	if err != nil || removed.Revision == result.Revision {
		t.Fatalf("remove: %+v %v", removed, err)
	}
	if _, err := s.ReadProvider(p.Provider); err == nil {
		t.Fatal("removed provider remains")
	}
	if _, exists := config.LoadCatalogs()[p.Provider]; exists {
		t.Fatal("removed provider's catalog remains")
	}
}

func TestProviderConfigurationConcurrentEditAndCancellation(t *testing.T) {
	for _, action := range []string{"edit", "cancel"} {
		t.Run(action, func(t *testing.T) {
			s := customProviderService(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
				if action == "cancel" {
					cancel()
				} else {
					_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error { cfg.Theme = "light"; return nil })
					if err != nil {
						t.Fatal(err)
					}
				}
				return []llm.ModelInfo{{ID: "fixture"}}, nil
			}
			p := customProviderParams(t, s)
			_, err := s.CreateProvider(ctx, p)
			if action == "edit" && !errors.Is(err, config.ErrRevisionConflict) || action == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatalf("unexpected failure: %v", err)
			}
			cfg, _ := config.Load()
			if _, exists := cfg.Providers[p.Provider]; exists {
				t.Fatal("interrupted create committed configuration")
			}
			if action == "edit" && cfg.Theme != "light" {
				t.Fatal("concurrent config change lost")
			}
		})
	}
}

func TestProviderConfigurationManualAliasOwnershipAndRemovalBlockers(t *testing.T) {
	s := customProviderService(t)
	s.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		return []llm.ModelInfo{{ID: "fixture"}}, nil
	}
	p := customProviderParams(t, s)
	p.ManualModel = &protocol.ProviderManualModel{Alias: "kimi-k3", ID: "other-model"}
	if _, err := s.CreateProvider(t.Context(), p); err == nil {
		t.Fatal("create overwrote a configured model alias")
	}
	p.ManualModel = &protocol.ProviderManualModel{Alias: "custom-test/fixture", ID: "fixture", Context: 8192, MaxOutput: 2048}
	created, err := s.CreateProvider(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(created.RemovalBlockers) != 1 || !strings.Contains(created.RemovalBlockers[0], "custom-test/fixture") {
		t.Fatalf("missing alias blocker: %+v", created.RemovalBlockers)
	}
	if _, err := s.RemoveProvider(t.Context(), protocol.ProviderRemoveParams{Provider: p.Provider, Revision: created.Revision}); err == nil {
		t.Fatal("removed provider referenced by alias")
	}
	updated, err := s.UpdateProvider(t.Context(), protocol.ProviderUpdateParams{Provider: p.Provider, Revision: created.Revision, ManualModel: &protocol.ProviderManualModel{Alias: p.ManualModel.Alias, ID: "fixture", Context: 16384, MaxOutput: 4096}})
	if err != nil || len(updated.Models) != 1 || updated.Models[0].Context != 16384 {
		t.Fatalf("model limit update: %+v %v", updated, err)
	}
	_, err = s.UpdateProvider(t.Context(), protocol.ProviderUpdateParams{Provider: p.Provider, Revision: updated.Revision, ManualModel: &protocol.ProviderManualModel{Alias: p.ManualModel.Alias, ID: "different-model"}})
	if err == nil {
		t.Fatal("update replaced an alias with a different model")
	}
	_, _, err = config.UpdateVersioned(updated.Revision, func(cfg *config.Config) error {
		cfg.DefaultProvider, cfg.CompactProvider = p.Provider, p.Provider
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	read, _ := s.ReadProvider(p.Provider)
	if len(read.RemovalBlockers) != 3 {
		t.Fatalf("default/compaction references missing: %v", read.RemovalBlockers)
	}
}

func TestProviderConfigurationBuiltinsAndCredentialReplacement(t *testing.T) {
	s := customProviderService(t)
	t.Setenv("OPENROUTER_API_KEY", "private-environment-key")
	read, err := s.ReadProvider("openrouter")
	if err != nil || read.Custom || read.Credential.Mode != "environment" || read.Definition.BaseURL != config.OpenRouterBaseURL {
		t.Fatalf("builtin read: %+v %v", read, err)
	}
	s.validate = func(ctx context.Context, endpoint, key string) ([]llm.ModelInfo, error) {
		if endpoint != config.OpenRouterBaseURL || key != "private-new-key" {
			t.Fatal("unexpected built-in route or credential")
		}
		return []llm.ModelInfo{{ID: "fixture", SupportsTools: new(true), OutputModalities: []string{"text"}}}, nil
	}
	updated, err := s.UpdateProvider(t.Context(), protocol.ProviderUpdateParams{Provider: "openrouter", Revision: read.Revision, Credential: &protocol.ProviderCredential{Mode: "api_key", Key: "private-new-key"}})
	if err != nil || updated.Credential.Mode != "api_key" {
		t.Fatalf("builtin edit: %+v %v", updated, err)
	}
	body, _ := json.Marshal(updated)
	if strings.Contains(string(body), "private-") {
		t.Fatal("save response exposed a credential")
	}
	cfg, _ := config.Load()
	if cfg.Providers["openrouter"].APIKeyEnv != "" || cfg.Providers["openrouter"].APIKey != "private-new-key" {
		t.Fatal("replacement left an environment fallback")
	}
	_, err = s.UpdateProvider(t.Context(), protocol.ProviderUpdateParams{Provider: "openrouter", Revision: updated.Revision, BaseURL: new("https://other.test/v1"), Credential: &protocol.ProviderCredential{Mode: "none"}})
	if err == nil {
		t.Fatal("edited a built-in endpoint")
	}
}

func TestProviderConfigurationLastProviderRemovalPreservesRecoveryGuard(t *testing.T) {
	s := customProviderService(t)
	_, revision, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers = map[string]config.Provider{"only": {Name: "Only provider", BaseURL: "http://localhost:1234/v1", Auth: "none"}}
		cfg.Models = map[string]config.Model{}
		cfg.DefaultModel, cfg.DefaultProvider, cfg.CompactModel, cfg.CompactProvider = "", "", "", ""
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.RemoveProvider(t.Context(), protocol.ProviderRemoveParams{Provider: "only", Revision: revision})
	if err == nil || !strings.Contains(err.Error(), "Add another connection") {
		t.Fatalf("missing last-provider guidance: %v", err)
	}
	cfg, after, err := config.ReadVersioned()
	if err != nil || after != revision || len(cfg.Providers) != 1 {
		t.Fatal("last-provider removal changed protected configuration")
	}
}

func TestProviderConfigurationHostRPCAndSecretContracts(t *testing.T) {
	s := customProviderService(t)
	s.validate = func(context.Context, string, string) ([]llm.ModelInfo, error) {
		return []llm.ModelInfo{{ID: "fixture"}}, nil
	}
	server := &Server{providers: s}
	connection := &serverConn{ctx: t.Context()}
	p := customProviderParams(t, s)
	body, _ := json.Marshal(p)
	result, rpcErr, handled := server.handleProvider(connection, rpcMessage{Method: "provider.create", Params: body})
	if rpcErr != nil || !handled {
		t.Fatalf("host-only create: %v, handled %v", rpcErr, handled)
	}
	created, ok := result.(protocol.ProviderConfiguration)
	if !ok || created.Provider != p.Provider {
		t.Fatalf("unexpected create result: %T", result)
	}
	for _, method := range []string{"provider.create", "provider.update"} {
		operation, ok := protocol.Lookup(method)
		if !ok || !operation.Sensitive || operation.Execution != protocol.Ephemeral {
			t.Fatalf("secret-bearing method can enter history: %+v", operation)
		}
	}
	body, _ = json.Marshal(protocol.ProviderNameParams{Provider: p.Provider})
	result, rpcErr, handled = server.handleProvider(connection, rpcMessage{Method: "provider.get", Params: body})
	if rpcErr != nil || !handled {
		t.Fatalf("host-only get: %v", rpcErr)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), p.Credential.Key) {
		t.Fatal("host get returned raw credentials")
	}
	_, rpcErr, _ = server.handleProvider(connection, rpcMessage{Method: "provider.update", Params: json.RawMessage(`{"provider":"custom-test","revision":"old-revision","credential":{"mode":"api_key","key":"private-key"}}`)})
	if rpcErr == nil || rpcErr.Code != -32009 {
		t.Fatalf("missing revision conflict contract: %+v", rpcErr)
	}
	_, rpcErr, _ = server.handleProvider(connection, rpcMessage{Method: "provider.update", Params: json.RawMessage(`{"provider":"custom-test","unexpected":"secret"}`)})
	if rpcErr == nil || strings.Contains(rpcErr.Message, "secret") {
		t.Fatalf("invalid request was not safely rejected: %+v", rpcErr)
	}
}

func TestProviderManualModelClearsLegacyLimitAndPreservesOtherFields(t *testing.T) {
	cfg := &config.Config{Models: map[string]config.Model{
		"alias":  {Name: "Human name", ID: "model", Providers: []string{"custom"}, Context: 4096, MaxTokens: 4096, MaxOut: 1024, Vision: true},
		"shared": {ID: "model", Providers: []string{"custom", "other"}, Context: 4096},
	}}
	if err := applyManualProviderModel(cfg, "custom", &protocol.ProviderManualModel{Alias: "alias", ID: "model"}, false); err != nil {
		t.Fatal(err)
	}
	model := cfg.Models["alias"]
	if model.Context != 0 || model.MaxTokens != 0 || model.MaxOut != 0 || model.Name != "Human name" || !model.Vision {
		t.Fatalf("model metadata was lost or legacy limit survived: %+v", model)
	}
	if err := applyManualProviderModel(cfg, "custom", &protocol.ProviderManualModel{Alias: "shared", ID: "model", Context: 8192}, false); err == nil || cfg.Models["shared"].Context != 4096 {
		t.Fatal("manual model update edited a shared alias")
	}
}

func TestProviderConfigurationRedactsInvalidFileAuthoredEndpoint(t *testing.T) {
	s := customProviderService(t)
	_, _, err := config.UpdateVersioned("", func(cfg *config.Config) error {
		cfg.Providers["unsafe-old-route"] = config.Provider{Name: "Old connection", BaseURL: "https://user:private-userinfo@example.test/v1?key=private-query#private-fragment", APIKey: "private-literal-key"}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	read, err := s.ReadProvider("unsafe-old-route")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(read)
	if strings.Contains(string(encoded), "private-") || read.Definition.BaseURL != "https://example.test/v1" || read.Credential.Available == nil || *read.Credential.Available {
		t.Fatalf("unsafe editable metadata: %s", encoded)
	}
}
