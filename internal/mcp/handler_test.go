package mcp

import (
	"encoding/json"
	"testing"
)

func findTestHandler(t *testing.T, handlers []Handler, name string) Handler {
	t.Helper()
	for _, handler := range handlers {
		if handler.Def.Function.Name == name {
			return handler
		}
	}
	t.Fatalf("missing MCP handler %q", name)
	return Handler{}
}

func callTestHandler(t *testing.T, handlers []Handler, name string, args json.RawMessage) string {
	t.Helper()
	out, err := findTestHandler(t, handlers, name).Run(t.Context(), args)
	if err != nil {
		t.Errorf("MCP handler %s: %v", name, err)
	}
	return out
}
