package providerhost

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/config"
)

func TestCatalogPricesPreserveExactNullableUnits(t *testing.T) {
	for _, test := range []struct {
		name, body, id, base string
		input, output, cache *int64
	}{
		{name: "exact per token", body: `{"data":[{"id":"model","pricing":{"prompt":"9.007199254740993","completion":"0","input_cache_read":null}}]}`, input: new(int64(9007199254740993)), output: new(int64(0))},
		{name: "absent stays unknown", body: `{"data":[{"id":"model","pricing":{"input_cache_read":"0"}}]}`, cache: new(int64(0))},
		{name: "numeric Together per million", id: "togetherai", base: "https://api.together.ai/v1", body: `[{"id":"model","type":"chat","pricing":{"input":9.007199254740993,"output":0.3,"cached_input":0}}]`, input: new(int64(9007199255)), output: new(int64(300000000)), cache: new(int64(0))},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := noAuth()
			if test.base != "" {
				p.BaseURL = test.base
			}
			models, err := decodeModels([]byte(test.body), test.id, p)
			if test.name == "numeric Together per million" {
				if err == nil {
					t.Fatal("nonintegral nano-USD rate rounded silently")
				}
				return
			}
			if err != nil || len(models) != 1 {
				t.Fatal("valid exact prices rejected", err)
			}
			prices := models[0].Prices
			if !reflect.DeepEqual(prices.Input, test.input) || !reflect.DeepEqual(prices.Output, test.output) || !reflect.DeepEqual(prices.CachedInput, test.cache) || prices.Reasoning != nil || prices.CachedOutput != nil {
				t.Fatalf("presence or exact units changed: %+v", prices)
			}
		})
	}
	models, err := decodeModels([]byte(`[{"id":"model","pricing":{"input":0.3,"output":0,"cached_input":null}}]`), "togetherai", config.Provider{Kind: "openai-chat", BaseURL: "https://api.together.ai/v1"})
	if err != nil || *models[0].Prices.Input != 300000000 || *models[0].Prices.Output != 0 || models[0].Prices.CachedInput != nil {
		t.Fatal("Together units changed", err)
	}
	for _, raw := range []string{`"-1"`, `"NaN"`, `"1/2"`, `"1e999999999"`, `"0.0000000000000001"`, `"999999999999999999"`} {
		if _, err := parseRate(json.RawMessage(raw), 1_000_000_000_000_000); err == nil {
			t.Fatalf("unrepresentable rate accepted: %s", raw)
		}
	}
}

