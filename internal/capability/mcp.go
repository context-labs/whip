package capability

import "encoding/json"

// MCPSelector identifies one exact configured tool definition. Reconnecting a
// server does not change a selector unless its configuration or schema changes.
type MCPSelector struct {
	Server     string `json:"server"`
	Tool       string `json:"tool"`
	Definition string `json:"definition"`
}

// MCPCall binds an operation to the currently connected tool and its origin.
// Generation is ephemeral; delegated grants retain only the stable selector.
type MCPCall struct {
	MCPSelector
	Generation string          `json:"generation"`
	Source     string          `json:"source"`
	Trusted    bool            `json:"trusted"`
	Arguments  json.RawMessage `json:"arguments"`
}
