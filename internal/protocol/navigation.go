package protocol

import (
	"reflect"

	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

type TreeSummariesParams struct {
	RootIDs []ID `json:"root_ids"`
}
type TreeActivity struct {
	ActiveTurnCount            Counter `json:"active_turn_count"`
	QueuedInputCount           Counter `json:"queued_input_count"`
	PendingPermissionCount     Counter `json:"pending_permission_count"`
	PendingQuestionCount       Counter `json:"pending_question_count"`
	ActiveWorkspaceActionCount Counter `json:"active_workspace_action_count"`
}
type TreeNavigationSummary struct {
	Tree             Tree         `json:"tree"`
	RootID           ID           `json:"root_id"`
	WorkingDirectory string       `json:"working_directory"`
	Activity         TreeActivity `json:"activity"`
}
type TreeSummariesResult struct {
	Items          []TreeNavigationSummary `json:"items"`
	MissingRootIDs []ID                    `json:"missing_root_ids"`
}

func TreeSummariesFromDomain(page session.TreeNavigationPage) TreeSummariesResult {
	result := TreeSummariesResult{Items: []TreeNavigationSummary{}, MissingRootIDs: []ID{}}
	for _, item := range page.Items {
		result.Items = append(result.Items, TreeNavigationSummary{
			Tree: TreeFromDomain(item.Tree), RootID: ID(item.RootID), WorkingDirectory: item.WorkingDirectory,
			Activity: TreeActivity{ActiveTurnCount: Counter(item.ActiveTurnCount), QueuedInputCount: Counter(item.QueuedInputCount), PendingPermissionCount: Counter(item.PendingPermissionCount), PendingQuestionCount: Counter(item.PendingQuestionCount), ActiveWorkspaceActionCount: Counter(item.ActiveWorkspaceActionCount)},
		})
	}
	for _, id := range page.Missing {
		result.MissingRootIDs = append(result.MissingRootIDs, ID(id))
	}
	return result
}

func navigationSchema(schema *jsonschema.Schema, t reflect.Type) {
	switch t {
	case reflect.TypeFor[TreeSummariesParams]():
		field := schema.Properties["root_ids"]
		field.Type = "array"
		field.Types = nil
		field.MinItems = new(1)
		field.MaxItems = new(64)
		field.UniqueItems = true
	case reflect.TypeFor[TreeSummariesResult]():
		for _, name := range []string{"items", "missing_root_ids"} {
			field := schema.Properties[name]
			field.Type = "array"
			field.Types = nil
			field.MaxItems = new(64)
		}
		schema.Properties["missing_root_ids"].UniqueItems = true
	case reflect.TypeFor[TreeNavigationSummary]():
		schema.Properties["working_directory"].MaxLength = new(4096)
	}
}