func TestProviderMetadataEnvelopeAndCanonicalEnrichment(t *testing.T) {
	for _, test := range []struct {
		name, body      string
		context, output *int64
		efforts         []string
		tools           *bool
	}{
		{name: "OpenRouter architecture", body: `{"data":[{"id":"new-live-model","supported_parameters":["tools"],"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"top_provider":{"context_length":32000,"max_completion_tokens":4000}}]}`, context: new(int64(32000)), output: new(int64(4000)), tools: new(true)},
		{name: "explicit zero and empty", body: `{"data":[{"id":"new-live-model","context_length":0,"max_completion_tokens":0,"reasoning_efforts":[],"supports_tools":false}]}`, context: new(int64(0)), output: new(int64(0)), efforts: []string{}, tools: new(false)},
		{name: "Groq context window", body: `{"data":[{"id":"new-live-model","context_window":131072}]}`, context: new(int64(131072))},
		{name: "DeepInfra metadata", body: `{"data":[{"id":"new-live-model","metadata":{"context_length":64000}}]}`, context: new(int64(64000))},
	} {
		t.Run(test.name, func(t *testing.T) {
			models, err := decodeModels([]byte(test.body), "custom", noAuth())
			if err != nil || len(models) != 1 {
				t.Fatal(err)
			}
			value := models[0]
			if !reflect.DeepEqual(value.ContextWindowTokens, test.context) || !reflect.DeepEqual(value.MaxOutputTokens, test.output) || !reflect.DeepEqual(value.ReasoningEfforts, test.efforts) || !reflect.DeepEqual(value.SupportsTools, test.tools) {
				t.Fatalf("metadata changed: %+v", value)
			}
		})
	}
	raw := []byte(`{"data":[{"id":"kimi-k3-fast","pricing":{"prompt":"0"}},{"id":"new-live-model"}]}`)
	p := config.Provider{Kind: "openai-chat", BaseURL: "https://api.inference.net/v1"}
	live, err := decodeModels(raw, "inference-net", p)
	if err != nil || len(live) != 2 {
		t.Fatal("live membership replaced by bundled allowlist", err)
	}
	if live[0].ContextWindowTokens == nil || *live[0].Prices.Input != 0 || live[0].MetadataSource != "advertised+bundled" {
		t.Fatal("exact-ID supplemental metadata missing or zero overwritten")
	}
	p.BaseURL = "https://custom.test/v1"
	custom, err := decodeModels(raw, "inference-net", p)
	if err != nil || custom[0].ContextWindowTokens != nil {
		t.Fatal("preset metadata crossed custom endpoint", err)
	}
	for _, body := range []string{`{}`, `{"data":null}`, `{"data":[{"id":"same"},{"id":"same"}]}`, `{"data":[{"id":"bad\nname"}]}`, `{"data":[{"id":"m","context_length":1.5}]}`} {
		if _, err := decodeModels([]byte(body), "custom", noAuth()); err == nil {
			t.Fatal("malformed catalog accepted", body)
		}
	}
	for _, preset := range Presets() {
		models, err := BundledModels(preset.ID)
		if err != nil {
			t.Fatal(preset.ID, err)
		}
		for _, value := range models {
			if !validModel(value) {
				t.Fatal("invalid retained metadata", preset.ID, value.ID)
			}
		}
	}
}

func TestSubscriptionCatalogVisibilityNaturalCeilingAndUnknownPricing(t *testing.T) {
	body := `{"models":[{"slug":"gpt-6-astra","visibility":"list","context_window":1000000,"effective_context_window_percent":90,"supported_reasoning_levels":[{"effort":"medium"},{"effort":"high"}],"input_modalities":["text","image"]},{"slug":"gpt-5.5","visibility":"hidden","context_window":10000},{"slug":"unreviewed-new","visibility":"list","context_window":10000}]}`
	models, err := decodeModels([]byte(body), "openai-codex", config.Provider{Kind: "openai-codex"})
	if err != nil || len(models) != 1 {
		t.Fatal("unsupported visibility/ceiling entered catalog", err)
	}
	value := models[0]
	if *value.ContextWindowTokens != 900000 || *value.AdvertisedContextTokens != 1000000 || *value.EffectiveContextPercent != 90 || *value.MaxOutputTokens != 128000 || value.Prices.Input != nil || !reflect.DeepEqual(value.ReasoningEfforts, []string{"medium", "high"}) {
		t.Fatal("subscription metadata or unknown pricing changed")
	}
	empty, err := decodeModels([]byte(`{"models":[]}`), "openai-codex", config.Provider{Kind: "openai-codex"})
	if err != nil || len(empty) != 0 {
		t.Fatal("successful empty catalog did not clear")
	}
	if _, err := decodeModels([]byte(`{"data":[`+strings.Repeat(`{"id":"m"},`, maxModels)+`{"id":"last"}]}`), "custom", noAuth()); err == nil {
		t.Fatal("catalog count unbounded")
	}
}

func TestModelRowsAreBoundedBeforeAllocationAndRejectTrailingValues(t *testing.T) {
	for _, raw := range []string{`[null,null,null]`, `[{}] {}`, `[{}] true`, `[{}]x`} {
		if _, err := decodeRows[advertisedModel]([]byte(raw), 2); err == nil {
			t.Fatal("unbounded or trailing model rows accepted")
		}
	}
	if rows, err := decodeRows[advertisedModel]([]byte(" [ {}, {} ] \n"), 2); err != nil || len(rows) != 2 {
		t.Fatal("bounded array rejected", err)
	}
}
