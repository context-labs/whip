package config

import "slices"

// Codex uses ChatGPT subscription credentials from ~/.codex/auth.json rather
// than an API key. Its account-scoped catalog augments this fixed fallback
// route after login, so these limits keep the provider usable if that fetch is
// temporarily unavailable.
const (
	CodexProviderName   = "codex"
	CodexBaseURL        = "https://chatgpt.com/backend-api"
	CodexDefaultModel   = "gpt-5.4"
	CodexDefaultContext = 272000
	CodexDefaultMaxOut  = 128000
)

// UpsertCodex registers the fixed Codex subscription provider and makes its
// default model route selectable. Existing model providers, configured limits,
// and defaults are preserved: signing in must not switch a user's active model
// or overwrite an intentional model override. The caller owns Save().
func (c *Config) UpsertCodex() {
	if c.Providers == nil {
		c.Providers = map[string]Provider{}
	}
	c.Providers[CodexProviderName] = Provider{
		Name:    "Codex",
		BaseURL: CodexBaseURL,
		API:     "openai-codex-responses",
		Auth:    "codex",
	}

	if c.Models == nil {
		c.Models = map[string]Model{}
	}
	m := c.Models[CodexDefaultModel]
	if !hasProvider(m.Providers, CodexProviderName) {
		m.Providers = append(m.Providers, CodexProviderName)
	}
	if m.Context == 0 && m.MaxTokens == 0 {
		m.Context = CodexDefaultContext
	}
	if m.MaxOut == 0 {
		m.MaxOut = CodexDefaultMaxOut
	}
	c.Models[CodexDefaultModel] = m
	logf("config.codex", "upserted codex provider and %s route", CodexDefaultModel)
}

func hasProvider(providers []string, want string) bool {
	return slices.Contains(providers, want)
}
