package protocol

import (
	"encoding/json"
	"testing"
)

func TestHostSkillCompletionScope(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields string
		valid  bool
	}{
		{"legacy", `"cwd":"/project"`, true},
		{"legacy-empty-scope", `"scope":"","cwd":"/project"`, true},
		{"missing-cwd", `"scope":""`, false},
		{"empty-cwd", `"cwd":""`, false},
		{"global", `"scope":"global"`, true},
		{"global-empty-cwd", `"scope":"global","cwd":""`, true},
		{"global-with-cwd", `"scope":"global","cwd":"/project"`, false},
		{"global-whitespace-cwd", `"scope":"global","cwd":" "`, false},
		{"project-alias", `"scope":"project","cwd":"/project"`, false},
		{"unknown", `"scope":"other"`, false},
		{"null-scope", `"scope":null,"cwd":"/project"`, false},
		{"null-cwd", `"scope":"global","cwd":null`, false},
		{"number-cwd", `"scope":"global","cwd":42`, false},
		{"unknown-field", `"scope":"global","extra":true`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := json.RawMessage(`{"prefix":"","limit":1024,` + test.fields + `}`)
			if err := ValidateRPC("host.skills.complete", raw); (err == nil) != test.valid {
				t.Fatalf("valid=%v, err=%v", test.valid, err)
			}
		})
	}
	for _, raw := range []string{`{}`, `{"scope":"global","limit":1}`, `{"scope":"global","prefix":""}`} {
		if err := ValidateRPC("host.skills.complete", json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted missing required fields: %s", raw)
		}
	}
}
