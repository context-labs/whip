package runtime

import (
	"github.com/context-labs/whip/internal/session"
	"testing"
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
