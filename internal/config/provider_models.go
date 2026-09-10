package config

import (
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/config/modelsdev"
	"github.com/context-labs/whip/internal/llm"
)

// CanonicalProviderPreset requires both the saved ID and its exact destination.
// A user-defined route that reuses a preset ID keeps its own model behavior.
func CanonicalProviderPreset(id string, provider Provider) (ProviderPreset, bool) {
	preset, ok := canonicalPreset(provider)
	return preset, ok && preset.ID == id
}

// PresetModels provides offline candidates from the bundled catalog. Live
// membership remains authoritative; this list never grants account access.
func PresetModels(id string) []llm.ModelInfo {
	models := presetMetadata(id)
	result := make([]llm.ModelInfo, 0, len(models))
	for _, model := range models {
		if model.SupportsTools == nil || !*model.SupportsTools || !slices.Contains(model.OutputModalities, "text") || model.ContextLength <= 0 || model.MaxCompletionTokens <= 0 || !presetModelTransportSupported(id, model.ID) {
			continue
		}
		if id == "deepseek" {
			model.ReasoningEfforts = nil
		}
		result = append(result, model)
	}
	slices.SortFunc(result, func(a, b llm.ModelInfo) int { return strings.Compare(a.ID, b.ID) })
	return result
}

// RetainedPresetModels contains explicit policy exceptions absent upstream.
// Inference.net's current Kimi routes are newer than its Models.dev entry.
func RetainedPresetModels(id string) []llm.ModelInfo {
	if id != InferenceNetProvider {
		return nil
	}
	return []llm.ModelInfo{
		{ID: "kimi-k3-fast", ContextLength: 1048576, MaxCompletionTokens: 1048576, ReasoningEfforts: []string{"low", "medium", "high"}, InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, SupportsTools: new(true)},
		{ID: "kimi-k3", ContextLength: 1048576, MaxCompletionTokens: 131072, InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, SupportsTools: new(true)},
	}
}

func presetMetadata(id string) map[string]llm.ModelInfo {
	metadata, _ := modelsdev.Provider(ModelsDevProviderID(id))
	models := make(map[string]llm.ModelInfo, len(metadata.Models))
	for id, source := range metadata.Models {
		model := llm.ModelInfo{ID: id, SupportsTools: source.SupportsTools, InputModalities: source.InputModalities, OutputModalities: source.OutputModalities, ReasoningEfforts: source.ReasoningEfforts}
		if source.ContextLength != nil {
			model.ContextLength = *source.ContextLength
		}
		if source.MaxCompletionTokens != nil {
			model.MaxCompletionTokens = *source.MaxCompletionTokens
		}
		if source.Reasoning != nil && !*source.Reasoning {
			model.ReasoningEfforts = []string{}
		}
		if source.Pricing != nil {
			model.Pricing = &llm.Pricing{Prompt: source.Pricing.Prompt, Completion: source.Pricing.Completion, InputCacheRead: source.Pricing.InputCacheRead}
		}
		models[id] = model
	}
	for _, model := range RetainedPresetModels(id) {
		// Once upstream gains the route, its metadata can replace the retained fallback.
		if _, found := models[model.ID]; !found {
			models[model.ID] = model
		}
	}
	return models
}

func enrichModel(model, base llm.ModelInfo) llm.ModelInfo {
	if model.ContextLength == 0 {
		model.ContextLength = base.ContextLength
	}
	if model.MaxCompletionTokens == 0 {
		model.MaxCompletionTokens = base.MaxCompletionTokens
	}
	if model.InputModalities == nil {
		model.InputModalities = base.InputModalities
	}
	if model.OutputModalities == nil {
		model.OutputModalities = base.OutputModalities
	}
	if model.ReasoningEfforts == nil {
		model.ReasoningEfforts = base.ReasoningEfforts
	}
	if model.SupportsTools == nil {
		model.SupportsTools = base.SupportsTools
	}
	if base.Pricing != nil {
		pricing := llm.Pricing{}
		if model.Pricing != nil {
			pricing = *model.Pricing
		}
		if pricing.Prompt == "" {
			pricing.Prompt = base.Pricing.Prompt
		}
		if pricing.Completion == "" {
			pricing.Completion = base.Pricing.Completion
		}
		if pricing.InputCacheRead == "" {
			pricing.InputCacheRead = base.Pricing.InputCacheRead
		}
		model.Pricing = &pricing
	}
	return model
}

