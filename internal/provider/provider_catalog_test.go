package provider

import (
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
)

func TestModelInfoLitesPreservesAdvertisedMetadata(t *testing.T) {
	for _, test := range []struct {
		name  string
		input []llm.ModelInfo
		want  []config.ModelInfoLite
	}{
		{name: "absent catalog", want: []config.ModelInfoLite{}},
		{name: "empty catalog", input: []llm.ModelInfo{}, want: []config.ModelInfoLite{}},
		{name: "unknown metadata", input: []llm.ModelInfo{{ID: "unknown"}}, want: []config.ModelInfoLite{{ID: "unknown"}}},
		{
			name:  "explicitly empty capabilities and free pricing",
			input: []llm.ModelInfo{{ID: "free", ReasoningEfforts: []string{}, InputModalities: []string{}, Pricing: &llm.Pricing{Prompt: "0", Completion: "0", InputCacheRead: "0"}}},
			want:  []config.ModelInfoLite{{ID: "free", ReasoningEfforts: []string{}, InputModalities: []string{}, Pricing: llm.Pricing{Prompt: "0", Completion: "0", InputCacheRead: "0"}}},
		},
		{
			name:  "exact pricing and provider order",
			input: []llm.ModelInfo{{ID: "z", ContextLength: 1234, MaxCompletionTokens: 567, ReasoningEfforts: []string{"future", "high"}, InputModalities: []string{"image", "text"}, Pricing: &llm.Pricing{Prompt: "0.1234567890123456789012345678", Completion: "0.000005", InputCacheRead: "0"}}, {ID: "a", Pricing: &llm.Pricing{Prompt: "0"}}},
			want:  []config.ModelInfoLite{{ID: "z", ContextLength: 1234, MaxCompletionTokens: 567, ReasoningEfforts: []string{"future", "high"}, InputModalities: []string{"image", "text"}, Pricing: llm.Pricing{Prompt: "0.1234567890123456789012345678", Completion: "0.000005", InputCacheRead: "0"}}, {ID: "a", Pricing: llm.Pricing{Prompt: "0"}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := modelInfoLites(test.input)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("cached models = %#v; want %#v", got, test.want)
			}
		})
	}
}
