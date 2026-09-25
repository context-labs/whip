package llm

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
)

// decodeModels accepts the standard envelope and Together's documented array.
// Provider extensions describe capabilities; callers decide which to expose.
func decodeModels(body []byte) ([]ModelInfo, error) {
	type model struct {
		ModelInfo
		// Pricing is an object on most routers and a list of tiers on Requesty.
		Pricing             json.RawMessage `json:"pricing"`
		SupportedParameters []string        `json:"supported_parameters"`
		Architecture        struct {
			Input  []string `json:"input_modalities"`
			Output []string `json:"output_modalities"`
		} `json:"architecture"`
		TopProvider struct {
			Context int `json:"context_length"`
			Output  int `json:"max_completion_tokens"`
		} `json:"top_provider"`
		Metadata struct {
			Context int      `json:"context_length"`
			Tags    []string `json:"tags"`
		} `json:"metadata"`
		// Requesty's flat catalog fields; prices are USD per token.
		API             string   `json:"api"`
		ContextWindow   int      `json:"context_window"`
		MaxOutputTokens int      `json:"max_output_tokens"`
		InputPrice      *float64 `json:"input_price"`
		OutputPrice     *float64 `json:"output_price"`
		CachedPrice     *float64 `json:"cached_price"`
		ToolCalling     *bool    `json:"supports_tool_calling"`
		Vision          *bool    `json:"supports_vision"`
	}
	var rows []model
	if bytes.HasPrefix(bytes.TrimSpace(body), []byte("[")) {
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
	} else {
		var list struct {
			Data []model `json:"data"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return nil, err
		}
		rows = list.Data
	}
	result := make([]ModelInfo, 0, len(rows))
	for _, row := range rows {
		value := row.ModelInfo
		if value.ID == "" {
			continue
		}
		if bytes.HasPrefix(bytes.TrimSpace(row.Pricing), []byte("{")) {
			var pricing Pricing
			if err := json.Unmarshal(row.Pricing, &pricing); err != nil {
				return nil, err
			}
			value.Pricing = &pricing
		} else if row.InputPrice != nil && row.OutputPrice != nil {
			value.Pricing = &Pricing{Prompt: perToken(*row.InputPrice), Completion: perToken(*row.OutputPrice)}
			if row.CachedPrice != nil {
				value.Pricing.InputCacheRead = perToken(*row.CachedPrice)
			}
		}
		if value.ContextLength == 0 {
			value.ContextLength = max(row.TopProvider.Context, row.Metadata.Context, row.ContextWindow)
		}
		if value.MaxCompletionTokens == 0 {
			value.MaxCompletionTokens = max(row.TopProvider.Output, row.MaxOutputTokens)
		}
		if value.InputModalities == nil {
			value.InputModalities = row.Architecture.Input
		}
		if value.InputModalities == nil && row.Vision != nil {
			value.InputModalities = []string{"text"}
			if *row.Vision {
				value.InputModalities = append(value.InputModalities, "image")
			}
		}
		if value.OutputModalities == nil {
			value.OutputModalities = row.Architecture.Output
		}
		if value.SupportsTools == nil && row.SupportedParameters != nil {
			value.SupportsTools = new(slices.Contains(row.SupportedParameters, "tools"))
		}
		if value.SupportsTools == nil {
			value.SupportsTools = row.ToolCalling
		}
		if value.Type == "" {
			value.Type = row.API
		}
		// DeepInfra's catalog includes non-chat APIs. Only recognized category
		// tags imply a type; absent tags or unrelated new tags remain unknown.
		if value.Type == "" {
			for _, kind := range []string{"chat", "embed", "image-gen", "video-gen", "tts", "stt"} {
				if slices.Contains(row.Metadata.Tags, kind) {
					value.Type = kind
					break
				}
			}
		}
		result = append(result, value)
	}
	return result, nil
}

// perToken renders a numeric USD rate in the decimal string shape of Pricing.
func perToken(rate float64) string {
	return strconv.FormatFloat(rate, 'f', -1, 64)
}
