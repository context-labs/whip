package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type ListTreesParams struct {
	After    *ID   `json:"after,omitempty"`
	Archived *bool `json:"archived,omitempty"`
	Pinned   *bool `json:"pinned,omitempty"`
	Limit    int   `json:"limit" min:"1" max:"100"`
}

type TreeSummary struct {
	Tree   Tree `json:"tree"`
	RootID ID   `json:"root_id"`
}

type ListTreesResult struct {
	Items      []TreeSummary `json:"items"`
	NextCursor *ID           `json:"next_cursor"`
}

type ListDefinitionsParams struct {
	After *DefinitionRef `json:"after,omitempty"`
	Limit int            `json:"limit" min:"1" max:"100"`
}

type DefinitionSummary struct {
	Ref       DefinitionRef `json:"ref"`
	Name      string        `json:"name" pattern:"^[\\s\\S]{0,256}$"`
	CreatedAt string        `json:"created_at"`
}

type ListDefinitionsResult struct {
	Items      []DefinitionSummary `json:"items"`
	NextCursor *DefinitionRef      `json:"next_cursor"`
}

func discoverySchema(schema *jsonschema.Schema, t reflect.Type) {
	if t == reflect.TypeFor[ListTreesResult]() || t == reflect.TypeFor[ListDefinitionsResult]() {
		schema.Properties["items"].Type = "array"
		schema.Properties["items"].Types = nil
		schema.Properties["items"].MaxItems = new(100)
	}
}
