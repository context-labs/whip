package runtime

import (
	"context"
	"slices"
	"sort"

	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// captureChildMCPTools snapshots available trusted tool identities. It never
// discovers or starts a connection. Explicit grants are delegated separately;
// these identities become usable only while live policy permits the child.
func (r *Runtime) captureChildMCPTools(ctx context.Context, parent session.Session) ([]store.MCPToolScope, error) {
	result := []store.MCPToolScope{}
	if !slices.Contains(parent.Config.Modules, "mcp") {
		return result, nil
	}
	// An unused MCP subsystem must not add workspace filesystem prerequisites
	// to spawning a child. Only validate an owner that already has connections.
	r.mcp.mu.Lock()
	existing := r.mcp.roots[parent.TreeID]
	r.mcp.mu.Unlock()
	if existing == nil {
		return result, nil
	}
	entry, err := r.mcpRoot(ctx, parent, false)
	if err != nil {
		return nil, err
	}
	manager := r.mcpManager(entry)
	if manager == nil {
		return result, nil
	}
	var ceiling []store.MCPToolScope
	if parent.ParentID != nil {
		ceiling, err = r.store.MCPInheritedTools(ctx, parent.ID)
		if err != nil {
			return nil, err
		}
	}
	for _, server := range manager.Statuses() {
		if server.Status != mcp.StatusReady || !parent.Config.MCPServers.Contains(server.Name) {
			continue
		}
		catalog, err := manager.Catalog(server.Name)
		if err != nil {
			continue
		}
		for _, row := range catalog {
			if !row.Call.Trusted {
				continue
			}
			item := store.MCPToolScope{Capability: mcpCallCapability(row.Call), Resource: mcpCallResource(entry.root, row.Call)}
			if parent.ParentID != nil && !slices.Contains(ceiling, item) {
				continue
			}
			result = append(result, item)
			if len(result) > store.MaxChildMCPTools {
				return nil, store.ErrLimit
			}
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Resource < result[j].Resource })
	return result, nil
}
