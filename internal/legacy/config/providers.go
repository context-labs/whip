package config

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/openaiauth"
)

// ProviderPreset describes a connection WHIP supports without endpoint setup.
type ProviderPreset struct {
	ID                   string
	Provider             Provider
	Methods              []string
	Recommended          bool
	Category             string
	Family               string
	KeyURL               string
	EnvironmentVariables []string
	// SuggestedModels are coding candidates; catalog membership is checked before use.
	SuggestedModels []string
	// OnboardingEffort enables one-step TUI selection of the first suggested model.
	OnboardingEffort string
}

// ProviderPresets returns fresh values; callers cannot change the built-ins.
func ProviderPresets() []ProviderPreset {
	return augmentProviderPresets(ProviderPresetPolicy())
}

// ProviderPresetPolicy returns Whip-owned connection behavior before catalog enrichment.
func ProviderPresetPolicy() []ProviderPreset {
	return []ProviderPreset{
		{
			ID: InferenceNetProvider,
			Provider: Provider{
				Name: "Inference.net", BaseURL: InferenceNetBaseURL,
				API: "openai-completions", APIKeyEnv: InferenceNetEnvVar,
			},
			Methods:     []string{"login", "api_key"},
			Recommended: true,
			Category:    "popular", KeyURL: "https://inference.net/dashboard",
			EnvironmentVariables: []string{InferenceNetEnvVar},
			SuggestedModels:      []string{"kimi-k3-fast", "kimi-k3"},
			OnboardingEffort:     "high",
		},
		{
			ID: "openrouter",
			Provider: Provider{
				Name: "OpenRouter", BaseURL: OpenRouterBaseURL,
				API: "openai-completions", APIKeyEnv: OpenRouterEnvVar,
			},
			Methods:  []string{"api_key"},
			Category: "popular", KeyURL: "https://openrouter.ai/settings/keys",
			EnvironmentVariables: []string{OpenRouterEnvVar},
			SuggestedModels:      []string{"z-ai/glm-5.3", "moonshotai/kimi-k3", "moonshotai/kimi-k2.5", "anthropic/claude-sonnet-4.6"},
			OnboardingEffort:     "max",
		},
		{
			ID: "openai",
			// #nosec G101 -- APIKeyEnv is an environment variable name, not a credential.
			Provider: Provider{Name: "OpenAI", BaseURL: "https://api.openai.com/v1", API: "openai-completions", APIKeyEnv: "OPENAI_API_KEY"},
			Methods:  []string{"api_key"}, Category: "popular", Family: "openai",
			KeyURL: "https://platform.openai.com/api-keys", EnvironmentVariables: []string{"OPENAI_API_KEY"},
			SuggestedModels: []string{"gpt-6-astra"}, OnboardingEffort: "medium",
		},
		{
			ID: openaiauth.Provider,
			Provider: Provider{
				Name: "OpenAI (ChatGPT subscription)", BaseURL: openaiauth.BaseURL, API: openaiauth.Provider,
			},
			Methods: []string{"login"}, Category: "popular", Family: "openai",
			OnboardingEffort: "medium",
			SuggestedModels:  []string{"gpt-6-astra", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.5", "gpt-5.3-codex"},
		},
		{
			ID: "cerebras",
			// #nosec G101 -- APIKeyEnv is an environment variable name, not a credential.
			Provider: Provider{
				Name:    "Cerebras",
				BaseURL: "https://api.cerebras.ai/v1",
				API:     "openai-completions", APIKeyEnv: "CEREBRAS_API_KEY",
			},
			Methods:              []string{"api_key"},
			Category:             "providers",
			KeyURL:               "https://cloud.cerebras.ai/platform/api-keys",
			EnvironmentVariables: []string{"CEREBRAS_API_KEY"},
		},
		{
			ID: "deepinfra",
			// #nosec G101 -- APIKeyEnv is an environment variable name, not a credential.
			Provider: Provider{
				Name:    "DeepInfra",
				BaseURL: "https://api.deepinfra.com/v1/openai",
				API:     "openai-completions", APIKeyEnv: "DEEPINFRA_API_KEY",
			},
			Methods:              []string{"api_key"},
			Category:             "providers",
			KeyURL:               "https://deepinfra.com/dash/api_keys",
			EnvironmentVariables: []string{"DEEPINFRA_API_KEY", "DEEPINFRA_TOKEN"},
		},
		{
			ID: "deepseek",
			// #nosec G101 -- APIKeyEnv is an environment variable name, not a credential.
			Provider: Provider{
				Name:    "DeepSeek",
				BaseURL: "https://api.deepseek.com",
				API:     "openai-completions", APIKeyEnv: "DEEPSEEK_API_KEY",
			},
			Methods:              []string{"api_key"},
			Category:             "providers",
			KeyURL:               "https://platform.deepseek.com/api_keys",
			EnvironmentVariables: []string{"DEEPSEEK_API_KEY"},
		},
		{
			ID: "fireworks-ai",
			Provider: Provider{
				Name:    "Fireworks AI",
				BaseURL: "https://api.fireworks.ai/inference/v1",
				API:     "openai-completions", APIKeyEnv: "FIREWORKS_API_KEY",
			},
			Methods:              []string{"api_key"},
			Category:             "providers",
			KeyURL:               "https://app.fireworks.ai/settings/users/api-keys",
			EnvironmentVariables: []string{"FIREWORKS_API_KEY"},
		},
		{
			ID: "groq",
			// #nosec G101 -- APIKeyEnv is an environment variable name, not a credential.
			Provider: Provider{
				Name:    "Groq",
				BaseURL: "https://api.groq.com/openai/v1",
				API:     "openai-completions", APIKeyEnv: "GROQ_API_KEY",
			},
			Methods:              []string{"api_key"},
			Category:             "providers",
			KeyURL:               "https://console.groq.com/keys",
			EnvironmentVariables: []string{"GROQ_API_KEY"},
		},
		{
			ID: "togetherai",
			Provider: Provider{
				Name:    "Together AI",
				BaseURL: "https://api.together.ai/v1",
				API:     "openai-completions", APIKeyEnv: "TOGETHER_API_KEY",
			},
			Methods:              []string{"api_key"},
			Category:             "providers",
			KeyURL:               "https://api.together.ai/settings/api-keys",
			EnvironmentVariables: []string{"TOGETHER_API_KEY"},
		},
		{
			ID: "xai",
			// #nosec G101 -- APIKeyEnv is an environment variable name, not a credential.
			Provider: Provider{
				Name:    "xAI",
				BaseURL: "https://api.x.ai/v1",
				API:     "openai-completions", APIKeyEnv: "XAI_API_KEY",
			},
			Methods:              []string{"api_key"},
			Category:             "providers",
			KeyURL:               "https://console.x.ai/",
			EnvironmentVariables: []string{"XAI_API_KEY"},
		},
	}
}

// AvailableEnvironmentVariable returns the first available, correctly routed host key name.
func (p ProviderPreset) AvailableEnvironmentVariable() string {
	if p.ID == "openai" && !openAIEnvironmentMatches() {
		return ""
	}
	for _, name := range p.EnvironmentVariables {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return name
		}
	}
	return ""
}

