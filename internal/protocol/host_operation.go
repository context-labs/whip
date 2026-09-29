package protocol

import (
	"encoding/json"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// DirectHostInput preserves exact argument bytes, including large JSON integers.
type DirectHostInput struct {
	Module          string `json:"module" enum:"shell,files,tools,computer"`
	Name            ID     `json:"name"`
	ArgumentsBase64 string `json:"arguments_base64"`
}

type CallHostToolParams struct {
	Identity  RequestIdentity `json:"identity"`
	SessionID ID              `json:"session_id"`
	Operation DirectHostInput `json:"operation"`
}

type RunShellParams struct {
	Identity    RequestIdentity `json:"identity"`
	SessionID   ID              `json:"session_id"`
	Command     string          `json:"command"`
	Timeout     *float64        `json:"timeout,omitempty" min:"0.001" max:"120"`
	Interactive bool            `json:"interactive"`
}

type HostToolSchema struct {
	Module      string          `json:"module" enum:"shell,files,tools,computer"`
	Name        ID              `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type HostToolSchemasResult struct {
	Items []HostToolSchema `json:"items"`
}

func hostOperationSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[DirectHostInput]():
		schema.Properties["arguments_base64"].MaxLength = new(699052)
	case reflect.TypeFor[RunShellParams]():
		schema.Properties["command"].MinLength = new(1)
		schema.Properties["command"].MaxLength = new(65536)
	case reflect.TypeFor[HostToolSchemasResult]():
		schema.Properties["items"].Type, schema.Properties["items"].Types = "array", nil
		schema.Properties["items"].MaxItems = new(136)
	case reflect.TypeFor[HostOperation]():
		schema.OneOf = []*jsonschema.Schema{
			{Properties: map[string]*jsonschema.Schema{"origin": {Enum: []any{"cell"}}, "cell_id": {Not: &jsonschema.Schema{Type: "null"}}}},
			{Properties: map[string]*jsonschema.Schema{"origin": {Enum: []any{"host_operation"}}, "cell_id": {Type: "null"}}},
		}
	case reflect.TypeFor[Configuration]():
		configured := schema.Properties["model"]
		empty := configured.CloneSchemas()
		empty.Properties["provider"] = &jsonschema.Schema{Type: "string", Enum: []any{""}}
		empty.Properties["name"] = &jsonschema.Schema{Type: "string", Enum: []any{""}}
		empty.Properties["effort"] = &jsonschema.Schema{Type: "string", Enum: []any{""}}
		empty.Not = &jsonschema.Schema{AnyOf: []*jsonschema.Schema{{Required: []string{"temperature"}}, {Required: []string{"top_p"}}}}
		schema.Properties["model"] = &jsonschema.Schema{OneOf: []*jsonschema.Schema{configured, empty}}
	}
}
