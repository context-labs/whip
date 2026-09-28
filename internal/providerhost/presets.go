package providerhost

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/openaiauth"
)

type Preset struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	BaseURL         string   `json:"base_url"`
	Methods         []string `json:"methods"`
	Environments    []string `json:"environments"`
	KeyURL          string   `json:"key_url"`
	SuggestedModels []string `json:"suggested_models"`
	SuggestedEffort string   `json:"suggested_effort"`
}

// Presets are setup templates, never discovered/configured routes. No ambient
// credential can change their endpoint, add a route, or select a default.
func Presets() []Preset {
	return []Preset{
		{ID: "inference-net", Name: "Inference.net", Kind: "openai-chat", BaseURL: "https://api.inference.net/v1", Methods: []string{"login", "api_key"}, Environments: []string{"INFERENCE_API_KEY"}, KeyURL: "https://inference.net/dashboard", SuggestedModels: []string{"kimi-k3-fast", "kimi-k3"}, SuggestedEffort: "high"},
		{ID: "openrouter", Name: "OpenRouter", Kind: "openai-chat", BaseURL: "https://openrouter.ai/api/v1", Methods: []string{"api_key"}, Environments: []string{"OPENROUTER_API_KEY"}, KeyURL: "https://openrouter.ai/settings/keys", SuggestedModels: []string{"z-ai/glm-5.3", "moonshotai/kimi-k3", "moonshotai/kimi-k2.5", "anthropic/claude-sonnet-4.6"}, SuggestedEffort: "max"},
		{ID: "openai", Name: "OpenAI", Kind: "openai-responses", BaseURL: "https://api.openai.com/v1", Methods: []string{"api_key"}, Environments: []string{"OPENAI_API_KEY"}, KeyURL: "https://platform.openai.com/api-keys", SuggestedModels: []string{"gpt-6-astra"}, SuggestedEffort: "medium"},
		{ID: "openai-codex", Name: "OpenAI (ChatGPT subscription)", Kind: "openai-codex", Methods: []string{"login"}, SuggestedModels: []string{"gpt-6-astra", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.5", "gpt-5.3-codex"}, SuggestedEffort: "medium"},
		{ID: "cerebras", Name: "Cerebras", Kind: "openai-chat", BaseURL: "https://api.cerebras.ai/v1", Methods: []string{"api_key"}, Environments: []string{"CEREBRAS_API_KEY"}, KeyURL: "https://cloud.cerebras.ai/platform/api-keys"},
		{ID: "deepinfra", Name: "DeepInfra", Kind: "openai-chat", BaseURL: "https://api.deepinfra.com/v1/openai", Methods: []string{"api_key"}, Environments: []string{"DEEPINFRA_API_KEY", "DEEPINFRA_TOKEN"}, KeyURL: "https://deepinfra.com/dash/api_keys"},
		{ID: "deepseek", Name: "DeepSeek", Kind: "openai-chat", BaseURL: "https://api.deepseek.com", Methods: []string{"api_key"}, Environments: []string{"DEEPSEEK_API_KEY"}, KeyURL: "https://platform.deepseek.com/api_keys"},
		{ID: "fireworks-ai", Name: "Fireworks AI", Kind: "openai-chat", BaseURL: "https://api.fireworks.ai/inference/v1", Methods: []string{"api_key"}, Environments: []string{"FIREWORKS_API_KEY"}, KeyURL: "https://app.fireworks.ai/settings/users/api-keys"},
		{ID: "groq", Name: "Groq", Kind: "openai-chat", BaseURL: "https://api.groq.com/openai/v1", Methods: []string{"api_key"}, Environments: []string{"GROQ_API_KEY"}, KeyURL: "https://console.groq.com/keys"},
		{ID: "togetherai", Name: "Together AI", Kind: "openai-chat", BaseURL: "https://api.together.ai/v1", Methods: []string{"api_key"}, Environments: []string{"TOGETHER_API_KEY"}, KeyURL: "https://api.together.ai/settings/api-keys"},
		{ID: "xai", Name: "xAI", Kind: "openai-chat", BaseURL: "https://api.x.ai/v1", Methods: []string{"api_key"}, Environments: []string{"XAI_API_KEY"}, KeyURL: "https://console.x.ai/"},
	}
}

func canonical(id string, p config.Provider) bool {
	for _, preset := range Presets() {
		if preset.ID == id && preset.BaseURL == strings.TrimRight(p.BaseURL, "/") && (preset.Kind == p.Kind || id == "openai" && p.Kind == "openai-chat") {
			return true
		}
	}
	return false
}

// This reviewed 2026-09-10 Models.dev artifact is retained verbatim. Its origin,
// retrieval time, and input digest live in the artifact; it is never fetched
// implicitly or used as authority for live account membership.
//
//go:embed catalog.json
var bundledJSON []byte

type bundledModel struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Context          *int64   `json:"contextLength"`
	Output           *int64   `json:"maxCompletionTokens"`
	Tools            *bool    `json:"supportsTools"`
	Reasoning        *bool    `json:"reasoning"`
	Efforts          []string `json:"reasoningEfforts"`
	Input            []string `json:"inputModalities"`
	OutputModalities []string `json:"outputModalities"`
	Pricing          struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
		Cache      string `json:"inputCacheRead"`
	} `json:"pricing"`
}

