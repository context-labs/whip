package mcp

import "encoding/json"

// Definition is a protocol tool declaration for the standalone MCP server.
type Definition struct {
	Type     string `json:"type"`
	Function struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Parameters  json.RawMessage `json:"parameters"`
	} `json:"function"`
}
