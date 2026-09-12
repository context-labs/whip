package daemon

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/inferencenet"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
	"github.com/context-labs/whip/internal/protocol"
)

// ListProviders reads host-owned connection metadata without upstream requests
// or execution of secret commands. Its revision guards subsequent mutations.
func (s *ProviderService) ListProviders() (protocol.ProviderList, error) {
	return s.ListProvidersFor("", "")
}

// ListProvidersFor checks an explicit session/CLI selection without changing defaults.
func (s *ProviderService) ListProvidersFor(model, provider string) (protocol.ProviderList, error) {
	cfg, revision, err := config.ReadVersioned()
	if err != nil {
		return protocol.ProviderList{}, err
	}
	result := protocol.ProviderList{Revision: revision, Providers: []protocol.ProviderEntry{}}
	result.DefaultProvider = cfg.DefaultProvider
	if model, configured := cfg.Models[cfg.DefaultModel]; configured {
		if result.DefaultProvider == "" && len(model.Providers) > 0 {
			result.DefaultProvider = model.Providers[0]
		}
	}
	credentials := config.DiscoverCredentials(cfg)
	seen := map[string]bool{}
	for _, preset := range config.ProviderPresets() {
		entry := protocol.ProviderEntry{
			ID: preset.ID, Name: preset.Provider.Name, Methods: preset.Methods,
			Category: preset.Category, Family: preset.Family, KeyURL: preset.KeyURL,
		}
		if route, exists := cfg.Providers[preset.ID]; exists {
			entry.Name = cmp.Or(route.Name, entry.Name)
			if preset.ID != openaiauth.Provider && strings.TrimRight(route.BaseURL, "/") != preset.Provider.BaseURL {
				entry.Custom = true
				entry.Category, entry.Family, entry.KeyURL = "providers", "", ""
			}
			if preset.ID == config.InferenceNetProvider && inferenceNetLoginRoute(cfg) != nil {
				entry.Methods = []string{"api_key"}
			}
			if preset.ID != openaiauth.Provider && route.API != "" && route.API != "openai-completions" {
				entry.Methods = []string{}
			}
		}
		entry.Status, err = s.providerStatusWithCredentials(cfg, preset.ID, credentials)
		if err != nil {
			entry.Status = unavailableProvider(preset.ID, "Could not read the provider account on this host.")
		}
		// Offer the same explicit environment mode as key setup. A saved literal
		// remains authoritative until the user chooses this connection method.
		variable := cfg.Providers[preset.ID].APIKeyEnv
		if variable == "" && !entry.Custom {
			variable = credentials.AvailableEnvironmentVariable(preset)
		}
		if !entry.Custom && variable == preset.Provider.APIKeyEnv {
			variable = credentials.AvailableEnvironmentVariable(preset)
		}
		candidate := preset.Provider
		candidate.APIKeyEnv = variable
		if slices.Contains(entry.Methods, "api_key") && variable != "" && entry.Status.EnvironmentVariable != variable && credentials.KeyStatus(candidate).Available {
			entry.Methods = append(entry.Methods, "environment")
		}
		result.Providers = append(result.Providers, entry)
		seen[preset.ID] = true
	}
	custom := []protocol.ProviderEntry{}
	for id, route := range cfg.Providers {
		if seen[id] {
			continue
		}
		entry := protocol.ProviderEntry{ID: id, Name: cmp.Or(route.Name, id), Custom: true, Methods: []string{}, Category: "providers"}
		if route.API == "openai-completions" || route.API == "" {
			entry.Methods = append(entry.Methods, "api_key")
		}
		entry.Status, err = s.providerStatusWithCredentials(cfg, id, credentials)
		if err != nil {
			entry.Status = unavailableProvider(id, "Could not read the provider configuration on this host.")
		}
		custom = append(custom, entry)
	}
	slices.SortFunc(custom, func(a, b protocol.ProviderEntry) int { return cmp.Compare(a.ID, b.ID) })
	result.Providers = append(result.Providers, custom...)
	catalogs := s.catalogsForRoutes(credentials.EffectiveProviders(cfg))
	if result.DefaultProvider == "" {
		result.DefaultProvider = s.providerSelectionWithCredentials(cfg, catalogs, "", "", credentials).Provider
	}
	selection := s.providerSelectionWithCredentials(cfg, catalogs, model, provider, credentials)
	result.Selection = &selection
	for i := range result.Providers {
		entry := &result.Providers[i]
		for _, preset := range config.ProviderPresets() {
			if preset.ID == entry.ID {
				entry.Recommended = preset.Recommended && !entry.Custom && !entry.Status.Disabled && entry.Status.AuthState != "configuration_error"
				break
			}
		}
		if providerCanAttempt(entry.Status) {
			entry.SuggestedModel = s.suggestedProviderModelWithCredentials(cfg, catalogs, entry.ID, credentials)
		}
	}
	return result, nil
}

