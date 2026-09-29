package protocol

import (
	"reflect"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

type (
	RecentTreesParams struct {
		Limit       int               `json:"limit" min:"1" max:"100"`
		After       *RecentTreeCursor `json:"after,omitempty"`
		Archived    *bool             `json:"archived,omitempty"`
		Pinned      *bool             `json:"pinned,omitempty"`
		PinnedFirst bool              `json:"pinned_first,omitempty"`
	}
	RecentTreeCursor struct {
		TreeID         ID     `json:"tree_id"`
		LastActivityAt string `json:"last_activity_at"`
		Pinned         bool   `json:"pinned"`
	}
	RecentTree struct {
		Tree             Tree           `json:"tree"`
		RootID           ID             `json:"root_id"`
		WorkingDirectory string         `json:"working_directory"`
		Model            ModelSelection `json:"model"`
		LastActivityAt   string         `json:"last_activity_at"`
	}
)

// CatalogRevision cannot pin recent order: every call reads current activity.
type RecentTreesResult struct {
	CatalogRevision Counter           `json:"catalog_revision" pattern:"^[1-9][0-9]{0,18}$"`
	Items           []RecentTree      `json:"items"`
	HasMore         bool              `json:"has_more"`
	NextCursor      *RecentTreeCursor `json:"next_cursor,omitempty"`
}

func RecentTreesFromDomain(page session.RecentTreePage) RecentTreesResult {
	result := RecentTreesResult{CatalogRevision: Counter(page.CatalogRevision), Items: []RecentTree{}, HasMore: page.HasMore}
	if page.Next != nil {
		result.NextCursor = &RecentTreeCursor{TreeID: ID(page.Next.TreeID), LastActivityAt: page.Next.LastActivityAt.Format(time.RFC3339Nano), Pinned: page.Next.Pinned}
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, RecentTree{Tree: TreeFromDomain(item.Tree), RootID: ID(item.RootID), WorkingDirectory: item.WorkingDirectory, Model: ModelSelection{Provider: ID(item.Model.Provider), Name: item.Model.Name, Effort: item.Model.Effort, Temperature: item.Model.Temperature, TopP: item.Model.TopP}, LastActivityAt: item.LastActivityAt.Format(time.RFC3339Nano)})
	}
	return result
}

func recentSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[RecentTreesResult]():
		schema.Properties["items"].Type = "array"
		schema.Properties["items"].Types = nil
		schema.Properties["items"].MaxItems = new(100)
	case reflect.TypeFor[RecentTree]():
		schema.Properties["working_directory"].MaxLength = new(4096)
		schema.Properties["last_activity_at"].MaxLength = new(64)
	case reflect.TypeFor[RecentTreeCursor]():
		schema.Properties["last_activity_at"].MaxLength = new(64)
		schema.Properties["last_activity_at"].MinLength = new(1)
	}
}
