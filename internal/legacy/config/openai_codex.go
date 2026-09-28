package config

import (
	"errors"

	"github.com/context-labs/whip/internal/openaiauth"
)

// ValidateOpenAICodex prevents subscription tokens from being routed to custom
// endpoints or confused with API billing credentials.
func (p Provider) ValidateOpenAICodex() error {
	if p.API != openaiauth.Provider || p.BaseURL != openaiauth.BaseURL || p.APIKey != "" || p.APIKeyEnv != "" || p.Auth != "" {
		return errors.New("openai-codex requires the built-in ChatGPT endpoint and subscription login, without API keys")
	}
	return nil
}

// UpsertOpenAICodex preserves existing routes and defaults, refusing a conflicting
// provider entry rather than replacing a user's custom configuration.
func (c *Config) UpsertOpenAICodex() error {
	if existing, ok := c.Providers[openaiauth.Provider]; ok {
		return existing.ValidateOpenAICodex()
	}
	if c.Providers == nil {
		c.Providers = make(map[string]Provider)
	}
	c.Providers[openaiauth.Provider] = Provider{
		Name: "OpenAI (ChatGPT subscription)", BaseURL: openaiauth.BaseURL, API: openaiauth.Provider,
	}
	return nil
}
