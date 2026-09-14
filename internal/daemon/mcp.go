package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/context-labs/whip/internal/capability"
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

// attachMCP adds client-supplied servers (ACP passes the editor's MCP
// configuration) to the root's live manager. Attachments are additive: they
// never replace the running manager or re-read native configuration, so an
// attach cannot drop earlier attachments or live imports. Client-supplied
// origin and source are descriptive; every attachment is untrusted for calls.
// The definition's server list is the same boundary the factory applies, and
// a native name cannot be taken over. Both refusals stay visible as blocked
// rows instead of vanishing. Re-attaching a name this or another client
// attached earlier replaces that entry.
func (s *Session) attachMCP(attached map[string]mcp.ServerConfig) error {
	servers := mcp.AttachedConfigs(attached)
	selected := mcp.Select(mcp.Filtered{Merged: servers}, s.definition.MCP.Servers)
	blocked := map[string]mcp.ServerConfig{}
	refuse := func(name, note string) {
		cfg := servers[name]
		cfg.Enabled = new(false)
		cfg.Note = note
		blocked[name] = cfg
		delete(selected.Merged, name)
	}
	for name := range servers {
		if _, ok := selected.Merged[name]; !ok {
			refuse(name, "outside this agent's MCP server list")
		}
	}
	manager := s.mcpManager()
	if manager == nil {
		manager = mcp.NewManager(selected.Merged)
		manager.SetBlocked(blocked)
		configureMCP(s, Components{MCP: manager})
		if previous := s.swapMCP(manager); previous != nil {
			_ = safeClose("previous MCP", previous.Close)
		}
		return nil
	}
	var replace []string
	for name := range selected.Merged {
		current, exists := manager.Config(name)
		switch {
		case !exists:
		case current.Trusted:
			refuse(name, "native configuration keeps this name")
		default:
			replace = append(replace, name)
		}
	}
	manager.RemoveServers(replace...)
	manager.AddServers(context.Background(), selected.Merged)
	manager.AddBlocked(blocked)
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
