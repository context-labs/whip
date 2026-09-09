package daemon

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
)

// ModelClient is the common route constructor for runtime and catalog calls.
// Subscription clients borrow the host's single refresh owner.
func (s *ProviderService) ModelClient(provider config.Provider) (*llm.Client, error) {
	if provider.API == openaiauth.Provider {
		if err := provider.ValidateOpenAICodex(); err != nil {
			return nil, err
		}
		if s == nil || s.openAI == nil {
			return nil, errors.New("OpenAI subscription credentials are unavailable on this execution host")
		}
		return llm.NewSubscription(s.openAI), nil
	}
	key, err := provider.ResolveKey()
	if err != nil {
		return nil, err
	}
	if key == "" {
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
	catalogs := config.LoadCatalogs()
	routes := cfg.EffectiveProviders()
	for name, catalog := range catalogs {
		route, ok := routes[name]
		if !ok || strings.TrimRight(route.BaseURL, "/") != strings.TrimRight(catalog.BaseURL, "/") {
			delete(catalogs, name)
			continue
		}
		if catalog.BaseURL != openaiauth.BaseURL {
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
	client, err := s.ModelClient(provider)
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
	models, err := client.Models(ctx)
	if err != nil {
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	route, ok := cfg.EffectiveProviders()[name]
	// Compare only endpoint/auth for subscription login's minimal route value.
	sameSubscription := provider.API == openaiauth.Provider && route.ValidateOpenAICodex() == nil
	if !ok || (route != provider && !sameSubscription) {
		return errors.New("provider changed during model discovery")
	}
	if provider.API != openaiauth.Provider && provider.KeyStatus().Source != "command" {
		key, err := provider.ResolveKey()
		if err != nil || key != client.APIKey {
			return errors.New("provider credentials changed during model discovery")
		}
	}
	return config.UpdateCatalog(name, catalog)
}
