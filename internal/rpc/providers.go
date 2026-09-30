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

func dispatchProvider(ctx context.Context, host HostServices, method string, raw json.RawMessage) (any, error) {
	service := host.ProviderHost
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
	case "providers.candidates":
		value, err := service.Candidates(ctx)
		result := protocol.ProviderCandidates{Revision: value.Revision, Items: []protocol.ProviderCandidate{}}
		for _, candidate := range value.Items {
			result.Items = append(result.Items, protocol.ProviderCandidate{Provider: protocol.ID(candidate.Provider), Source: candidate.Source, CredentialState: candidate.CredentialState})
		}
		return result, err
	case "providers.use_candidate":
		return decode(raw, func(p protocol.UseProviderCandidateParams) (any, error) {
			value, err := service.UseCandidate(ctx, p.Revision, providerhost.Candidate{Provider: string(p.Provider), Source: p.Source})
			return providerInventory(value), err
		})
	case "providers.set_enabled":
		return decode(raw, func(p protocol.SetProviderEnabledParams) (any, error) {
			value, err := service.SetEnabled(ctx, p.Revision, string(p.Provider), p.Enabled)
			return providerInventory(value), err
		})
	case "providers.list":
		value, err := service.List(ctx)
		return providerInventory(value), err
	case "providers.setup_key":
		return decode(raw, func(p protocol.ProviderKeySetup) (any, error) {
			var key *providerhost.KeyPublication
			if p.Key != nil {
				key = &providerhost.KeyPublication{ID: string(p.Key.ID), Key: p.Key.Key}
			}
			value, err := service.SetupKey(ctx, p.Revision, p.Provider, key, p.Environment)
			return providerInventory(value), err
		})
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
	case "providers.disconnect":
		return decode(raw, func(p protocol.DisconnectProviderParams) (any, error) {
			if host.Config == nil {
				return nil, ErrMethod
			}
			snapshot, err := host.Config.Snapshot(ctx)
			if err != nil {
				return nil, providerhost.ErrStorage
			}
			if snapshot.Revision != p.Revision {
				return nil, config.ErrRevisionConflict
			}
			route, exists := snapshot.Host.Providers[string(p.Provider)]
			if !exists {
				return nil, providerhost.ErrMissing
			}
			var value config.ProviderDisconnect
			var cleanupFailure string
			switch {
			case route.Kind == "openai-codex":
				if host.OpenAI == nil {
					return nil, ErrMethod
				}
				_, err = host.OpenAI.LogoutGuarded(ctx, func(clear func() error) error {
					var guardErr error
					value, guardErr = host.Config.DisconnectProvider(ctx, p.Revision, string(p.Provider), "openai-codex", clear)
					return guardErr
				})
			case route.CredentialSource == "inference-net":
				if host.Inference == nil {
					return nil, ErrMethod
				}
				status, logoutErr := host.Inference.LogoutGuarded(ctx, func(clear func() error) error {
					var guardErr error
					value, guardErr = host.Config.DisconnectProvider(ctx, p.Revision, string(p.Provider), "inference-net", clear)
					return guardErr
				})
				err, cleanupFailure = logoutErr, status.CleanupFailure
			default:
				value, err = host.Config.DisconnectProvider(ctx, p.Revision, string(p.Provider), "", nil)
			}
			if err != nil {
				return nil, err
			}
			if value.CredentialState != "preserved_external" {
				service.ClearDiscovery(string(p.Provider))
			}
			inventory, err := service.List(ctx)
			return protocol.ProviderDisconnectResult{Inventory: providerInventory(inventory), CredentialState: value.CredentialState, LocalFailure: optionalText(value.LocalFailure), CleanupFailure: optionalText(cleanupFailure)}, err
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
	case "providers.set_preferences":
		return decode(raw, func(p protocol.ProviderPreferencesParams) (any, error) {
			value, err := service.SetPreferences(ctx, p.Revision, providerDefaults(p.Defaults), session.PermissionMode(p.PermissionMode))
			return providerInventory(value), err
		})
	case "host.set_execution_preferences":
		return decode(raw, func(p protocol.SetExecutionPreferencesParams) (any, error) {
			d := p.Preferences
			value, err := service.SetExecutionPreferences(ctx, p.ExpectedRevision, providerhost.ExecutionPreferences{
				Engine: session.Engine(d.Engine), CompactionPercent: d.CompactionPercent, CompactionModel: providerDefaults(d.CompactionModel),
				GoalMaxContinuations: providerInt(d.GoalMaxContinuations), MaxAttempts: d.MaxAttempts, ImportClaude: d.ImportClaude, ImportCodex: d.ImportCodex,
			})
			return executionDefaults(value), err
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
			return protocol.ProviderReadiness{Configured: value.Configured, Disabled: value.Disabled, CredentialState: value.CredentialState, CatalogState: value.CatalogState, ModelState: value.ModelState, InferenceState: value.InferenceState}, err
		})
	default:
		return nil, ErrMethod
	}
}

func nonNilStrings(values []string) []string { return append([]string{}, values...) }

func providerInventory(value providerhost.Inventory) protocol.ProviderInventory {
	result := protocol.ProviderInventory{Revision: value.Revision, PermissionMode: string(value.PermissionMode), Routes: []protocol.ProviderRoute{}}
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
		result.Routes = append(result.Routes, protocol.ProviderRoute{
			ID: protocol.ID(route.ID), Disabled: route.Disabled, Kind: route.Kind, BaseURL: route.BaseURL,
			Credential: protocol.ProviderCredentialStatus{
				Source: route.Credential.Source, State: route.Credential.State,
				Environment: route.Credential.Environment, File: route.Credential.File,
				CanDisconnect: route.Credential.CanDisconnect,
			},
			Models: models,
		})
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