// EnrichPresetCatalog fills missing exact-ID metadata without changing cached
// membership or freshness. It never applies metadata to a custom destination.
func EnrichPresetCatalog(id string, provider Provider, catalog Catalog) Catalog {
	if catalog.BaseURL != "" && strings.TrimRight(catalog.BaseURL, "/") != strings.TrimRight(provider.BaseURL, "/") {
		return catalog
	}
	if _, ok := CanonicalProviderPreset(id, provider); !ok {
		return catalog
	}
	metadata := presetMetadata(id)
	catalog.Models = slices.Clone(catalog.Models)
	for i, value := range catalog.Models {
		base, found := metadata[value.ID]
		if !found {
			continue
		}
		model := enrichModel(llm.ModelInfo{ID: value.ID, ContextLength: value.ContextLength, MaxCompletionTokens: value.MaxCompletionTokens, ReasoningEfforts: value.ReasoningEfforts, InputModalities: value.InputModalities, Pricing: &value.Pricing}, base)
		value.ContextLength, value.MaxCompletionTokens = model.ContextLength, model.MaxCompletionTokens
		value.ReasoningEfforts, value.InputModalities = model.ReasoningEfforts, model.InputModalities
		if id == "deepseek" {
			value.ReasoningEfforts = nil
		}
		if model.Pricing != nil {
			value.Pricing = *model.Pricing
		}
		catalog.Models[i] = value
	}
	return catalog
}

// CompatiblePresetModels filters explicit incompatibilities on canonical presets.
// Live membership is authoritative, including models with sparse metadata.
// Bundled entries only supplement missing metadata; they are not an allowlist.
// Saved aliases remain configured independently of this discovery catalog.
func CompatiblePresetModels(id string, provider Provider, discovered []llm.ModelInfo) []llm.ModelInfo {
	if _, ok := CanonicalProviderPreset(id, provider); !ok {
		return discovered
	}
	reviewed := presetMetadata(id)
	result := make([]llm.ModelInfo, 0, len(discovered))
	seen := map[string]bool{}
	for _, model := range discovered {
		if model.ID == "" || seen[model.ID] {
			continue
		}
		if model.SupportsTools != nil && !*model.SupportsTools {
			continue
		}
		nonChat := model.Type != "" && model.Type != "chat"
		nonText := model.OutputModalities != nil && !slices.Contains(model.OutputModalities, "text")
		if nonChat || nonText {
			continue
		}
		if !presetModelTransportSupported(id, model.ID) {
			continue
		}
		if base, found := reviewed[model.ID]; found {
			model = enrichModel(model, base)
		}
		if id == "deepseek" {
			model.ReasoningEfforts = nil
		}
		seen[model.ID] = true
		result = append(result, model)
	}
	return result
}

// Sparse catalogs cannot express every API restriction. Keep these exceptions
// scoped to canonical routes: gateways may support the same IDs through chat.
func presetModelTransportSupported(provider, model string) bool {
	if provider == "deepseek" {
		// This route requires reasoning_content replay, unlike V4's supported
		// explicit non-thinking mode.
		return model != "deepseek-reasoner"
	}
	if provider != "openai" {
		return true
	}
	// OpenAI's /models mixes chat with specialist APIs and Responses-only
	// models. Astra has its own Responses adapter; these routes do not.
	for _, prefix := range []string{
		"text-", "dall-e-", "tts-", "whisper-", "sora-", "omni-moderation",
		"babbage-", "davinci-", "computer-use-", "codex-",
	} {
		if strings.HasPrefix(model, prefix) {
			return false
		}
	}
	for _, part := range []string{
		"-embedding", "-image", "-audio", "-realtime", "-transcribe", "-search",
		"-codex", "-deep-research", "-instruct",
	} {
		if strings.Contains(model, part) {
			return false
		}
	}
	for _, prefix := range []string{"gpt-5-pro", "gpt-5.2-pro", "o1-pro", "o3-pro", "o1-mini", "o1-preview"} {
		if model == prefix || strings.HasPrefix(model, prefix+"-") {
			return false
		}
	}
	return true
}