var bundled = sync.OnceValues(func() (map[string]map[string]Model, error) {
	var snapshot struct {
		Providers map[string]struct {
			Models map[string]bundledModel `json:"models"`
		} `json:"providers"`
	}
	if json.Unmarshal(bundledJSON, &snapshot) != nil {
		return nil, ErrDiscovery
	}
	result := map[string]map[string]Model{}
	for id, provider := range snapshot.Providers {
		if id == "inference" {
			id = "inference-net"
		}
		models := map[string]Model{}
		for modelID, raw := range provider.Models {
			prices, err := parsePrices(map[string]json.RawMessage{"prompt": quoted(raw.Pricing.Prompt), "completion": quoted(raw.Pricing.Completion), "input_cache_read": quoted(raw.Pricing.Cache)}, false)
			if err != nil {
				return nil, err
			}
			value := Model{ID: modelID, Name: raw.Name, Prices: prices, ContextWindowTokens: raw.Context, MaxOutputTokens: raw.Output, ReasoningEfforts: raw.Efforts, InputModalities: raw.Input, OutputModalities: raw.OutputModalities, SupportsTools: raw.Tools, MetadataSource: "bundled"}
			if raw.Reasoning != nil && !*raw.Reasoning {
				value.ReasoningEfforts = []string{}
			}
			models[modelID] = value
		}
		result[id] = models
	}
	if result["inference-net"] == nil {
		result["inference-net"] = map[string]Model{}
	}
	for _, value := range []Model{
		{ID: "kimi-k3-fast", ContextWindowTokens: new(int64(1048576)), MaxOutputTokens: new(int64(1048576)), ReasoningEfforts: []string{"low", "medium", "high"}, InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, SupportsTools: new(true), MetadataSource: "bundled"},
		{ID: "kimi-k3", ContextWindowTokens: new(int64(1048576)), MaxOutputTokens: new(int64(131072)), InputModalities: []string{"text", "image"}, OutputModalities: []string{"text"}, SupportsTools: new(true), MetadataSource: "bundled"},
	} {
		if _, exists := result["inference-net"][value.ID]; !exists {
			result["inference-net"][value.ID] = value
		}
	}
	return result, nil
})

func quoted(value string) json.RawMessage {
	if value == "" {
		return nil
	}
	raw, _ := json.Marshal(value)
	return raw
}

// BundledModels is a separate, clearly unverified offline view. It never
// replaces successful live membership, including a successfully empty list.
func BundledModels(id string) ([]Model, error) {
	if id == openaiauth.Provider {
		return []Model{}, nil
	}
	all, err := bundled()
	if err != nil {
		return nil, err
	}
	result := []Model{}
	kind := "openai-chat"
	if id == "openai" {
		kind = "openai-responses"
	}
	for _, value := range all[id] {
		if value.SupportsTools == nil || !*value.SupportsTools || !slices.Contains(value.OutputModalities, "text") || value.ContextWindowTokens == nil || value.MaxOutputTokens == nil || !transportSupported(id, value.ID, kind) {
			continue
		}
		if id == "deepseek" {
			value.ReasoningEfforts = []string{}
		}
		result = append(result, cloneModel(value))
	}
	slices.SortFunc(result, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	return result, nil
}
