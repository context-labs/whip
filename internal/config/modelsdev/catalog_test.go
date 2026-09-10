package modelsdev

import "testing"

func TestProviderReturnsIndependentValues(t *testing.T) {
	provider, ok := Provider("cerebras")
	if !ok {
		t.Fatal("missing bundled provider")
	}
	provider.Env[0] = "CHANGED"
	model := provider.Models["gpt-oss-120b"]
	*model.ContextLength = 1
	*model.SupportsTools = false
	model.InputModalities[0] = "changed"
	model.ReasoningEfforts[0] = "changed"
	model.Pricing.Prompt = "0"
	delete(provider.Models, "gpt-oss-120b")
	next, ok := Provider("cerebras")
	if !ok || next.Env[0] != "CEREBRAS_API_KEY" {
		t.Fatal("provider environment mutated")
	}
	model = next.Models["gpt-oss-120b"]
	if *model.ContextLength != 131072 || !*model.SupportsTools || model.InputModalities[0] != "text" || model.ReasoningEfforts[0] != "low" || model.Pricing.Prompt != "0.00000035" {
		t.Fatalf("mutated bundled model: %+v", model)
	}
	metadata, ok := Metadata("cerebras")
	if !ok || metadata.Models != nil || metadata.Env[0] != "CEREBRAS_API_KEY" {
		t.Fatal("invalid lightweight metadata")
	}
	metadata.Env[0] = "CHANGED"
	nextMetadata, _ := Metadata("cerebras")
	if nextMetadata.Env[0] != "CEREBRAS_API_KEY" {
		t.Fatal("metadata environment mutated")
	}
}
