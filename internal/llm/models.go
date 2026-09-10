package llm

import (
	"bytes"
	"encoding/json"
	"slices"
)

// decodeModels accepts the standard envelope and Together's documented array.
// Provider extensions describe capabilities; callers decide which to expose.
func decodeModels(body []byte) ([]ModelInfo, error) {
	type model struct {
		ModelInfo
		SupportedParameters []string `json:"supported_parameters"`
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
		if value.ContextLength == 0 {
			value.ContextLength = max(row.TopProvider.Context, row.Metadata.Context)
		}
		if value.MaxCompletionTokens == 0 {
			value.MaxCompletionTokens = row.TopProvider.Output
		}
		if value.InputModalities == nil {
			value.InputModalities = row.Architecture.Input
		}
		if value.OutputModalities == nil {
			value.OutputModalities = row.Architecture.Output
		}
		if value.SupportsTools == nil && row.SupportedParameters != nil {
			value.SupportsTools = new(slices.Contains(row.SupportedParameters, "tools"))
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
