package runtime

import (
	"context"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

// MCPStatus observes declared or live state. It never creates a manager or
// resolves credentials. Child metadata remains limited to delegated tools.
func (r *Runtime) MCPStatus(ctx context.Context, id session.SessionID) ([]mcp.ServerStatus, error) {
	owner, err := r.store.Session(ctx, id)
	if err != nil {
		return nil, err
	}
	entry, err := r.mcpRoot(ctx, owner, false)
	if err != nil {
		return nil, err
	}
	if owner.ParentID == nil {
		return r.mcpStatuses(ctx, owner, entry)
	}
	value, err := r.mcpCatalog(ctx, owner, session.MCPCatalogRequest{Action: "list_servers"})
	if err != nil {
		return nil, err
	}
	return value.([]mcp.ServerStatus), nil
}

// beginMCPHuman owns an explicitly accepted connection action until completion
// or runtime shutdown. Disconnecting an observer does not reverse that action.
func (r *Runtime) beginMCPHuman(ctx context.Context) (context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	release, err := takeMCPSlot(ctx, r.mcp.humans)
	if err != nil {
		return nil, nil, err
	}
	r.mcp.mu.Lock()
	if r.mcp.closed {
		r.mcp.mu.Unlock()
		release()
		return nil, nil, ErrClosed
	}
	r.mcp.workers.Add(1)
	r.mcp.mu.Unlock()
	owned, cancel := context.WithTimeout(r.mcp.ctx, 300*time.Second)
	return owned, func() { cancel(); release(); r.mcp.workers.Done() }, nil
}

func (r *Runtime) MCPRefresh(ctx context.Context, id session.SessionID) (mcp.RefreshResult, error) {
	return r.mcpAction(ctx, id, "refresh", "", nil)
}

func (r *Runtime) MCPReload(ctx context.Context, id session.SessionID) (mcp.RefreshResult, error) {
	return r.mcpAction(ctx, id, "reload", "", nil)
}

func (r *Runtime) MCPReconnect(ctx context.Context, id session.SessionID, name string) (mcp.RefreshResult, error) {
	return r.mcpAction(ctx, id, "reconnect", name, nil)
}

func (r *Runtime) MCPEnable(ctx context.Context, id session.SessionID, name string) (mcp.RefreshResult, error) {
	return r.mcpAction(ctx, id, "enable", name, nil)
}

func (r *Runtime) MCPDisable(ctx context.Context, id session.SessionID, name string) (mcp.RefreshResult, error) {
	return r.mcpAction(ctx, id, "disable", name, nil)
}

func (r *Runtime) MCPAttach(ctx context.Context, id session.SessionID, servers map[string]mcp.ServerConfig) (mcp.RefreshResult, error) {
	return r.mcpAction(ctx, id, "attach", "", servers)
}

func (r *Runtime) mcpAction(ctx context.Context, id session.SessionID, action, name string, attachments map[string]mcp.ServerConfig) (mcp.RefreshResult, error) {
	if len(attachments) > mcp.MaxServers {
		return mcp.RefreshResult{}, store.ErrLimit
	}
	owner, err := r.store.Session(ctx, id)
	if err != nil {
		return mcp.RefreshResult{}, err
	}
	if owner.ParentID != nil || name != "" && !owner.Config.MCPServers.Contains(name) {
		return mcp.RefreshResult{}, store.ErrConflict
	}
	ctx, done, err := r.beginMCPHuman(ctx)
	if err != nil {
		return mcp.RefreshResult{}, err
	}
	defer done()
	entry, err := r.mcpRoot(ctx, owner, true)
	if err != nil {
		return mcp.RefreshResult{}, err
	}
	release, err := takeMCPSlot(ctx, entry.actions)
	if err != nil {
		return mcp.RefreshResult{}, err
	}
	defer release()
	// The owner was inserted before this second existence check, so a deletion
	// racing afterward will observe and retire it during subtree cleanup.
	if _, err := r.store.Session(ctx, owner.ID); err != nil {
		return mcp.RefreshResult{}, err
	}
	manager := r.mcpManager(entry)
	if action == "refresh" || action == "reload" || action == "attach" {
		discovery, err := r.discoverMCP(ctx, owner)
		if err != nil {
			return mcp.RefreshResult{}, err
		}
		if action == "reload" {
			r.mcp.mu.Lock()
			if !r.mcp.closed && !entry.retired {
				entry.manager = nil
			}
			r.mcp.mu.Unlock()
			if manager != nil {
				manager.Close()
			}
		}
		if action == "attach" {
			additions, refused := map[string]mcp.ServerConfig{}, map[string]mcp.ServerConfig{}
			// An attachment never replaces a declaration, including declarations
			// filtered by policy. Its apparent provenance cannot confer trust.
			for server, cfg := range mcp.AttachedConfigs(attachments) {
				if _, exists := discovery.config.Merged[server]; exists {
					cfg.Note = "attachment cannot replace a configured server"
					refused[server] = cfg
					continue
				}
				if _, exists := discovery.config.Blocked[server]; exists {
					cfg.Note = "attachment cannot replace a configured server"
					refused[server] = cfg
					continue
				}
				if !owner.Config.MCPServers.Contains(server) {
					cfg.Note = "attachment is outside selected MCP servers"
					refused[server] = cfg
					continue
				}
				if problem := cfg.Valid(); problem != "" {
					return mcp.RefreshResult{}, fmt.Errorf("%w: %s", session.ErrInvalid, problem)
				}
				additions[server] = cfg
			}
			discovery.config.Merged = additions
			result, err := r.applyMCPDiscovery(ctx, entry, discovery)
			if manager := r.mcpManager(entry); manager != nil {
				if failure := manager.AddBlocked(refused); failure != nil {
					return result, failure
				}
				result.Blocked = mcp.ServerStatuses(manager.Blocked())
			}
			return result, err
		}
		return r.applyMCPDiscovery(ctx, entry, discovery)
	}
	if manager == nil {
		return mcp.RefreshResult{}, store.ErrNotFound
	}
	var changed bool
	switch action {
	case "enable":
		changed = manager.Enable(name)
	case "disable":
		changed = manager.Disable(name)
	case "reconnect":
		changed = manager.Reconnect(name)
	default:
		return mcp.RefreshResult{}, session.ErrInvalid
	}
	if !changed {
		return mcp.RefreshResult{}, store.ErrConflict
	}
	return mcp.RefreshResult{Added: []string{}, Existing: []string{name}, Changed: []string{}, Servers: mcp.ServerStatuses(manager.Statuses()), Blocked: mcp.ServerStatuses(manager.Blocked()), SourceErrors: mcp.ServerStatuses(manager.SourceErrors())}, nil
}
