package daemon

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

var errNoCompatibleProviderModels = errors.New("no compatible chat models were found; refresh the catalog or configure a custom endpoint")

// ModelClient is the common route constructor for runtime and catalog calls.
// Subscription clients borrow the host's single refresh owner.
func (s *ProviderService) ModelClient(provider config.Provider, configs ...*config.Config) (*llm.Client, error) {
	if provider.API == openaiauth.Provider {
		if err := provider.ValidateOpenAICodex(); err != nil {
			return nil, err
		}
		if s == nil || s.openAI == nil {
			return nil, errors.New("OpenAI subscription credentials are unavailable on this execution host")
		}
		return llm.NewSubscription(s.openAI), nil
	}
	var cfg *config.Config
	if len(configs) > 0 {
		cfg = configs[0]
	}
	if cfg == nil {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return nil, err
		}
	}
	key, err := provider.ResolveKey(cfg)
	if err != nil {
		return nil, err
	}
	if key == "" && provider.Auth != "none" {
		return nil, errors.New("no API key for provider")
	}
	return llm.New(provider.BaseURL, key), nil
}

func (s *ProviderService) Catalogs() map[string]config.Catalog {
	cfg, err := config.Load()
	if err != nil {
		return map[string]config.Catalog{}
	}
	return s.CatalogsFor(cfg)
}

// CatalogsFor uses the same configuration snapshot as model resolution.
func (s *ProviderService) CatalogsFor(cfg *config.Config) map[string]config.Catalog {
	return s.catalogsForRoutes(cfg.EffectiveProviders())
}

func (s *ProviderService) catalogsForRoutes(routes map[string]config.Provider) map[string]config.Catalog {
	catalogs := config.LoadCatalogs()
	for name, catalog := range catalogs {
		route, ok := routes[name]
		if !ok || strings.TrimRight(route.BaseURL, "/") != strings.TrimRight(catalog.BaseURL, "/") {
			delete(catalogs, name)
			continue
		}
		if catalog.BaseURL != openaiauth.BaseURL {
			catalogs[name] = config.EnrichPresetCatalog(name, route, catalog)
			continue
		}
		if s == nil || s.openAI == nil {
			delete(catalogs, name)
			continue
		}
		credentials, err := s.openAI.Snapshot()
		if err != nil || credentials.AccountID == "" || credentials.AccountID != catalog.AccountID {
			delete(catalogs, name)
		}
	}
	return catalogs
}