func openAIEnvironmentMatches() bool {
	for _, name := range []string{"OPENAI_BASE_URL", "OPENAI_API_BASE"} {
		value := strings.TrimSpace(os.Getenv(name))
		if value != "" && strings.TrimRight(value, "/") != "https://api.openai.com/v1" {
			return false
		}
	}
	return true
}

func canonicalPreset(p Provider) (ProviderPreset, bool) {
	if p.API != "" && p.API != "openai-completions" {
		return ProviderPreset{}, false
	}
	for _, preset := range ProviderPresets() {
		if preset.Provider.API == "openai-completions" && strings.TrimRight(p.BaseURL, "/") == preset.Provider.BaseURL {
			return preset, true
		}
	}
	return ProviderPreset{}, false
}

// EffectiveProviders adds discovered routes without mutating saved configuration.
// Explicit entries win as a whole, including their endpoint and key references.
func (c *Config) EffectiveProviders() map[string]Provider {
	return DiscoverCredentials(c).EffectiveProviders(c)
}

// EffectiveProviders reuses the operation's external credential snapshot.
func (credentials *CredentialSnapshot) EffectiveProviders(c *Config) map[string]Provider {
	providers := maps.Clone(c.Providers)
	if providers == nil {
		providers = map[string]Provider{}
	}
	for _, preset := range ProviderPresets() {
		if _, exists := providers[preset.ID]; exists || preset.Provider.API == openaiauth.Provider {
			continue
		}
		if status := credentials.KeyStatus(preset.Provider); status.Available {
			provider := preset.Provider
			if status.Environment != "" {
				provider.APIKeyEnv = status.Environment
			}
			providers[preset.ID] = provider
		}
	}
	for _, name := range c.DisabledProviders {
		delete(providers, name)
	}
	return providers
}

