package runtime

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestBrowserHostSchemasKeepNamedAndOfferedTargetsSeparate(t *testing.T) {
	for _, action := range []string{"run", "detach"} {
		t.Run(action, func(t *testing.T) {
			var declaration session.ToolDeclaration
			for _, schema := range directSchemas {
				if schema.Module == "browser" && schema.Name == action {
					declaration.InputSchema = schema.InputSchema
				}
			}
			var schema map[string]any
			if err := json.Unmarshal(declaration.InputSchema, &schema); err != nil || schema["type"] != "object" {
				t.Fatalf("MCP tools require explicit top-level object type: %s (%v)", declaration.InputSchema, err)
			}
			for _, target := range []map[string]any{{"attachment_id": "exact"}, {"session": "default"}, {"session": "headless:named"}} {
				if action == "run" {
					target["code"] = "info()"
				}
				if err := declaration.ValidateInput(target); err != nil {
					t.Fatal(target, err)
				}
			}
			for _, target := range []map[string]any{{}, {"session": ""}, {"session": "../foreign"}, {"attachment_id": "exact", "session": "default"}, {"session": "default", "expected_document": "desktop-doc"}} {
				if action == "run" {
					target["code"] = "info()"
				}
				if err := declaration.ValidateInput(target); err == nil {
					t.Fatal("invalid target accepted", target)
				}
			}
		})
	}
}
