package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExplicitCacheControlPreservesOwnerAndProviderProfile(t *testing.T) {
	for _, endpoint := range []string{"https://api.openai.com/v1", "https://api.groq.com/openai/v1"} {
		request := chatRequest()
		owner := request.SessionID
		request.CacheKey = strings.Repeat("cache", 30)
		raw, err := encodeChat(request, endpoint, 100)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if endpoint == "https://api.openai.com/v1" {
			if body["prompt_cache_key"] != requestCacheKey(request) || len(body["prompt_cache_key"].(string)) != 64 {
				t.Fatal(body)
			}
		} else if _, ok := body["prompt_cache_key"]; ok {
			t.Fatal("unsupported profile received cache key")
		}
		if request.SessionID != owner {
			t.Fatal("cache key replaced owner identity")
		}
	}
}
