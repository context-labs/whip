package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCatalogMetadataRoundTrip(t *testing.T) {
	original := ProviderCatalogsResult{
		Models:    map[string]ModelDescriptor{"host-alias": {ID: "remote-model", Providers: []string{"private-host"}, Context: 12345}},
		Providers: map[string]ProviderDescriptor{"private-host": {BaseURL: "https://provider.example/v1"}},
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "baseURL") || strings.Contains(string(raw), "api_key") {
		t.Fatalf("invalid metadata wire: %s", raw)
	}
	var decoded ProviderCatalogsResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Models["host-alias"].ID != "remote-model" || decoded.Providers["private-host"].BaseURL == "" {
		t.Fatalf("metadata lost: %s", raw)
	}
}
