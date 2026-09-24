package protocol

import (
	"encoding/json"

	"github.com/context-labs/whip/internal/mcp"
)

type MCPServerConfig struct {
	Command        []string          `json:"command,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Cwd            string            `json:"cwd,omitempty"`
	URL            string            `json:"url,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Enabled        *bool             `json:"enabled,omitempty"`
	Note           string            `json:"note,omitempty"`
	StartupTimeout int               `json:"startup_timeout,omitempty"`
	ToolTimeout    int               `json:"tool_timeout,omitempty"`
	Source         string            `json:"source,omitempty"`
	Origin         string            `json:"origin,omitempty"`
	Trusted        bool              `json:"-"`
}
type mcpAttachWire struct {
	Servers map[string]MCPServerConfig `json:"servers"`
}

func (p MCPAttachParams) MarshalJSON() ([]byte, error) {
	wire := mcpAttachWire{}
	if p.Servers != nil {
		wire.Servers = make(map[string]MCPServerConfig, len(p.Servers))
	}
	for name, c := range p.Servers {
		wire.Servers[name] = MCPServerConfig(c)
	}
	return json.Marshal(wire)
}

func (p *MCPAttachParams) UnmarshalJSON(raw []byte) error {
	var wire mcpAttachWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	p.Servers = nil
	if wire.Servers != nil {
		p.Servers = make(map[string]mcp.ServerConfig, len(wire.Servers))
	}
	for name, c := range wire.Servers {
		c.Trusted = false
		p.Servers[name] = mcp.ServerConfig(c)
	}
	return nil
}
