package daemon

import (
	"cmp"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

func providerCanAttempt(status ProviderStatus) bool {
	return !status.Disabled && ((status.Available != nil && *status.Available) || status.AuthState == "unchecked")
}

func (s *ProviderService) providerSelection(cfg *config.Config, catalogs map[string]config.Catalog, model, provider string) protocol.ProviderSelection {
	return s.providerSelectionWithCredentials(cfg, catalogs, model, provider, nil)
}

func (s *ProviderService) providerSelectionWithCredentials(
	cfg *config.Config,
	catalogs map[string]config.Catalog,
	model, provider string,
	credentials *config.CredentialSnapshot,
) protocol.ProviderSelection {
	if model == "" {
		model = cfg.DefaultModel
		provider = cmp.Or(provider, cfg.DefaultProvider)
	}
	selection := protocol.ProviderSelection{Model: model, Provider: provider, Reason: "model_required"}
	alias, configured := cfg.Models[model]
	id := model
	if configured {
		id = cmp.Or(alias.ID, model)
		provider = cmp.Or(provider, cfg.DefaultProvider)
		if provider == "" && len(alias.Providers) > 0 {
			provider = alias.Providers[0]
		}
	} else if provider == "" {
		for name, catalog := range catalogs {
			if catalog.Find(model) != nil {
				if provider != "" {
					selection.Reason = "provider_required"
					return selection // An ambiguous model needs an explicit provider.
				}
				provider = name
			}
		}
	}
	selection.Provider = provider
	if provider == "" {
		selection.Reason = "provider_required"
		return selection
	}
	status, err := s.providerStatusWithCredentials(cfg, provider, credentials)
	if err != nil || !providerCanAttempt(status) {
		selection.Reason = "provider_unavailable"
		return selection
	}
	// Configured aliases remain authoritative on their declared routes, even
	// offline. Cross-provider aliases must match that provider's own catalog.
	if model == "" || (!configured || (len(alias.Providers) > 0 && !slices.Contains(alias.Providers, provider))) && catalogs[provider].Find(id) == nil {
		return selection
	}
	if provider == openaiauth.Provider {
		limit := llm.SubscriptionOutputLimit(id)
		if limit == 0 || (alias.MaxOut > 0 && alias.MaxOut < limit) {
			return selection
		}
	}
	selection.Ready, selection.Reason = true, "ready"
	return selection
}

func (s *ProviderService) suggestedProviderModel(cfg *config.Config, catalogs map[string]config.Catalog, provider string) string {
	return s.suggestedProviderModelWithCredentials(cfg, catalogs, provider, nil)
}

func (s *ProviderService) suggestedProviderModelWithCredentials(
	cfg *config.Config,
	catalogs map[string]config.Catalog,
	provider string,
	credentials *config.CredentialSnapshot,
) string {
	if credentials == nil {
		credentials = config.DiscoverCredentials(cfg)
	}
	current := s.providerSelectionWithCredentials(cfg, catalogs, "", "", credentials)
	if current.Ready && current.Provider == provider {
		return current.Model
	}
	for _, preset := range config.ProviderPresets() {
		if preset.ID != provider {
			continue
		}
		route, ok := credentials.EffectiveProviders(cfg)[provider]
		if !ok || strings.TrimRight(route.BaseURL, "/") != strings.TrimRight(preset.Provider.BaseURL, "/") {
			break
		}
		for _, model := range preset.SuggestedModels {
			if s.providerSelectionWithCredentials(cfg, catalogs, model, provider, credentials).Ready {
				return model
			}
		}
	}
	// A sole explicitly configured model needs no arbitrary catalog ranking.
	var candidate string
	for name, model := range cfg.Models {
		if slices.Contains(model.Providers, provider) && s.providerSelectionWithCredentials(cfg, catalogs, name, provider, credentials).Ready {
			if candidate != "" {
				return ""
			}
			candidate = name
		}
	}
	return candidate
}
