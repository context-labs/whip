package config

import "strings"

// OpenRouter is an OpenAI-compatible gateway: one key and one endpoint reach
// every model in its catalog, and whip's catalog resolution (resolveFromCatalog)
// makes each advertised model usable with no per-model config entry. These
// helpers back `whip auth openrouter` and `/auth openrouter`.

const (
	// OpenRouterBaseURL is the OpenAI-compatible API root.
	OpenRouterBaseURL = "https://openrouter.ai/api/v1"
	// OpenRouterEnvVar is the environment variable the provider entry reads
	// its key from when configured in env mode.
	OpenRouterEnvVar = "OPENROUTER_API_KEY"
)

// UpsertOpenRouter registers (or re-registers) the "openrouter" provider on
// cfg, leaving every other provider and all model routes untouched. The key
// lands in exactly one place: the environment (apiKeyEnv mode, for users who
// export OPENROUTER_API_KEY themselves) or a literal apiKey in config.json
// (0600 perms — the same trust level as pi/opencode's auth stores).
//
// Re-running with the other mode clears the stale field so there is never a
// split-brain key. The caller owns Save() and catalog prefetch.
func (c *Config) UpsertOpenRouter(key string, envMode bool) {
	p := Provider{
		Name:    "OpenRouter",
		BaseURL: OpenRouterBaseURL,
		API:     "openai-completions",
	}
	if envMode {
		p.APIKeyEnv = OpenRouterEnvVar
	} else {
		p.APIKey = key
	}
	if c.Providers == nil {
		c.Providers = map[string]Provider{}
	}
	c.Providers["openrouter"] = p
	logf("config.openrouter", "upserted openrouter provider (envMode=%v)", envMode)
}

// OpenRouterConfigured reports whether the openrouter provider entry exists
// and resolves to a non-empty key under the current environment.
func (c *Config) OpenRouterConfigured() bool {
	p, ok := c.Providers["openrouter"]
	return ok && p.Key(c) != ""
}

// TrimKey normalizes a pasted API key: whitespace and a stray leading
// "Bearer " both break Authorization headers, and both happen in practice
// when copying from dashboards.
func TrimKey(s string) string {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "Bearer "); ok {
		s = strings.TrimSpace(rest)
	}
	return s
}
