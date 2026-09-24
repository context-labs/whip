package protocol

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

func TestCatalogWirePreservesCacheSeparately(t *testing.T) {
	original := ProviderCatalogsResult{Catalogs: map[string]config.Catalog{"test": {BaseURL: "https://example.invalid", Models: []config.ModelInfoLite{{ID: "model", ContextLength: 128, Pricing: llm.Pricing{Prompt: "0.1234567890123456789012345678", Completion: "0.000005", InputCacheRead: "0"}}}}}}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "baseUrl") || !strings.Contains(string(raw), "context_length") {
		t.Fatal(string(raw))
	}
	for _, field := range []string{`"pricing_known":true`, `"in_price":`, `"out_price":`, `"cache_read_price_known":true`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("catalog lost existing scalar display field %s: %s", field, raw)
		}
	}
	schema, err := SchemaFor(reflect.TypeFor[ProviderCatalogsResult]())
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	var value any
	_ = json.Unmarshal(raw, &value)
	if err = resolved.Validate(value); err != nil {
		t.Fatal(err)
	}
	// Required map fields normalize absent Go values to JSON objects.
	original.Models = map[string]ModelDescriptor{}
	original.Providers = map[string]ProviderDescriptor{}
	var restored ProviderCatalogsResult
	if err = json.Unmarshal(raw, &restored); err != nil || !reflect.DeepEqual(original, restored) {
		t.Fatalf("roundtrip: %v %v", restored, err)
	}
	cache, _ := json.Marshal(original.Catalogs["test"])
	if !strings.Contains(string(cache), "baseUrl") {
		t.Fatal("changed persisted cache format")
	}
}
