package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type LanguageServerStatus struct {
	Name          string  `json:"name"`
	State         string  `json:"state" enum:"connected,not_started,failed"`
	WorkspaceRoot *string `json:"workspace_root"`
	Failure       *string `json:"failure"`
}

type LanguageServersResult struct {
	Items []LanguageServerStatus `json:"items"`
}

func languageServerSchema(schema *jsonschema.Schema, t reflect.Type) {
	if t == reflect.TypeFor[LanguageServersResult]() {
		field := schema.Properties["items"]
		field.Type = "array"
		field.Types = nil
		field.MaxItems = new(16)
	}
	if t == reflect.TypeFor[LanguageServerStatus]() {
		schema.Properties["name"].MaxLength = new(128)
		schema.Properties["workspace_root"].MaxLength = new(4096)
		schema.Properties["failure"].MaxLength = new(256)
	}
}
