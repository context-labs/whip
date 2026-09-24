package llm

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestAPIResponseScopeCredentialIsolation(t *testing.T) {
	client := New("https://api.openai.com/v1", "fixture-only-key")
	scope := client.apiResponseScope()
	if scope != client.apiResponseScope() || scope != New(client.BaseURL, client.APIKey).apiResponseScope() {
		t.Fatal("scope must survive requests and client restarts for the same credential")
	}
	if scope == New(client.BaseURL, "different-fixture-only-key").apiResponseScope() {
		t.Fatal("different credentials must not share opaque reasoning history")
	}
	if strings.Contains(scope, client.APIKey) {
		t.Fatal("persisted scope must not contain the API key")
	}
	digest, ok := strings.CutPrefix(scope, "openai-api:")
	if !ok {
		t.Fatal("scope must not collide with subscription account IDs")
	}
	decoded, err := hex.DecodeString(digest)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("scope must contain a 256-bit digest: %q, %v", digest, err)
	}
}
