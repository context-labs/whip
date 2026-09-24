package llm

import "testing"

func TestDecodeModelsProviderMetadata(t *testing.T) {
	tests := []struct {
		name            string
		body            string
		context, output int
		tools           *bool
		vision          bool
		typeName        string
	}{
		{name: "router", body: `{"data":[{"id":"model","supported_parameters":["tools","tool_choice"],"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"top_provider":{"context_length":32000,"max_completion_tokens":4000}}]}`, context: 32000, output: 4000, tools: new(true), vision: true},
		{name: "router no tools", body: `{"data":[{"id":"model","supported_parameters":[],"architecture":{"output_modalities":["text"]}}]}`, tools: new(false)},
		{name: "deepinfra", body: `{"data":[{"id":"model","metadata":{"context_length":64000,"max_tokens":64000}}]}`, context: 64000},
		{name: "deepinfra chat", body: `{"data":[{"id":"model","metadata":{"tags":["vision","reasoning","chat"]}}]}`, typeName: "chat"},
		{name: "deepinfra embedding", body: `{"data":[{"id":"model","metadata":{"tags":["embed"]}}]}`, typeName: "embed"},
		{name: "deepinfra image", body: `{"data":[{"id":"model","metadata":{"tags":["image-gen"]}}]}`, typeName: "image-gen"},
		{name: "deepinfra video", body: `{"data":[{"id":"model","metadata":{"tags":["video-gen"]}}]}`, typeName: "video-gen"},
		{name: "deepinfra tts", body: `{"data":[{"id":"model","metadata":{"tags":["tts"]}}]}`, typeName: "tts"},
		{name: "deepinfra stt", body: `{"data":[{"id":"model","metadata":{"tags":["stt"]}}]}`, typeName: "stt"},
		{name: "unknown tags", body: `{"data":[{"id":"model","metadata":{"tags":["new-feature"]}}]}`},
		{name: "together", body: `[{"id":"model","type":"chat","context_length":128000}]`, context: 128000, typeName: "chat"},
		{name: "generic", body: `{"data":[{"id":"model","context_length":24000,"max_completion_tokens":1000}]}`, context: 24000, output: 1000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := decodeModels([]byte(test.body))
			if err != nil || len(got) != 1 {
				t.Fatalf("models = %+v, %v", got, err)
			}
			model := got[0]
			if model.ContextLength != test.context || model.MaxCompletionTokens != test.output || model.SupportsVision() != test.vision || model.Type != test.typeName {
				t.Fatalf("metadata = %+v", model)
			}
			if (model.SupportsTools == nil) != (test.tools == nil) || model.SupportsTools != nil && *model.SupportsTools != *test.tools {
				t.Fatalf("tool support = %v", model.SupportsTools)
			}
		})
	}
}
