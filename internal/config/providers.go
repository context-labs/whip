package config

import (
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/openaiauth"
)

// ProviderPreset describes a connection WHIP supports without endpoint setup.
type ProviderPreset struct {
	ID       string
	Provider Provider
	Methods  []string
}

// ProviderPresets returns fresh values; callers cannot change the built-ins.
func ProviderPresets() []ProviderPreset {
	return []ProviderPreset{
		{
			ID: InferenceNetProvider,
			Provider: Provider{
				Name: "Inference.net", BaseURL: InferenceNetBaseURL,
				API: "openai-completions", APIKeyEnv: InferenceNetEnvVar,
			},
			Methods: []string{"login", "api_key"},
		},
		{
			ID: "openrouter",
			Provider: Provider{
				Name: "OpenRouter", BaseURL: OpenRouterBaseURL,
				API: "openai-completions", APIKeyEnv: OpenRouterEnvVar,
			},
			Methods: []string{"api_key"},
		},
		{
			ID: openaiauth.Provider,
			Provider: Provider{
				Name: "OpenAI (ChatGPT subscription)", BaseURL: openaiauth.BaseURL, API: openaiauth.Provider,
			},
			Methods: []string{"login"},
		},
	}
}

// EffectiveProviders adds discovered routes without mutating saved configuration.
// Explicit entries win as a whole, including their endpoint and key references.
func (c *Config) EffectiveProviders() map[string]Provider {
	providers := maps.Clone(c.Providers)
	if providers == nil {
		providers = map[string]Provider{}
	}
	for _, preset := range ProviderPresets() {
		if _, exists := providers[preset.ID]; exists || preset.Provider.API == openaiauth.Provider {
			continue
		}
		if preset.Provider.KeyStatus().Available {
			providers[preset.ID] = preset.Provider
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
}

// KeyStatus follows runtime precedence without executing configured secret commands.
func (p Provider) KeyStatus() KeyStatus {
	key, status, _ := p.resolveKey(false)
	status.Available = strings.TrimSpace(key) != ""
	return status
}

func (p Provider) resolveKey(commands bool) (string, KeyStatus, error) {
	status := KeyStatus{Source: "none"}
	if p.APIKeyEnv != "" {
		status = KeyStatus{Source: "environment", Environment: p.APIKeyEnv}
		if value := os.Getenv(p.APIKeyEnv); strings.TrimSpace(value) != "" {
			return value, status, nil
		}
	}
	if p.APIKey != "" {
		status = KeyStatus{Source: "literal"}
		switch {
		case strings.HasPrefix(p.APIKey, "!"):
			status.Source = "command"
			if !commands {
				return "", status, nil
			}
		case strings.HasPrefix(p.APIKey, "${") && strings.HasSuffix(p.APIKey, "}") && isEnvRefBody(p.APIKey[2:len(p.APIKey)-1]):
			status.Source = "reference"
			name := p.APIKey[2 : len(p.APIKey)-1]
			if isEnvName(name) {
				status.Source, status.Environment = "environment", name
			}
		case strings.HasPrefix(p.APIKey, "$") && isEnvName(p.APIKey[1:]):
			status.Source = "environment"
			status.Environment = p.APIKey[1:]
		}
		key, err := ResolveSecret(p.APIKey)
		if err != nil {
			return "", status, fmt.Errorf("provider %q apiKey: %w", p.Name, err)
		}
		return key, status, nil
	}
	// Account fallbacks belong only to the built-in endpoint, not lookalike URLs.
	if strings.TrimRight(p.BaseURL, "/") == InferenceNetBaseURL {
		if key := whipInferenceNetKey(); key != "" {
			return key, KeyStatus{Source: "machine"}, nil
		}
		if key := infKey(); key != "" {
			return key, KeyStatus{Source: "external"}, nil
		}
	}
	return "", status, nil
}
