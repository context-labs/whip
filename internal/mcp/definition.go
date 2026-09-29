package mcp

import (
	"context"
	"encoding/json"
)

// Definition is a protocol tool declaration for the standalone MCP server.
type Definition struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}

func newDefinition(name, desc, schema string) Definition {
	t := Definition{Type: "function"}
	t.Function.Name = name
	t.Function.Description = desc
	t.Function.Parameters = json.RawMessage(schema)
	return t
}

// Handler retains the legacy tool-host adapter while the client uses checked calls.
type Handler struct {
	Def Definition
	Run func(context.Context, json.RawMessage) (string, error)
}
