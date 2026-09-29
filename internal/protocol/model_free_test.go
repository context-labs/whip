package protocol

import (
	"encoding/json"
	"testing"
)

func TestModelFreePatchIsOnlyTheEntireEmptySelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model ModelSelection
		valid bool
	}{
		{"empty", ModelSelection{}, true},
		{"configured", ModelSelection{Provider: "provider", Name: "model"}, true},
		{"missing_provider", ModelSelection{Name: "model"}, false},
		{"missing_name", ModelSelection{Provider: "provider"}, false},
		{"effort", ModelSelection{Effort: "high"}, false},
		{"temperature", ModelSelection{Temperature: new(0.0)}, false},
		{"top_p", ModelSelection{TopP: new(0.5)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(CreateTreeParams{CreationID: "model_free", Definition: DefinitionRef{ID: "fixture", Revision: "0000000000000000000000000000000000000000000000000000000000000000"}, WorkingDirectory: "/workspace", Overrides: ConfigPatch{Model: &tc.model}})
			if err != nil {
				t.Fatal(err)
			}
			if err = Validate("CreateTreeParams", raw); (err == nil) != tc.valid {
				t.Fatal(string(raw), err)
			}
			if _, err := (ConfigPatch{Model: &tc.model}).Domain(); (err == nil) != tc.valid {
				t.Fatal("domain", err)
			}
		})
	}
}
