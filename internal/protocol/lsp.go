package protocol

import (
	"reflect"
	"strings"

	"github.com/context-labs/whip/internal/lsp"
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

func LanguageServersFromDomain(values []lsp.Status) LanguageServersResult {
	result := LanguageServersResult{Items: []LanguageServerStatus{}}
	for _, value := range values {
		row := LanguageServerStatus{Name: value.Name, State: strings.ReplaceAll(value.State, " ", "_")}
		if value.Root != "" {
			row.WorkspaceRoot = new(value.Root)
		}
		if value.Err != "" {
			row.Failure = new("Language server connection is unavailable.")
		}
		result.Items = append(result.Items, row)
	}
	return result
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
