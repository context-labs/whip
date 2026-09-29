package config

// Requesty is an OpenAI-compatible gateway: one key and one endpoint reach
// every model in its catalog, like OpenRouter. Its managed policies are
// curated routes listed at GET /models/managed beside the full catalog.

const (
	// RequestyBaseURL is the OpenAI-compatible API root.
	RequestyBaseURL = "https://router.requesty.ai/v1"
	// RequestyEnvVar is the environment variable the provider entry reads
	// its key from when configured in env mode.
	RequestyEnvVar = "REQUESTY_API_KEY"
)