func (s *ProviderService) refreshCatalog(ctx context.Context, name string, provider config.Provider) error {
	if s == nil {
		return errors.New("provider service is unavailable")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	route, ok := cfg.EffectiveProviders()[name]
	sameSubscription := provider.API == openaiauth.Provider && route.ValidateOpenAICodex() == nil
	if !ok || (route != provider && !sameSubscription) {
		return errors.New("provider changed before model discovery")
	}
	client, err := s.ModelClient(provider, cfg)
	if err != nil {
		return err
	}
	var generation uint64
	var accountID string
	if provider.API == openaiauth.Provider {
		s.provisionMu.Lock()
		credentials, authErr := s.openAI.Snapshot()
		generation, accountID = s.openAI.Generation(), credentials.AccountID
		s.provisionMu.Unlock()
		if authErr != nil {
			return authErr
		}
	}
	var models []llm.ModelInfo
	var discovery protocol.ProviderDiscovery
	if provider.API == openaiauth.Provider {
		models, err = client.Models(ctx)
	} else {
		models, discovery, err = s.discoverProviderModels(ctx, name, provider, client.APIKey)
	}
	// An authenticated live response with no compatible models still owns
	// membership. Save the empty result before reporting selection guidance.
	emptyCatalog := errors.Is(err, errNoCompatibleProviderModels)
	if err != nil && !emptyCatalog {
		return err
	}
	catalog := config.Catalog{
		FetchedAt: time.Now(), BaseURL: provider.BaseURL, AccountID: accountID, Models: modelInfoLites(models),
	}
	s.provisionMu.Lock()
	defer s.provisionMu.Unlock()
	if provider.API == openaiauth.Provider {
		if generation != s.openAI.Generation() {
			return openaiauth.ErrLoginChanged
		}
	}
	cfg, err = config.Load()
	if err != nil {
		return err
	}
	route, ok = cfg.EffectiveProviders()[name]
	// Compare only endpoint/auth for subscription login's minimal route value.
	sameSubscription = provider.API == openaiauth.Provider && route.ValidateOpenAICodex() == nil
	if !ok || (route != provider && !sameSubscription) {
		return errors.New("provider changed during model discovery")
	}
	if provider.API != openaiauth.Provider && config.DiscoverCredentials(cfg).KeyStatus(provider).Source != "command" {
		key, err := provider.ResolveKey(cfg)
		if err != nil || key != client.APIKey {
			return errors.New("provider credentials changed during model discovery")
		}
	}
	usingBundled := discovery.Status == "unverified" && strings.HasPrefix(discovery.Message, "Using bundled")
	if usingBundled {
		if cached, ok := s.CatalogsFor(cfg)[name]; ok && len(cached.Models) > 0 {
			return errors.New(discovery.Message)
		}
	}
	if err := config.UpdateCatalog(name, catalog); err != nil {
		return err
	}
	if usingBundled {
		return errors.New(discovery.Message)
	}
	if emptyCatalog {
		return errNoCompatibleProviderModels
	}
	return nil
}

// OpenRouter's public catalog accepts invalid keys. Check its authenticated
// endpoint first; keep custom endpoints on the generic /models contract.
func validateProviderModels(ctx context.Context, baseURL, key string) ([]llm.ModelInfo, error) {
	client := llm.New(baseURL, key)
	if client.BaseURL == config.OpenRouterBaseURL {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.BaseURL+"/key", nil)
		if err != nil {
			return nil, err
		}
		request.Header.Set("Authorization", "Bearer "+key)
		response, err := client.HTTP.Do(request)
		if err != nil {
			return nil, err
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, &llm.HTTPError{Status: response.Status}
		}
	}
	return client.Models(ctx)
}

// Model discovery never issues a completion or proves that inference will work.
// Only exact preset routes may fall back to the reviewed bundled model subset.
func (s *ProviderService) discoverProviderModels(ctx context.Context, id string, provider config.Provider, key string) ([]llm.ModelInfo, protocol.ProviderDiscovery, error) {
	models, err := s.validate(ctx, provider.BaseURL, key)
	result := protocol.ProviderDiscovery{Status: "loaded", Message: "Models loaded; inference has not been tested."}
	_, preset := config.CanonicalProviderPreset(id, provider)
	if err == nil {
		models = config.CompatiblePresetModels(id, provider, models)
		if len(models) == 0 {
			return nil, result, errNoCompatibleProviderModels
		}
		result.ModelCount = len(models)
		if preset && id == "deepinfra" {
			result.Status = "unverified"
			result.Message = "Public model catalog loaded; the API key and inference have not been verified."
		}
		return models, result, nil
	}
	if errors.Is(err, context.Canceled) {
		return nil, result, err
	}
	fallbackAllowed := errors.Is(err, context.DeadlineExceeded)
	if _, ok := errors.AsType[net.Error](err); ok {
		fallbackAllowed = true
	}
	if response, ok := errors.AsType[*llm.HTTPError](err); ok {
		code := strings.Fields(response.Status)
		// Do not bypass an observed authentication, permission, billing or
		// rate-limit failure with a bundled catalog.
		if len(code) > 0 && strings.HasPrefix(code[0], "4") && code[0] != "404" && code[0] != "405" {
			return nil, result, err
		}
		fallbackAllowed = len(code) > 0 && (code[0] == "404" || code[0] == "405" || strings.HasPrefix(code[0], "5"))
	}
	if preset && fallbackAllowed {
		if fallback := config.PresetModels(id); len(fallback) > 0 {
			return fallback, protocol.ProviderDiscovery{
				Status: "unverified", ModelCount: len(fallback),
				Message: "Using bundled models; the connection has not been verified. Refresh the catalog to retry.",
			}, nil
		}
	}
	return nil, result, err
}