// EnableProvider removes a deliberate opt-out after successful connection setup.
func (c *Config) EnableProvider(name string) {
	c.DisabledProviders = slices.DeleteFunc(c.DisabledProviders, func(value string) bool { return value == name })
}

// KeyStatus contains only credential provenance, never credential material.
type KeyStatus struct {
	Source      string
	Environment string
	Available   bool
	Path        string
	Error       string
}

// KeyStatus follows runtime precedence without executing configured secret commands.
func (p Provider) KeyStatus(cfg ...*Config) KeyStatus {
	return DiscoverCredentials(providerCredentialConfigs(p, cfg)...).KeyStatus(p)
}

// KeyStatus uses this operation's credential snapshot without executing secret commands.
func (s *CredentialSnapshot) KeyStatus(p Provider) KeyStatus {
	key, status, err := p.resolveKeyWithCredentials(false, s)
	if err != nil {
		status.Error = err.Error()
	}
	status.Available = strings.TrimSpace(key) != "" || p.Auth == "none" && p.ValidateAuth() == nil
	return status
}

// ValidateAuth rejects ambiguous unauthenticated routes before key resolution.
func (p Provider) ValidateAuth() error {
	if p.Auth != "" && p.Auth != "none" {
		return errors.New("unsupported provider authentication mode")
	}
	if p.Auth == "none" && (p.APIKey != "" || p.APIKeyEnv != "" || p.API != "" && p.API != "openai-completions") {
		return errors.New("unauthenticated providers cannot contain credentials or use subscription APIs")
	}
	return nil
}

// ResolveKey resolves credentials for a route using this operation's snapshot.
func (s *CredentialSnapshot) ResolveKey(p Provider) (string, error) {
	key, _, err := p.resolveKeyWithCredentials(true, s)
	return key, err
}

func (p Provider) resolveKeyWithCredentials(commands bool, snapshot *CredentialSnapshot) (string, KeyStatus, error) {
	status := KeyStatus{Source: "none"}
	if err := p.ValidateAuth(); err != nil {
		return "", status, err
	}
	if p.Auth == "none" {
		return "", status, nil
	}
	if snapshot == nil {
		snapshot = DiscoverCredentials()
	}
	if p.APIKeyEnv != "" {
		key, found, err := snapshot.providerNamedKey(p, p.APIKeyEnv)
		status = found
		if err != nil && p.APIKey == "" {
			return "", status, err
		}
		if key != "" {
			return key, status, nil
		}
	}
	if p.APIKey != "" {
		status = KeyStatus{Source: "literal"}
		if name := providerReferenceName(p.APIKey); name != "" {
			key, found, err := snapshot.providerNamedKey(p, name)
			if err == nil && key == "" {
				err = fmt.Errorf("provider key %s is unavailable", name)
			}
			return key, found, err
		}
		if strings.HasPrefix(p.APIKey, "!") {
			status.Source = "command"
			if !commands {
				return "", status, nil
			}
		} else if strings.HasPrefix(p.APIKey, "${") && strings.HasSuffix(p.APIKey, "}") && isEnvRefBody(p.APIKey[2:len(p.APIKey)-1]) {
			status.Source = "reference"
		}
		key, err := ResolveSecret(p.APIKey)
		if err != nil {
			return "", status, fmt.Errorf("provider %q apiKey: %w", p.Name, err)
		}
		return key, status, nil
	}
	// Only a preset's own reference opts into its alternative names.
	preset, canonical := canonicalPreset(p)
	if canonical && p.APIKeyEnv == preset.Provider.APIKeyEnv {
		for _, name := range preset.EnvironmentVariables {
			if name == p.APIKeyEnv {
				continue
			}
			key, found, err := snapshot.providerNamedKey(p, name)
			if err != nil {
				return "", found, err
			}
			if key != "" {
				return key, found, nil
			}
		}
	}
	if strings.TrimRight(p.BaseURL, "/") == InferenceNetBaseURL {
		if snapshot.machineKey != "" {
			return snapshot.machineKey, KeyStatus{Source: "machine"}, nil
		}
		if snapshot.infKey != "" {
			return snapshot.infKey, KeyStatus{Source: "external"}, nil
		}
	}
	return "", status, nil
}
