package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/capability"
)

type CatalogTool struct {
	Tool
	Call capability.MCPCall
}

// Catalog observes one exact connection generation under the server lock.
// Call arguments are absent until a host adapter captures a concrete invocation.
func (m *Manager) Catalog(name string) ([]CatalogTool, error) {
	s, err := m.lookup(name)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != StatusReady || s.sess == nil {
		return nil, s.unavailableLocked()
	}
	result := make([]CatalogTool, 0, len(s.defs))
	names := map[string]bool{}
	for _, definition := range s.defs {
		if names[definition.Name] {
			return nil, fmt.Errorf("MCP server advertises duplicate tool %q", definition.Name)
		}
		names[definition.Name] = true
		call, schema, err := s.definitionDescriptorLocked(definition)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(schema)
		if err != nil {
			return nil, err
		}
		result = append(result, CatalogTool{Name: definition.Name, Title: definition.Title, Description: definition.Description, InputSchema: raw, Call: call})
	}
	return result, nil
}
