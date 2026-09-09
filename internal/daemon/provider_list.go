package daemon

import (
	"cmp"
	"errors"
	"fmt"
	"net/url"
	"os"
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
	} else if name, _, _, _, resolveErr := cfg.ResolveRoute("", ""); resolveErr == nil {
		result.DefaultProvider = name
	}
	seen := map[string]bool{}
	for _, preset := range config.ProviderPresets() {
		entry := protocol.ProviderEntry{ID: preset.ID, Name: preset.Provider.Name, Methods: preset.Methods}
		if route, exists := cfg.Providers[preset.ID]; exists {
			entry.Name = cmp.Or(route.Name, entry.Name)
			if preset.ID != openaiauth.Provider && strings.TrimRight(route.BaseURL, "/") != preset.Provider.BaseURL {
				entry.Custom = true
			}
			if preset.ID == config.InferenceNetProvider && inferenceNetLoginRoute(cfg) != nil {
				entry.Methods = []string{"api_key"}
			}
			if preset.ID != openaiauth.Provider && route.API != "" && route.API != "openai-completions" {
				entry.Methods = []string{}
			}
		}
		entry.Status, err = s.providerStatus(cfg, preset.ID)
		if err != nil {
			entry.Status = unavailableProvider(preset.ID, "Could not read the provider account on this host.")
		}
		// Offer the same explicit environment mode as key setup. A saved literal
		// remains authoritative until the user chooses this connection method.
		variable := cfg.Providers[preset.ID].APIKeyEnv
		if variable == "" && !entry.Custom {
			variable = preset.Provider.APIKeyEnv
		}
		if slices.Contains(entry.Methods, "api_key") && variable != "" && entry.Status.KeySource != "environment" && strings.TrimSpace(os.Getenv(variable)) != "" {
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
		entry := protocol.ProviderEntry{ID: id, Name: cmp.Or(route.Name, id), Custom: true, Methods: []string{}}
		if route.API == "openai-completions" || route.API == "" {
			entry.Methods = append(entry.Methods, "api_key")
		}
		entry.Status, err = s.providerStatus(cfg, id)
		if err != nil {
			entry.Status = unavailableProvider(id, "Could not read the provider configuration on this host.")
		}
		custom = append(custom, entry)
	}
	slices.SortFunc(custom, func(a, b protocol.ProviderEntry) int { return cmp.Compare(a.ID, b.ID) })
	result.Providers = append(result.Providers, custom...)
	return result, nil
}

func unavailableProvider(id, warning string) ProviderStatus {
	return ProviderStatus{
		Provider: id, KeySource: "none", Available: new(false),
		AuthState: "configuration_error", Warnings: []string{warning},
	}
}

func (s *ProviderService) providerStatus(cfg *config.Config, name string) (ProviderStatus, error) {
	if name == openaiauth.Provider {
		if s == nil || s.openAI == nil {
			return unavailableProvider(name, "Subscription credentials are unavailable on this host."), nil
		}
		return s.openAIStatusFromConfig(cfg)
	}
	return providerKeyStatus(cfg, name)
}

func providerKeyStatus(cfg *config.Config, name string) (ProviderStatus, error) {
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
	key := entry.KeyStatus()
	disabled := slices.Contains(cfg.DisabledProviders, name)
	result := ProviderStatus{
		Provider: name, Configured: configured, KeySource: key.Source, Disabled: disabled,
		Available: new(key.Available && !disabled), EnvironmentVariable: key.Environment,
		AuthState: "key_required", Warnings: []string{},
	}
	if key.Available {
		result.AuthState = "connected"
	}
	if key.Source == "command" {
		result.AuthState = "unchecked"
	}
	endpoint, endpointErr := url.Parse(entry.BaseURL)
	validEndpoint := endpointErr == nil && endpoint.Host != "" && (endpoint.Scheme == "https" || endpoint.Scheme == "http") && endpoint.User == nil && endpoint.RawQuery == "" && endpoint.Fragment == ""
	if !validEndpoint || (entry.API != "" && entry.API != "openai-completions") {
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
			return errors.New("Inference.net account login requires its built-in endpoint; the saved custom provider was left unchanged")
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
