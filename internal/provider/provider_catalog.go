package provider

import (
	"context"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
)

func cacheProviderModels(provider, baseURL string, models []llm.ModelInfo) {
	// Catalogs are a cache; the runtime can refresh a failed write on next use.
	_ = config.UpdateCatalog(provider, config.Catalog{FetchedAt: time.Now(), BaseURL: baseURL, Models: modelInfoLites(models)})
}

// ListCatalogs refreshes the requested catalogs and returns the current host
// configuration after discovery. Request decoding and serialization stay with
// the caller.
func (s *ProviderService) ListCatalogs(ctx context.Context, refresh bool, selected string) (protocol.ProviderCatalogsResult, error) {
	cfg, err := config.Load()
	if err != nil {
		return protocol.ProviderCatalogsResult{}, err
	}
	catalogs := s.CatalogsFor(cfg)
	failures := map[string]string{}
	for name, provider := range cfg.EffectiveProviders() {
		if selected != "" && name != selected {
			continue
		}
		status, err := s.providerStatus(cfg, name)
		if err != nil || (status.AuthState != "unchecked" && (status.Available == nil || !*status.Available)) {
			continue
		}
		if cached, ok := catalogs[name]; !refresh && ok && !cached.NeedsDiscovery() {
			continue
		}
		fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err = s.refreshCatalog(fetchCtx, name, provider)
		cancel()
		if err != nil {
			failures[name] = "Model discovery failed; check the provider connection and retry."
		}
	}
	// Account/config changes may have completed while discovery was in flight.
	cfg, err = config.Load()
	if err != nil {
		return protocol.ProviderCatalogsResult{}, err
	}
	result := protocol.ProviderCatalogsResult{Catalogs: s.CatalogsFor(cfg), Models: map[string]protocol.ModelDescriptor{}, Providers: map[string]protocol.ProviderDescriptor{}, Errors: failures}
	for name, model := range cfg.Models {
		result.Models[name] = protocol.ModelDescriptor{Name: model.Name, ID: model.ID, Providers: model.Providers, Context: model.ContextWindow(), Vision: model.Vision}
		for _, provider := range model.Providers {
			result.Providers[provider] = protocol.ProviderDescriptor{Available: new(false)}
		}
	}
	for name, provider := range cfg.EffectiveProviders() {
		status, err := s.providerStatus(cfg, name)
		available := err == nil && ((status.Available != nil && *status.Available) || status.AuthState == "unchecked")
		descriptor := protocol.ProviderDescriptor{Available: new(available)}
		if available {
			descriptor.BaseURL = provider.BaseURL
		} else {
			delete(result.Catalogs, name)
		}
		result.Providers[name] = descriptor
	}
	return result, nil
}

func modelInfoLites(values []llm.ModelInfo) []config.ModelInfoLite {
	result := make([]config.ModelInfoLite, 0, len(values))
	for _, value := range values {
		var pricing llm.Pricing
		if value.Pricing != nil {
			pricing = *value.Pricing
		}
		result = append(result, config.ModelInfoLite{
			ID: value.ID, ContextLength: value.ContextLength, MaxCompletionTokens: value.MaxCompletionTokens,
			ReasoningEfforts: value.ReasoningEfforts, InputModalities: value.InputModalities,
			Pricing: pricing,
		})
	}
	return result
}
