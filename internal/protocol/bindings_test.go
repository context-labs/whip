package protocol

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestBindingProtocolModulesAndDerivedOwners(t *testing.T) {
	fixtures, err := Fixtures()
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	for _, fixture := range fixtures {
		if fixture.Type == "Session" && fixture.Valid {
			if err := json.Unmarshal(fixture.Value, &raw); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	config := raw["configuration"].(map[string]any)
	for _, modules := range []any{nil, []any{"unknown"}, []any{"files", "files"}} {
		config["modules"] = modules
		encoded, _ := json.Marshal(raw)
		if Validate("Session", encoded) == nil {
			t.Fatal("invalid effective modules accepted", modules)
		}
	}
	config["modules"] = []any{}
	encoded, _ := json.Marshal(raw)
	if err := Validate("Session", encoded); err != nil {
		t.Fatal("explicit none rejected", err)
	}
	for _, source := range []string{"tools_definition", "hooks_definition"} {
		patch := map[string]any{"session_id": "session", "expected_revision": "1", "patch": map[string]any{source: map[string]any{"id": "owner", "revision": "invalid"}}}
		encoded, _ := json.Marshal(patch)
		if Validate("UpdateConfigurationParams", encoded) == nil {
			t.Fatal("caller supplied provenance", source)
		}
	}
	_, _, ref, err := session.CanonicalDefinition(session.DefinitionDocument{ID: "tools", Name: "Tools", Defaults: session.ConfigPatch{Tools: map[string]session.ToolDeclaration{"lookup": {InputSchema: json.RawMessage(`{}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := ConfigurationFromDomain(session.Configuration{Modules: []string{}, ToolsDefinition: &ref, HooksDefinition: &session.DefinitionRef{ID: "hooks", Revision: ref.Revision}})
	if err != nil || configuration.ToolsDefinition.ID == configuration.HooksDefinition.ID || configuration.Modules == nil {
		t.Fatal("projection lost provenance/empty modules", err)
	}
}
