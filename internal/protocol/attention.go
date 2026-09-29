package protocol

import (
	"reflect"

	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

type HostAttentionCursor struct {
	TreeID    ID `json:"tree_id"`
	SessionID ID `json:"session_id"`
}
type HostAttentionParams struct {
	After    *HostAttentionCursor `json:"after"`
	Limit    int                  `json:"limit" min:"1" max:"100"`
	MaxBytes int                  `json:"max_bytes" min:"4096" max:"524288"`
}
type HostAttentionItem struct {
	TreeID    ID              `json:"tree_id"`
	RootID    ID              `json:"root_id"`
	SessionID ID              `json:"session_id"`
	Title     *string         `json:"title"`
	Activity  SessionActivity `json:"activity"`
}
type HostAttentionResult struct {
	Items      []HostAttentionItem  `json:"items"`
	NextCursor *HostAttentionCursor `json:"next_cursor"`
}

func attentionSchema(schema *jsonschema.Schema, t reflect.Type) {
	if t == reflect.TypeFor[HostAttentionResult]() {
		schema.Properties["items"].Type = "array"
		schema.Properties["items"].Types = nil
		schema.Properties["items"].MaxItems = new(100)
	}
}

func AttentionFromDomain(value session.AttentionPage) HostAttentionResult {
	result := HostAttentionResult{Items: []HostAttentionItem{}}
	for _, item := range value.Items {
		result.Items = append(result.Items, HostAttentionItem{TreeID: ID(item.TreeID), RootID: ID(item.RootID), SessionID: ID(item.SessionID), Title: item.Title, Activity: ActivityFromDomain(item.Activity)})
	}
	if value.NextCursor != nil {
		result.NextCursor = &HostAttentionCursor{TreeID: ID(value.NextCursor.TreeID), SessionID: ID(value.NextCursor.SessionID)}
	}
	return result
}
