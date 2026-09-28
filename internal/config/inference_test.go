package config

import (
	"errors"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestManagedInferenceRequiresExplicitExactGateway(t *testing.T) {
	for _, test := range []struct {
		name, kind, url, source, env string
		valid                        bool
	}{
		{"managed", "openai-chat", "https://api.inference.net/v1", "inference-net", "", true},
		{"plain no auth", "openai-chat", "https://api.inference.net/v1", "", "", true},
		{"explicit environment", "openai-chat", "https://api.inference.net/v1", "", "NAMED_KEY", true},
		{"mixed sources", "openai-chat", "https://api.inference.net/v1", "inference-net", "NAMED_KEY", false},
		{"unknown source", "openai-chat", "https://api.inference.net/v1", "automatic", "", false},
		{"management plane", "openai-chat", "https://observability-api.inference.net", "inference-net", "", false},
		{"other host", "openai-chat", "https://example.test/v1", "inference-net", "", false},
		{"lookalike", "openai-chat", "https://api.inference.net.example.test/v1", "inference-net", "", false},
		{"plaintext", "openai-chat", "http://api.inference.net/v1", "inference-net", "", false},
		{"port", "openai-chat", "https://api.inference.net:443/v1", "inference-net", "", false},
		{"trailing slash", "openai-chat", "https://api.inference.net/v1/", "inference-net", "", false},
		{"other path", "openai-chat", "https://api.inference.net/v1/other", "inference-net", "", false},
		{"encoded path", "openai-chat", "https://api.inference.net/%76%31", "inference-net", "", false},
		{"query", "openai-chat", "https://api.inference.net/v1?key=private", "inference-net", "", false},
		{"fragment", "openai-chat", "https://api.inference.net/v1#fragment", "inference-net", "", false},
		{"userinfo", "openai-chat", "https://private@api.inference.net/v1", "inference-net", "", false},
		{"responses", "openai-responses", "https://api.inference.net/v1", "inference-net", "", false},
		{"subscription", "openai-codex", "", "inference-net", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := Default()
			provider := Provider{Kind: test.kind, BaseURL: test.url, CredentialSource: test.source, CredentialEnv: test.env}
			host.Providers["inference"] = provider
			err := host.Validate()
			if test.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, session.ErrInvalid) {
				t.Fatalf("unsafe managed route accepted: %v", err)
			}
			lookups := 0
			credential, err := provider.Credential(t.Context(), func(string) (string, bool) { lookups++; return "private-env", true })
			if test.source != "" && (lookups != 0 || credential != "" || test.valid != (err == nil)) {
				t.Fatalf("managed source consulted an environment credential: lookups=%d err=%v", lookups, err)
			}
		})
	}
}

func TestManagedInferenceHostRoundTrip(t *testing.T) {
	host := Default()
	host.Providers["managed"] = Provider{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1", CredentialSource: "inference-net"}
	directory := t.TempDir()
	if err := Save(directory, host); err != nil {
		t.Fatal(err)
	}
	read, err := Load(directory)
	if err != nil || read.Providers["managed"].CredentialSource != "inference-net" {
		t.Fatalf("managed source did not survive host round trip: %v", err)
	}
	host.Version = 9
	if err := host.Validate(); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("old fresh host version accepted")
	}
}
