package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/providerhost"
	"github.com/context-labs/whip/internal/session"
)

func dispatchProvider(ctx context.Context, service *providerhost.Service, method string, raw json.RawMessage) (any, error) {
	if service == nil {
		return nil, ErrMethod
	}
	switch method {
	case "providers.presets":
		result := protocol.ProviderPresetsResult{Items: []protocol.ProviderPreset{}}
		for _, p := range providerhost.Presets() {
			result.Items = append(result.Items, protocol.ProviderPreset{ID: protocol.ID(p.ID), Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL, Methods: nonNilStrings(p.Methods), Environments: nonNilStrings(p.Environments), KeyURL: p.KeyURL, SuggestedModels: nonNilStrings(p.SuggestedModels), SuggestedEffort: p.SuggestedEffort})
		}
		return result, nil
	case "providers.bundled":
		return decode(raw, func(p protocol.ProviderParams) (any, error) {
			values, err := providerhost.BundledModels(string(p.Provider))
			return protocol.ProviderModelsResult{Items: providerModels(values)}, err
		})
	case "providers.list":
		value, err := service.List(ctx)
		return providerInventory(value), err
	case "providers.create", "providers.update":
		return decode(raw, func(p protocol.ChangeProviderParams) (any, error) {
			if p.KeepCredential && p.Declaration.Credential != nil {
				return nil, providerhost.ErrInvalid
			}
			change := providerhost.Change{Revision: p.Revision, ID: string(p.Provider), Provider: providerDeclaration(p.Declaration), KeepCredential: p.KeepCredential}
			if p.Key != nil {
				change.Key = &providerhost.KeyPublication{ID: string(p.Key.ID), Key: p.Key.Key}
			}
			var value providerhost.Inventory
			var err error
			if method == "providers.create" {
				value, err = service.Create(ctx, change)
			} else {
				value, err = service.Update(ctx, change)
			}
			return providerInventory(value), err
		})
	case "providers.remove":
		return decode(raw, func(p protocol.RemoveProviderParams) (any, error) {
			var replacement *providerhost.Defaults
			if p.Replacement != nil {
				value := providerDefaults(*p.Replacement)
				replacement = &value
			}
			value, err := service.Remove(ctx, p.Revision, string(p.Provider), replacement)
			return providerInventory(value), err
		})
	case "providers.defaults", "providers.compaction":
		return decode(raw, func(p protocol.ProviderDefaultsParams) (any, error) {
			var value providerhost.Inventory
			var err error
			if method == "providers.defaults" {
				value, err = service.SetDefaults(ctx, p.Revision, providerDefaults(p.Defaults))
			} else {
				value, err = service.SetCompactionModel(ctx, p.Revision, providerDefaults(p.Defaults))
			}
			return providerInventory(value), err
		})
	case "providers.catalog", "providers.refresh":
		return decode(raw, func(p protocol.ProviderParams) (any, error) {
			var value providerhost.Catalog
			var err error
			if method == "providers.catalog" {
				value, err = service.Catalog(ctx, string(p.Provider))
			} else {
				value, err = service.Refresh(ctx, string(p.Provider))
			}
			// Accepted discovery can fail while retaining useful same-scope cache.
			// Return that explicitly failed observation, never claim verification.
			if errors.Is(err, providerhost.ErrDiscovery) && value.Provider != "" {
				err = nil
			}
			return providerCatalog(value), err
		})
	case "providers.readiness":
		return decode(raw, func(p protocol.ProviderReadinessParams) (any, error) {
			selection := providerSelection(p.Selection)
			if err := selection.Validate(); err != nil {
				return nil, providerhost.ErrInvalid
			}
			value, err := service.Readiness(ctx, selection)
			return protocol.ProviderReadiness{Configured: value.Configured, CredentialState: value.CredentialState, CatalogState: value.CatalogState, ModelState: value.ModelState, InferenceState: value.InferenceState}, err
		})
	default:
		return nil, ErrMethod
	}
}

func nonNilStrings(values []string) []string { return append([]string{}, values...) }

func providerInventory(value providerhost.Inventory) protocol.ProviderInventory {
	result := protocol.ProviderInventory{Revision: value.Revision, Routes: []protocol.ProviderRoute{}}
	if value.Defaults.Name != "" {
		selected := selectionProjection(value.Defaults)
		result.Defaults = &selected
	}
	if value.CompactionModel != nil {
		selected := selectionProjection(*value.CompactionModel)
		result.CompactionModel = &selected
	}
	for _, route := range value.Routes {
		models := map[string]protocol.ProviderModelSettings{}
		for id, settings := range route.Models {
			models[id] = settingsProjection(settings)
		}
		result.Routes = append(result.Routes, protocol.ProviderRoute{ID: protocol.ID(route.ID), Kind: route.Kind, BaseURL: route.BaseURL, Credential: protocol.ProviderCredentialStatus{Source: route.Credential.Source, State: route.Credential.State, Environment: route.Credential.Environment, File: route.Credential.File}, Models: models})
	}
	return result
}

