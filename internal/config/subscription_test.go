package config

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestSubscriptionHostUsesNoConfigurableCredentialOrEndpoint(t *testing.T) {
	host := Default()
	host.Providers["subscription"] = Provider{Kind: "openai-codex", Models: map[string]Model{"gpt-6-astra": {ContextWindowTokens: new(int64(1000))}}}
	if err := host.Validate(); err != nil {
		t.Fatal("unspecified subscription ceiling must remain unresolved until preparation", err)
	}
	directory := t.TempDir()
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Providers["subscription"].Models["gpt-6-astra"].MaxOutputTokens != 0 {
		t.Fatal("configuration invented a generic output cap")
	}
	for _, tc := range []struct{ name, url, credential string }{
		{"endpoint", "https://chatgpt.com/backend-api/codex", ""},
		{"arbitrary endpoint", "https://example.test", ""},
		{"credential", "", "TOKEN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := host.Providers["subscription"]
			p.BaseURL, p.CredentialEnv = tc.url, tc.credential
			host.Providers["subscription"] = p
			if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("accepted configurable subscription route: %v", err)
			}
		})
	}
	p := Provider{Kind: "openai-codex", CredentialEnv: "TOKEN"}
	lookups := 0
	if _, err := p.Credential(func(string) (string, bool) { lookups++; return "private", true }); !errors.Is(err, session.ErrInvalid) || lookups != 0 {
		t.Fatalf("subscription consulted API credential: %v lookups=%d", err, lookups)
	}
}
