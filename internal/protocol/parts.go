package protocol

import "github.com/google/jsonschema-go/jsonschema"

func idSchema() *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Pattern: `^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`}
}

func closedObject(required []string, properties map[string]*jsonschema.Schema) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type: "object", Required: required, Properties: properties,
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
}

func toolCallSchema() *jsonschema.Schema {
	return closedObject([]string{"id", "name", "arguments"}, map[string]*jsonschema.Schema{
		"id": idSchema(), "name": {Type: "string", Pattern: `^[a-zA-Z0-9_-]{1,64}$`},
		"arguments": {Type: "object", AdditionalProperties: &jsonschema.Schema{}},
	})
}

func toolResultSchema() *jsonschema.Schema {
	return closedObject([]string{"call_id", "output", "is_error"}, map[string]*jsonschema.Schema{
		"call_id": idSchema(), "output": {Type: "string"}, "is_error": {Type: "boolean"},
	})
}

func partSchema(kinds ...string) *jsonschema.Schema {
	result := &jsonschema.Schema{}
	for _, kind := range kinds {
		var field string
		var value *jsonschema.Schema
		switch kind {
		case "text":
			field, value = "text", &jsonschema.Schema{Type: "string", Pattern: `^[\s\S]+$`}
		case "content":
			field, value = "reference_id", idSchema()
		case "tool_call":
			field, value = "call", toolCallSchema()
		case "tool_result":
			field, value = "result", toolResultSchema()
		}
		result.OneOf = append(result.OneOf, closedObject([]string{"type", field}, map[string]*jsonschema.Schema{
			"type": {Type: "string", Enum: []any{kind}}, field: value,
		}))
	}
	return result
}
