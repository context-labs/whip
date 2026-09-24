package tools

import (
	"encoding/json"
	"testing"
)

// Every built-in tool's JSON schema must parse — a malformed schema
// (trailing comma, stray quote) silently corrupts the provider request
// body for ALL tools, surfacing as cryptic marshal errors deep in the
// loop. This ratchet pins parseability at the source.
func TestBuiltinToolSchemasParse(t *testing.T) {
	services := NewServices()
	for _, tool := range append(append(All(), BrowserExec(services)), ComputerExec(services)) {
		var v any
		if err := json.Unmarshal(tool.Def.Function.Parameters, &v); err != nil {
			t.Errorf("%s: schema does not parse: %v", tool.Def.Function.Name, err)
		}
	}
}
