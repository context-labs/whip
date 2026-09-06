package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/tools"
)

// The root owns connections. Descendants and client adapters resolve this
// owner at use time, so attachment and reload cannot leave a stale registry.
func (s *Session) currentMCP() Closeable {
	s.mcpMu.RLock()
	defer s.mcpMu.RUnlock()
	return s.mcp
}

func (s *Session) swapMCP(manager Closeable) Closeable {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	previous := s.mcp
	s.mcp = manager
	return previous
}

func (s *Session) mcpManager() *mcp.Manager {
	if s == nil {
		return nil
	}
	manager, _ := s.currentMCP().(*mcp.Manager)
	return manager
}

func (s *Session) mcpProvider() tools.MCPProvider {
	if manager := s.mcpManager(); manager != nil {
		return manager
	}
	return nil
}

func (s *Session) attachMCP(attached map[string]mcp.ServerConfig) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	// Client-supplied origin and source are descriptive. Only definitions
	// loaded by this daemon from native WHIP configuration can confer trust.
	servers := mcp.AttachedConfigs(attached)
	for name, server := range mcp.FromConfigMap(cfg.MCPServers) {
		servers[name] = server
	}
	manager := mcp.NewManager(servers)
	configureMCP(s, Components{MCP: manager})
	previous := s.swapMCP(manager)
	if previous != nil {
		_ = safeClose("previous MCP", previous.Close)
	}
	return nil
}

func delegatedMCPTools(ctx context.Context, parent *AgentSession, names []string, requested any) ([]capability.MCPSelector, error) {
	if !slices.Contains(names, "mcp") {
		if requested != nil {
			return nil, errors.New("mcp_tools requires the mcp capability")
		}
		return nil, nil
	}
	granted, all, err := parent.root.store.MCPSelectors(ctx, parent.root.ID(), parent.id, parent.authority.MCP)
	if err != nil {
		return nil, err
	}
	available := make([]capability.MCPSelector, 0)
	if manager := parent.root.mcpManager(); manager != nil {
		for _, server := range manager.Statuses() {
			listed, err := manager.ListTools(server.Name)
			if err != nil {
				continue
			}
			for _, tool := range listed {
				call, err := manager.ResolveTool(server.Name, tool.Name)
				if err == nil && (all || slices.Contains(granted, call.MCPSelector)) {
					available = append(available, call.MCPSelector)
				}
			}
		}
	}
	if requested == nil {
		return available, nil
	}
	items, ok := requested.([]any)
	if !ok {
		return nil, errors.New("mcp_tools must be a list of {server, tool} dictionaries")
	}
	selected := make([]capability.MCPSelector, 0, len(items))
	for _, item := range items {
		pair, ok := item.(map[string]any)
		if !ok || len(pair) != 2 {
			return nil, errors.New("each mcp_tools entry requires exactly server and tool")
		}
		server, _ := pair["server"].(string)
		tool, _ := pair["tool"].(string)
		index := slices.IndexFunc(available, func(selector capability.MCPSelector) bool {
			return selector.Server == server && selector.Tool == tool
		})
		if index < 0 {
			return nil, fmt.Errorf("MCP tool %q on server %q is not available to the parent", tool, server)
		}
		if !slices.Contains(selected, available[index]) {
			selected = append(selected, available[index])
		}
	}
	return selected, nil
}