func providerCatalog(value providerhost.Catalog) protocol.ProviderCatalog {
	result := protocol.ProviderCatalog{Provider: protocol.ID(value.Provider), State: value.State, ScopeState: value.ScopeState, Discovery: value.Discovery, Stale: value.Stale, Failure: optionalText(value.Failure), Models: providerModels(value.Models)}
	if value.FetchedAt != nil {
		result.FetchedAt = accountTime(*value.FetchedAt)
	}
	return result
}

func providerModels(values []providerhost.Model) []protocol.ProviderModel {
	result := make([]protocol.ProviderModel, 0, len(values))
	for _, value := range values {
		result = append(result, protocol.ProviderModel{ID: value.ID, Name: value.Name, Prices: pricesProjection(value.Prices), ContextWindowTokens: providerCounter(value.ContextWindowTokens), AdvertisedContextTokens: providerCounter(value.AdvertisedContextTokens), EffectiveContextPercent: providerCounter(value.EffectiveContextPercent), MaxOutputTokens: providerCounter(value.MaxOutputTokens), ReasoningEfforts: slices.Clone(value.ReasoningEfforts), InputModalities: slices.Clone(value.InputModalities), OutputModalities: slices.Clone(value.OutputModalities), SupportsTools: value.SupportsTools, MetadataSource: value.MetadataSource})
	}
	return result
}

func providerCounter(value *int64) *protocol.Counter {
	if value == nil {
		return nil
	}
	return new(protocol.Counter(*value))
}

func providerInt(value *protocol.Counter) *int64 {
	if value == nil {
		return nil
	}
	return new(int64(*value))
}

func pricesProjection(p session.ModelPrices) protocol.ModelPrices {
	return protocol.ModelPrices{Input: providerCounter(p.Input), Output: providerCounter(p.Output), Reasoning: providerCounter(p.Reasoning), CachedInput: providerCounter(p.CachedInput), CachedOutput: providerCounter(p.CachedOutput)}
}

func providerPrices(p protocol.ModelPrices) session.ModelPrices {
	return session.ModelPrices{Input: providerInt(p.Input), Output: providerInt(p.Output), Reasoning: providerInt(p.Reasoning), CachedInput: providerInt(p.CachedInput), CachedOutput: providerInt(p.CachedOutput)}
}

func settingsProjection(p config.Model) protocol.ProviderModelSettings {
	return protocol.ProviderModelSettings{Prices: pricesProjection(p.Prices), ContextWindowTokens: providerCounter(p.ContextWindowTokens), MaxOutputTokens: protocol.Counter(p.MaxOutputTokens), TimeoutMillis: protocol.Counter(p.TimeoutMillis), MaxAttempts: p.MaxAttempts}
}

func providerSettings(p protocol.ProviderModelSettings) config.Model {
	return config.Model{Prices: providerPrices(p.Prices), ContextWindowTokens: providerInt(p.ContextWindowTokens), MaxOutputTokens: int64(p.MaxOutputTokens), TimeoutMillis: int64(p.TimeoutMillis), MaxAttempts: p.MaxAttempts}
}

func providerSelection(p protocol.ModelSelection) session.ModelSelection {
	return session.ModelSelection{Provider: string(p.Provider), Name: p.Name, Effort: p.Effort, Temperature: p.Temperature, TopP: p.TopP}.Clone()
}

func selectionProjection(p session.ModelSelection) protocol.ModelSelection {
	p = p.Clone()
	return protocol.ModelSelection{Provider: protocol.ID(p.Provider), Name: p.Name, Effort: p.Effort, Temperature: p.Temperature, TopP: p.TopP}
}

func providerDeclaration(p protocol.ProviderDeclaration) config.Provider {
	value := config.Provider{Kind: p.Kind, BaseURL: p.BaseURL, Models: map[string]config.Model{}}
	for id, settings := range p.Models {
		value.Models[id] = providerSettings(settings)
	}
	if c := p.Credential; c != nil {
		value.CredentialSource, value.CredentialEnv, value.CredentialFile = c.Source, c.Environment, c.File
		if c.Command != nil {
			value.CredentialCommand = &config.CredentialCommand{Executable: c.Command.Executable, Arguments: slices.Clone(c.Command.Arguments), Environment: slices.Clone(c.Command.Environment)}
		}
	}
	return value
}

func providerDefaults(p protocol.ProviderDefaults) providerhost.Defaults {
	value := providerhost.Defaults{}
	if p.Selection != nil {
		selection := providerSelection(*p.Selection)
		value.Selection = &selection
	}
	if p.Settings != nil {
		settings := providerSettings(*p.Settings)
		value.Settings = &settings
	}
	return value
}
