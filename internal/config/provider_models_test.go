package config

import (
	"reflect"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/openaiauth"
)

func TestPresetModelsCoverAPIKeyPresets(t *testing.T) {
	for _, preset := range ProviderPresets() {
		if preset.ID == openaiauth.Provider {
			continue
		}
		t.Run(preset.ID, func(t *testing.T) {
			models := PresetModels(preset.ID)
			if len(models) == 0 {
				t.Fatal("preset has no reviewed offline candidates")
			}
			for _, model := range models {
				if model.ID == "" || model.ContextLength <= 0 || model.MaxCompletionTokens <= 0 || model.SupportsTools == nil || !*model.SupportsTools {
					t.Fatalf("incomplete reviewed model: %+v", model)
				}
			}
			models[0].ID = "changed"
			models[0].InputModalities[0] = "changed"
			if next := PresetModels(preset.ID)[0]; next.ID == "changed" || next.InputModalities[0] == "changed" {
				t.Fatal("caller mutated preset models")
			}
		})
	}
}

func TestCompatiblePresetModels(t *testing.T) {
	for _, preset := range ProviderPresets() {
		if preset.ID == openaiauth.Provider {
			continue
		}
		t.Run(preset.ID, func(t *testing.T) {
			known := PresetModels(preset.ID)[0]
			input := []llm.ModelInfo{
				{ID: known.ID},
				{ID: "embedding", Type: "embedding"},
				{ID: "image-generation", OutputModalities: []string{"image"}, SupportsTools: new(true)},
				{ID: "audio", OutputModalities: []string{"audio"}},
				{ID: "no-tools", SupportsTools: new(false)},
				{ID: "responses-only", Type: "responses"},
				{ID: "new-chat-model"},
				{ID: known.ID},
			}
			got := CompatiblePresetModels(preset.ID, preset.Provider, input)
			if len(got) != 2 || got[1].ID != "new-chat-model" || got[0].ID != known.ID || got[0].ContextLength != known.ContextLength || got[0].MaxCompletionTokens != known.MaxCompletionTokens {
				t.Fatalf("sparse catalog = %+v", got)
			}
			denied := known
			denied.SupportsTools = new(false)
			if got := CompatiblePresetModels(preset.ID, preset.Provider, []llm.ModelInfo{denied}); len(got) != 0 {
				t.Fatal("ignored explicit tool incompatibility")
			}
			custom := preset.Provider
			custom.BaseURL += "/custom"
			if got := CompatiblePresetModels(preset.ID, custom, input); len(got) != len(input) {
				t.Fatal("preset ID changed custom catalog")
			}
			if got := CompatiblePresetModels("custom", preset.Provider, input); len(got) != len(input) {
				t.Fatal("canonical URL alone changed custom catalog")
			}
		})
	}
}

func TestCompatiblePresetRouterMetadata(t *testing.T) {
	provider := Provider{Name: "OpenRouter", BaseURL: OpenRouterBaseURL, API: "openai-completions"}
	model := llm.ModelInfo{ID: "new-provider/new-model", SupportsTools: new(true), OutputModalities: []string{"text"}, ContextLength: 32000, MaxCompletionTokens: 8000}
	got := CompatiblePresetModels("openrouter", provider, []llm.ModelInfo{model})
	if len(got) != 1 || got[0].ID != model.ID {
		t.Fatalf("explicit compatible metadata ignored: %+v", got)
	}
	model.OutputModalities = nil
	if len(CompatiblePresetModels("openrouter", provider, []llm.ModelInfo{model})) != 1 {
		t.Fatal("missing metadata hid a live model")
	}
}

func TestLiveModelMetadataIsAuthoritative(t *testing.T) {
	provider := Provider{BaseURL: "https://api.cerebras.ai/v1", API: "openai-completions"}
	input := []llm.ModelInfo{{
		ID: "gpt-oss-120b", ContextLength: 64000, MaxCompletionTokens: 8000,
		ReasoningEfforts: []string{}, InputModalities: []string{"text"},
		Pricing:       &llm.Pricing{Prompt: "0", Completion: "0"},
		SupportsTools: new(true), OutputModalities: []string{"text"},
	}, {ID: "future-qwen-whip-test"}}
	if got := CompatiblePresetModels("cerebras", provider, input); !reflect.DeepEqual(got, input) {
		t.Fatalf("live metadata changed: %+v", got)
	}
	if got := CompatiblePresetModels("cerebras", provider, input[1:]); len(got) != 1 || got[0].ID != input[1].ID {
		t.Fatalf("bundled model absent from live catalog was added: %+v", got)
	}
	for _, input := range [][]llm.ModelInfo{nil, {}, {{ID: "gpt-oss-120b", OutputModalities: []string{}}}} {
		if got := CompatiblePresetModels("cerebras", provider, input); len(got) != 0 {
			t.Fatalf("empty or incompatible catalog gained models: %+v", got)
		}
	}
}

