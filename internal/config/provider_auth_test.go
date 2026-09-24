package config

import "testing"

func TestProviderExplicitNoAuthentication(t *testing.T) {
	t.Setenv("WHIP_HOME", t.TempDir())
	t.Setenv("INFERENCE_API_KEY", "private-fixture-key")
	provider := Provider{BaseURL: InferenceNetBaseURL, API: "openai-completions", Auth: "none"}
	if key, err := provider.ResolveKey(); key != "" || err != nil {
		t.Fatalf("no auth used fallback: key present %t, error %v", key != "", err)
	}
	if status := provider.KeyStatus(); status.Source != "none" || !status.Available {
		t.Fatalf("no auth readiness: %+v", status)
	}
	for _, invalid := range []Provider{
		{Auth: "none", APIKey: "key"},
		{Auth: "none", APIKeyEnv: "INFERENCE_API_KEY"},
		{Auth: "none", API: "openai-codex"},
		{Auth: "unknown"},
	} {
		if _, err := invalid.ResolveKey(); err == nil {
			t.Fatalf("accepted ambiguous authentication %+v", invalid)
		}
		if invalid.KeyStatus().Available {
			t.Fatal("invalid authentication is ready")
		}
	}
}