func unavailableProvider(id, warning string) ProviderStatus {
	return ProviderStatus{
		Provider: id, KeySource: "none", Available: new(false),
		AuthState: "configuration_error", Warnings: []string{warning},
	}
}

func (s *ProviderService) providerStatus(cfg *config.Config, name string) (ProviderStatus, error) {
	return s.providerStatusWithCredentials(cfg, name, nil)
}

func (s *ProviderService) providerStatusWithCredentials(cfg *config.Config, name string, credentials *config.CredentialSnapshot) (ProviderStatus, error) {
	if name == openaiauth.Provider {
		if s == nil || s.openAI == nil {
			return unavailableProvider(name, "Subscription credentials are unavailable on this host."), nil
		}
		return s.openAIStatusFromConfig(cfg)
	}
	return providerKeyStatusWithCredentials(cfg, name, credentials)
}

func providerKeyStatus(cfg *config.Config, name string) (ProviderStatus, error) {
	return providerKeyStatusWithCredentials(cfg, name, nil)
}

func providerKeyStatusWithCredentials(cfg *config.Config, name string, credentials *config.CredentialSnapshot) (ProviderStatus, error) {
	if credentials == nil {
		credentials = config.DiscoverCredentials(cfg)
	}
	entry, configured := cfg.Providers[name]
	if !configured {
		found := false
		for _, preset := range config.ProviderPresets() {
			if preset.ID == name {
				entry, found = preset.Provider, true
				break
			}
		}
		if !found {
			return ProviderStatus{}, errors.New("unknown provider")
		}
	}
	key := credentials.KeyStatus(entry)
	disabled := slices.Contains(cfg.DisabledProviders, name)
	result := ProviderStatus{
		Provider: name, Configured: configured, KeySource: key.Source, Disabled: disabled,
		Available: new(key.Available && !disabled), EnvironmentVariable: key.Environment,
		CredentialPath: key.Path,
		AuthState:      "key_required", Warnings: []string{},
	}
	if key.Available {
		result.AuthState = "connected"
	}
	if key.Error != "" {
		result.AuthState = "configuration_error"
		result.Warnings = append(result.Warnings, key.Error)
	}
	if entry.Auth == "none" {
		result.AuthMethod = "none"
	}
	if key.Source == "command" {
		result.AuthState = "unchecked"
	}
	endpoint, endpointErr := url.Parse(entry.BaseURL)
	validEndpoint := endpointErr == nil && endpoint.Host != "" && (endpoint.Scheme == "https" || endpoint.Scheme == "http") && endpoint.User == nil && endpoint.RawQuery == "" && endpoint.Fragment == ""
	if !validEndpoint || (entry.API != "" && entry.API != "openai-completions") || entry.ValidateAuth() != nil {
		result.AuthState, result.Available = "configuration_error", new(false)
		result.Warnings = append(result.Warnings, "This provider requires a supported API configuration.")
	}
	if name == config.InferenceNetProvider && key.Source == "machine" {
		auth, err := inferencenet.LoadAuth()
		if err != nil {
			return ProviderStatus{}, errors.New("could not read provider account on execution host")
		}
		result.Email, result.ProjectID = auth.UserEmail, auth.ProjectID
		result.ProjectName, result.MachineKeyName = auth.ProjectName, auth.MachineKeyName
	}
	return result, nil
}

func inferenceNetLoginRoute(cfg *config.Config) error {
	if route, exists := cfg.Providers[config.InferenceNetProvider]; exists {
		if strings.TrimRight(route.BaseURL, "/") != config.InferenceNetBaseURL || (route.API != "" && route.API != "openai-completions") {
			return errors.New("account login to Inference.net requires its built-in endpoint; the saved custom provider was left unchanged")
		}
	}
	return nil
}

func (s *ProviderService) checkModelProvider(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return errors.New("could not read provider configuration on this host")
	}
	if slices.Contains(cfg.DisabledProviders, name) {
		return &llm.HTTPError{Status: "Provider disabled", Body: fmt.Sprintf("provider %q is disabled on this host; enable it in Settings or select another provider", name), Permanent: true}
	}
	status, err := s.providerStatus(cfg, name)
	if err != nil || (status.AuthState != "unchecked" && (status.Available == nil || !*status.Available)) {
		return &llm.HTTPError{Status: "Provider unavailable", Body: fmt.Sprintf("provider %q needs a connection on this host; connect it in Settings and reload the session", name), Permanent: true}
	}
	return nil
}
