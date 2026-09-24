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
	s.mcpGeneration++
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
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
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
	manager, _ := s.mcp.(*mcp.Manager)
	if manager == nil {
		manager = mcp.NewManager(selected.Merged)
		manager.AddBlocked(blocked)
		configureMCP(s, Components{MCP: manager})
		previous := s.mcp
		s.mcp = manager
		if previous != nil {
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
	if _, err := manager.AddServers(s.supervisor.ctx, selected.Merged); err != nil {
		return err
	}
	manager.AddBlocked(blocked)
	return nil
}

// refreshMCP adds newly configured servers without replacing the root runtime
// or any existing connection. Discovery runs outside the manager lock; a runtime
// replacement during discovery requires retrying against its new configuration.
func (s *Session) refreshMCP(ctx context.Context) (mcp.RefreshResult, error) {
	var result mcp.RefreshResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	s.mcpMu.RLock()
	load, generation := s.loadMCP, s.mcpGeneration
	allowed := slices.Clone(s.mcpServers)
	s.mcpMu.RUnlock()
	if load == nil {
		return result, errors.New("MCP refresh is unavailable for this session")
	}
	discovery, err := load(ctx)
	if err != nil {
		return result, fmt.Errorf("load MCP configuration: %w", err)
	}
	discovery = mcp.Select(discovery, allowed)
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if s.supervisor != nil && s.supervisor.ctx.Err() != nil {
		return result, ErrStopped
	}
	if generation != s.mcpGeneration {
		return result, errors.New("MCP runtime changed during discovery; retry refresh")
	}
	manager, _ := s.mcp.(*mcp.Manager)
	if manager == nil {
		if s.mcp != nil {
			return result, errors.New("MCP refresh is unavailable for this session")
		}
		manager = mcp.NewManager(nil)
		configureMCP(s, Components{MCP: manager})
		s.mcp = manager
	}
	result, err = manager.AddServers(ctx, discovery.Merged)
	if err != nil {
		return result, err
	}
	// Preserve attachment refusals and never add a blocked row over a live name.
	blocked := make(map[string]mcp.ServerConfig)
	for name, cfg := range discovery.Blocked {
		if _, exists := manager.Config(name); !exists {
			blocked[name] = cfg
		}
	}
	manager.SetBlocked(blocked)
	manager.SetSourceErrors(discovery.Errs)
	result.Blocked, result.SourceErrors = mcp.ServerStatuses(manager.Blocked()), mcp.ServerStatuses(manager.SourceErrors())
	return result, nil
}

// reconnectMCP requests reconnection, not successful readiness. It cannot
// enable disabled servers or change their saved/live configuration.
func (s *Session) reconnectMCP(ctx context.Context, name string) (mcp.Server, error) {
	if err := ctx.Err(); err != nil {
		return mcp.Server{}, err
	}
	manager := s.mcpManager()
	if manager == nil {
		return mcp.Server{}, errors.New("MCP integration is unavailable")
	}
	if _, err := applyMCPAction(manager, "mcp.reconnect", name); err != nil {
		return mcp.Server{}, err
	}
	for _, status := range manager.Statuses() {
		if status.Name == name {
			return status, nil
		}
	}
	return mcp.Server{}, fmt.Errorf("MCP server %q was removed during reconnect", name)
}

// mcpListDefaultLimit is the list_tools window when the model gives none.
const mcpListDefaultLimit = 100

// mcpAuthorized resolves a tool's callable descriptor and reports whether
// this agent may call it right now: the check list_tools has always made, so
// discovery never advertises a call the dispatcher would refuse.
func (host *recursiveHost) mcpAuthorized(ctx context.Context, manager *mcp.Manager, server, tool string) (capability.MCPCall, bool) {
	call, err := manager.ResolveTool(server, tool)
	node := host.session
	return call, err == nil && node.root.store.AuthorizeMCP(ctx, node.root.ID(), node.id, node.authority.MCP, call.MCPSelector) == nil
}

// mcpToolEntry is one tool as the model sees it: identity, callable
// descriptor and authorization, with the input schema only when asked for.
func (host *recursiveHost) mcpToolEntry(ctx context.Context, manager *mcp.Manager, server string, tool mcp.Tool, schema bool) map[string]any {
	call, authorized := host.mcpAuthorized(ctx, manager, server, tool.Name)
	entry := map[string]any{
		"name": tool.Name, "title": tool.Title, "description": tool.Description,
		"authorized": authorized, "definition": call.Definition, "generation": call.Generation,
	}
	if schema {
		entry["input_schema"] = tool.InputSchema
	}
	return entry
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