func TestPresetDiscoveryTransportExceptions(t *testing.T) {
	provider := Provider{BaseURL: "https://api.openai.com/v1", API: "openai-completions"}
	for _, id := range []string{
		"text-embedding-3-large", "gpt-image-1", "gpt-4o-audio-preview", "gpt-realtime",
		"gpt-5.2-codex", "gpt-5-pro-2025-10-06", "o3-pro", "o1-mini", "gpt-4o-search-preview",
	} {
		input := []llm.ModelInfo{{ID: id}}
		if got := CompatiblePresetModels("openai", provider, input); len(got) != 0 {
			t.Errorf("unsupported OpenAI API route exposed: %s", id)
		}
		router := Provider{BaseURL: OpenRouterBaseURL, API: "openai-completions"}
		if got := CompatiblePresetModels("openrouter", router, input); len(got) != 1 {
			t.Errorf("native transport exception affected gateway: %s", id)
		}
	}
	for _, id := range []string{"gpt-6-astra", "gpt-4.1-nano", "future-chat-model"} {
		if got := CompatiblePresetModels("openai", provider, []llm.ModelInfo{{ID: id}}); len(got) != 1 {
			t.Errorf("live OpenAI model absent from preset was hidden: %s", id)
		}
	}
	deepseek := Provider{BaseURL: "https://api.deepseek.com", API: "openai-completions"}
	if got := CompatiblePresetModels("deepseek", deepseek, []llm.ModelInfo{{ID: "deepseek-reasoner"}}); len(got) != 0 {
		t.Fatal("unreplayable reasoning transport was exposed")
	}
}

func TestModelsDevEnrichmentPreservesLiveValues(t *testing.T) {
	provider := Provider{BaseURL: OpenRouterBaseURL, API: "openai-completions"}
	model := llm.ModelInfo{ID: "z-ai/glm-5.3", ContextLength: 123, MaxCompletionTokens: 45, ReasoningEfforts: []string{}, InputModalities: []string{}, OutputModalities: []string{"text"}, SupportsTools: new(true), Pricing: &llm.Pricing{Prompt: "0", Completion: "0"}}
	got := CompatiblePresetModels("openrouter", provider, []llm.ModelInfo{model})
	if len(got) != 1 || got[0].ContextLength != 123 || got[0].MaxCompletionTokens != 45 || got[0].ReasoningEfforts == nil || len(got[0].ReasoningEfforts) != 0 || got[0].InputModalities == nil || len(got[0].InputModalities) != 0 || got[0].Pricing.Prompt != "0" || got[0].Pricing.Completion != "0" {
		t.Fatalf("live metadata replaced: %+v", got)
	}
	if got[0].Pricing.InputCacheRead != "0.00000026" {
		t.Fatalf("missing price was not enriched: %+v", got[0].Pricing)
	}
	if model.Pricing.InputCacheRead != "" {
		t.Fatal("enrichment mutated caller pricing")
	}
	got = CompatiblePresetModels("openrouter", provider, []llm.ModelInfo{{ID: model.ID}})
	if len(got) != 1 || got[0].ContextLength != 1310720 || got[0].Pricing.Prompt != "0.0000014" {
		t.Fatalf("exact-ID metadata not applied: %+v", got)
	}
	unknown := llm.ModelInfo{ID: "other/glm-5.3"}
	got = CompatiblePresetModels("openrouter", provider, []llm.ModelInfo{unknown})
	if !reflect.DeepEqual(got, []llm.ModelInfo{unknown}) {
		t.Fatal("metadata matched a model basename")
	}
}

func TestEnrichPresetCatalogPreservesMembershipAndFreshness(t *testing.T) {
	provider := Provider{BaseURL: "https://api.cerebras.ai/v1", API: "openai-completions"}
	catalog := Catalog{FetchedAt: time.Unix(100, 0), BaseURL: provider.BaseURL, Models: []ModelInfoLite{{ID: "qwen-3.8-27b", ReasoningEfforts: []string{}, InputModalities: []string{}, Pricing: llm.Pricing{Prompt: "0"}}, {ID: "new-model"}}}
	got := EnrichPresetCatalog("cerebras", provider, catalog)
	if !got.FetchedAt.Equal(catalog.FetchedAt) || len(got.Models) != 2 || got.Models[1].ID != "new-model" || got.Models[1].ContextLength != 0 {
		t.Fatalf("cache membership or freshness changed: %+v", got)
	}
	model := got.Models[0]
	if model.ContextLength != 65536 || model.ReasoningEfforts == nil || len(model.ReasoningEfforts) != 0 || model.InputModalities == nil || len(model.InputModalities) != 0 || model.Pricing.Prompt != "0" || model.Pricing.Completion != "0.00000149" {
		t.Fatalf("cache enrichment: %+v", model)
	}
	if catalog.Models[0].ContextLength != 0 {
		t.Fatal("mutated shared cache")
	}
	provider.BaseURL += "/custom"
	if got := EnrichPresetCatalog("cerebras", provider, catalog); !reflect.DeepEqual(got, catalog) {
		t.Fatal("custom destination enriched")
	}
}
