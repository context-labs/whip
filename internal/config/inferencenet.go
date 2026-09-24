package config

// Inference.net is whip's first-class provider: `whipcode auth inference-net
// login` provisions a machine API key (stored in ~/.whipcode/inference-net.json)
// and registers the provider entry here, so a user never handles a key.
// BYOK is also supported (literal apiKey or apiKeyEnv). These helpers back
// `whipcode auth inference-net` and `/auth inference-net`.

const (
	// InferenceNetProvider is the canonical provider map key.
	InferenceNetProvider = "inference-net"
	// InferenceNetBaseURL is the OpenAI-compatible API root.
	InferenceNetBaseURL = "https://api.inference.net/v1"
	// InferenceNetEnvVar is the env var the provider reads in env mode.
	InferenceNetEnvVar = "INFERENCE_API_KEY"
)

// UpsertInferenceNet registers (or re-registers) the "inference-net" provider,
// leaving every other provider and all model routes untouched. envMode stores
// the key as apiKeyEnv; otherwise a literal apiKey. When neither applies (the
// machine-key fallback in ResolveKey covers it), both key fields are left
// empty so the stored machine key is used. The caller owns Save().
func (c *Config) UpsertInferenceNet(key string, envMode bool) {
	p := Provider{
		Name:    "Inference.net",
		BaseURL: InferenceNetBaseURL,
		API:     "openai-completions",
	}
	if envMode {
		p.APIKeyEnv = InferenceNetEnvVar
	} else if key != "" {
		p.APIKey = key
	}
	if c.Providers == nil {
		c.Providers = map[string]Provider{}
	}
	c.Providers[InferenceNetProvider] = p
	logf("config.inferencenet", "upserted inference-net provider (envMode=%v, literal=%v)", envMode, key != "")
}
