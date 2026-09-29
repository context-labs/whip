package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func mcpCallResource(root session.SessionID, call capability.MCPCall) string {
	identity, _ := json.Marshal(struct {
		Root     session.SessionID
		Selector capability.MCPSelector
	}{root, call.MCPSelector})
	return fmt.Sprintf("mcp_call_%x", sha256.Sum256(identity))
}

func mcpCallCapability(call capability.MCPCall) string {
	if call.Trusted {
		return "mcp.call.trusted"
	}
	return "mcp.call"
}

func (r *Runtime) prepareMCP(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	switch call.Name {
	case "refresh", "reconnect":
		return r.prepareMCPConnect(ctx, current, call)
	case "call":
		return r.prepareMCPCall(ctx, current, call)
	case "list_servers", "list_tools", "search", "describe", "instructions":
		var args struct {
			Server string `json:"server"`
			Tool   string `json:"tool"`
			Query  string `json:"query"`
			Limit  int    `json:"limit"`
		}
		if err := decodeArguments(call.Arguments, &args); err != nil {
			return tool.Prepared{}, err
		}
		if args.Limit == 0 {
			args.Limit = mcp.SearchDefaultLimit
		}
		request := session.MCPCatalogRequest{SessionID: current.ID, TreeID: current.TreeID, ConfigRevision: current.ConfigRevision, Selection: current.Config.MCPServers, Action: call.Name, Server: args.Server, Tool: args.Tool, Query: args.Query, Limit: args.Limit}
		if err := request.Validate(); err != nil {
			return tool.Prepared{}, err
		}
		raw, err := json.Marshal(request)
		if err != nil {
			return tool.Prepared{}, err
		}
		return tool.Prepared{Capability: "mcp.catalog", Resource: string(current.TreeID), Arguments: raw, Lifetime: r.mcp.ctx, Acquire: func(context.Context) (func(), error) { return func() {}, nil }, Run: func(ctx context.Context, id session.OperationID) (any, error) {
			value, err := r.mcpCatalog(ctx, current, request)
			if err != nil {
				return nil, err
			}
			return r.mcpMetadataValue(ctx, current.ID, id, value)
		}}, nil
	default:
		return tool.Prepared{}, fmt.Errorf("%w: unknown MCP operation", session.ErrInvalid)
	}
}

func (r *Runtime) prepareMCPCall(ctx context.Context, current session.Session, invocation tool.Invocation) (tool.Prepared, error) {
	var args struct {
		Server    string          `json:"server"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := decodeArguments(invocation.Arguments, &args); err != nil {
		return tool.Prepared{}, err
	}
	if !current.Config.MCPServers.Contains(args.Server) {
		return tool.Prepared{}, store.ErrConflict
	}
	entry, err := r.mcpRoot(ctx, current, false)
	if err != nil {
		return tool.Prepared{}, err
	}
	manager := r.mcpManager(entry)
	if manager == nil {
		return tool.Prepared{}, errors.New("MCP connections have not been started; root refresh requires authority")
	}
	call, err := manager.ResolveTool(args.Server, args.Tool)
	if err != nil {
		return tool.Prepared{}, err
	}
	call.Arguments = args.Arguments
	if err := manager.ValidateArguments(call); err != nil {
		return tool.Prepared{}, err
	}
	lifetime, err := manager.CallContext(call)
	if err != nil {
		return tool.Prepared{}, err
	}
	declaration, _ := manager.Config(args.Server)
	arguments, err := json.Marshal(call)
	if err != nil {
		return tool.Prepared{}, err
	}
	var acquired *mcp.AcquiredCall
	var images *hostImages
	return tool.Prepared{
		Capability: mcpCallCapability(call), Resource: mcpCallResource(entry.root, call), Arguments: arguments, Mutating: true, Lifetime: lifetime, Timeout: declaration.ToolTimeoutDuration(),
		Acquire: func(ctx context.Context) (func(), error) {
			imageAllowance, releaseImages, err := r.acquireHostImages(ctx, current, invocation)
			if err != nil {
				return nil, err
			}
			images = imageAllowance
			releaseSlot, err := r.mcpCallSlots(ctx, current, entry)
			release := func() { releaseSlot(); releaseImages() }
			if err != nil {
				releaseImages()
				return nil, err
			}
			acquired, err = manager.AcquireCall(ctx, call)
			if err != nil {
				release()
				return nil, err
			}
			return func() { acquired.Close(); release() }, nil
		}, Run: func(ctx context.Context, id session.OperationID) (any, error) {
			result, callErr := acquired.Execute(ctx)
			if callErr != nil && !errors.Is(callErr, mcp.ErrToolFailure) {
				return nil, callErr
			}
			value, writeErr := r.mcpResult(ctx, current.ID, id, result, images)
			if writeErr != nil {
				return value, tool.SettledFailure(errors.Join(callErr, writeErr))
			}
			if callErr != nil {
				return value, tool.SettledFailure(callErr)
			}
			return value, nil
		},
	}, nil
}

func (r *Runtime) prepareMCPConnect(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	if current.ParentID != nil {
		return tool.Prepared{}, store.ErrConflict
	}
	var args struct {
		Server string `json:"server"`
	}
	if err := decodeArguments(call.Arguments, &args); err != nil {
		return tool.Prepared{}, err
	}
	if (call.Name == "refresh" && args.Server != "") || (call.Name == "reconnect" && (args.Server == "" || !current.Config.MCPServers.Contains(args.Server))) {
		return tool.Prepared{}, session.ErrInvalid
	}
	discovery, err := r.discoverMCP(ctx, current)
	if err != nil {
		return tool.Prepared{}, err
	}
	entry, err := r.mcpRoot(ctx, current, true)
	if err != nil {
		return tool.Prepared{}, err
	}
	// Reconnection retains the already captured declaration rather than silently
	// adopting a changed endpoint. Its trust is that declaration's trust.
	identity, trusted := discovery.identity, discovery.trusted
	var reconnect mcp.ServerConfig
	if call.Name == "reconnect" {
		manager := r.mcpManager(entry)
		if manager == nil {
			return tool.Prepared{}, store.ErrNotFound
		}
		var ok bool
		reconnect, ok = manager.Config(args.Server)
		if !ok {
			return tool.Prepared{}, store.ErrNotFound
		}
		raw, _ := json.Marshal(struct {
			Config  mcp.ServerConfig
			Trusted bool
		}{reconnect, reconnect.Trusted})
		identity, trusted = fmt.Sprintf("%x", sha256.Sum256(raw)), reconnect.Trusted
	}
	capability := "mcp.connect"
	if trusted {
		capability = "mcp.connect.trusted"
	}
	arguments, _ := json.Marshal(struct{ Action, Server, Identity string }{call.Name, args.Server, identity})
	return tool.Prepared{
		Capability: capability, Resource: "mcp_connect_" + string(entry.root) + "_" + identity, Arguments: arguments, Mutating: true, Timeout: 300 * time.Second, Lifetime: r.mcp.ctx,
		Acquire: func(ctx context.Context) (func(), error) { return takeMCPSlot(ctx, entry.actions) },
		Run: func(ctx context.Context, _ session.OperationID) (any, error) {
			if call.Name == "reconnect" {
				manager := r.mcpManager(entry)
				if manager == nil {
					return nil, tool.SettledFailure(store.ErrConflict)
				}
				latest, ok := manager.Config(args.Server)
				if !ok || latest.Trusted != reconnect.Trusted || !session.SameContract(latest, reconnect) {
					return nil, tool.SettledFailure(store.ErrConflict)
				}
				if !manager.Reconnect(args.Server) {
					return nil, tool.SettledFailure(store.ErrConflict)
				}
				return map[string]any{"accepted": true, "server": args.Server}, nil
			}
			latest, err := r.discoverMCP(ctx, current)
			if err != nil {
				return nil, tool.SettledFailure(err)
			}
			if latest.identity != identity {
				return nil, tool.SettledFailure(store.ErrConflict)
			}
			return r.applyMCPDiscovery(ctx, entry, discovery)
		},
	}, nil
}

type MCPTool struct {
	mcp.Tool
	Server     string `json:"server"`
	Generation string `json:"generation"`
	Capability string `json:"capability"`
	Resource   string `json:"resource"`
}

func (r *Runtime) mcpVisibleCatalog(ctx context.Context, current session.Session, entry *mcpRoot) (map[string][]MCPTool, map[string]bool, error) {
	visible := map[string][]MCPTool{}
	grants := map[string]bool{}
	if current.ParentID != nil {
		standing, err := r.store.MCPDelegatedAuthority(ctx, current.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, grant := range standing {
			grants[grant.Capability+"\x00"+grant.Resource] = true
		}
	}
	manager := r.mcpManager(entry)
	if manager == nil {
		return visible, grants, nil
	}
	for _, server := range manager.Statuses() {
		if server.Status != mcp.StatusReady || !current.Config.MCPServers.Contains(server.Name) {
			continue
		}
		catalog, err := manager.Catalog(server.Name)
		if err != nil {
			continue
		}
		for _, row := range catalog {
			capability, resource := mcpCallCapability(row.Call), mcpCallResource(entry.root, row.Call)
			if current.ParentID != nil && !grants[capability+"\x00"+resource] {
				continue
			}
			visible[server.Name] = append(visible[server.Name], MCPTool{Tool: row.Tool, Server: server.Name, Generation: row.Call.Generation, Capability: capability, Resource: resource})
		}
	}
	return visible, grants, nil
}

func (r *Runtime) mcpCatalog(ctx context.Context, current session.Session, request session.MCPCatalogRequest) (any, error) {
	entry, err := r.mcpRoot(ctx, current, false)
	if err != nil {
		return nil, err
	}
	visible, grants, err := r.mcpVisibleCatalog(ctx, current, entry)
	if err != nil {
		return nil, err
	}
	manager := r.mcpManager(entry)
	if current.ParentID == nil && request.Server != "" && request.Action != "instructions" {
		if manager == nil {
			return nil, errors.New("MCP connections have not been started")
		}
		if _, err := manager.ListTools(request.Server); err != nil {
			return nil, err
		}
	}
	switch request.Action {
	case "list_servers":
		if current.ParentID == nil {
			return r.mcpStatuses(ctx, current, entry)
		}
		result := []mcp.ServerStatus{}
		if manager != nil {
			for _, row := range mcp.ServerStatuses(manager.Statuses()) {
				if tools := visible[row.Name]; len(tools) > 0 {
					row.Tools = len(tools)
					row.Note = ""
					row.Error = ""
					result = append(result, row)
				}
			}
		}
		return result, nil
	case "list_tools":
		if rows := visible[request.Server]; rows != nil {
			return rows, nil
		}
		if current.ParentID != nil {
			return nil, store.ErrNotFound
		}
		return []MCPTool{}, nil
	case "describe":
		for _, row := range visible[request.Server] {
			if row.Name == request.Tool {
				return row, nil
			}
		}
		if current.ParentID == nil && manager != nil {
			_, err := manager.Describe(request.Server, request.Tool)
			return nil, err
		}
		return nil, store.ErrNotFound
	case "search":
		catalog := map[string][]mcp.Tool{}
		for server, rows := range visible {
			if request.Server != "" && request.Server != server {
				continue
			}
			for _, row := range rows {
				catalog[server] = append(catalog[server], row.Tool)
			}
		}
		return mcp.SearchCatalog(catalog, request.Query, request.Limit)
	case "instructions":
		if manager == nil {
			return nil, store.ErrNotFound
		}
		text, generation, _, err := manager.Instructions(request.Server)
		if err != nil {
			return nil, err
		}
		resource := fmt.Sprintf("mcp_instructions_%x", sha256.Sum256([]byte(string(entry.root)+"\x00"+request.Server+"\x00"+text)))
		if current.ParentID != nil && !grants["mcp.instructions\x00"+resource] {
			return nil, store.ErrConflict
		}
		return map[string]any{"server": request.Server, "generation": generation, "text": text, "resource": resource}, nil
	}
	return nil, session.ErrInvalid
}

func (r *Runtime) mcpStatuses(ctx context.Context, current session.Session, entry *mcpRoot) ([]mcp.ServerStatus, error) {
	manager := r.mcpManager(entry)
	if manager != nil {
		result := []mcp.ServerStatus{}
		for _, row := range append(mcp.ServerStatuses(manager.Statuses()), mcp.ServerStatuses(manager.Blocked())...) {
			if current.Config.MCPServers.Contains(row.Name) {
				result = append(result, row)
			}
		}
		return append(result, mcp.ServerStatuses(manager.SourceErrors())...), nil
	}
	discovery, err := r.discoverMCP(ctx, current)
	if err != nil {
		return nil, err
	}
	result := []mcp.ServerStatus{}
	for _, name := range sortedMCPNames(discovery.config.Merged) {
		cfg := discovery.config.Merged[name]
		status := "not_started"
		if cfg.Disabled() {
			status = "disabled"
		}
		result = append(result, mcp.ServerStatus{Name: name, Status: status, Note: cfg.Note, Source: mcp.SourceLabel(cfg.Source)})
	}
	for _, name := range sortedMCPNames(discovery.config.Blocked) {
		result = append(result, mcp.ServerStatus{Name: name, Status: "blocked", Note: discovery.config.Blocked[name].Note})
	}
	for source := range discovery.config.Errs {
		result = append(result, mcp.ServerStatus{Name: mcp.SourceLabel(source), Status: "unreadable", Error: "source could not be read"})
	}
	return result, nil
}

func (r *Runtime) mcpMetadataValue(ctx context.Context, owner session.SessionID, id session.OperationID, value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(raw) <= 64<<10 {
		return value, nil
	}
	refs, err := r.mcpContentParts(ctx, owner, string(id), "metadata", "application/json", raw)
	return map[string]any{"content_parts": refs, "bytes": len(raw)}, err
}

func (r *Runtime) mcpContentParts(ctx context.Context, owner session.SessionID, id, part, media string, data []byte) ([]session.ContentReference, error) {
	if len(data) > 16<<20 {
		return nil, store.ErrLimit
	}
	refs := []session.ContentReference{}
	for index := 0; len(data) > 0; index++ {
		length := min(len(data), session.MaxContentBytes)
		identity := fmt.Sprintf("mcp_%x", sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d", id, part, index)))
		ref, err := r.PutContent(ctx, owner, identity, media, data[:length])
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
		data = data[length:]
	}
	return refs, nil
}

func (r *Runtime) mcpResult(ctx context.Context, owner session.SessionID, id session.OperationID, result capability.MCPResult, images *hostImages) (output tool.Output, err error) {
	value := map[string]any{"text": result.Text}
	defer func() {
		output.Value = value
		output.ContentReferences = append([]string(nil), images.refs...)
		if err != nil {
			value["remote_completed"] = true
			value["output_unavailable"] = true
		}
	}()
	if len(result.Text) > 64<<10 {
		refs, err := r.mcpContentParts(ctx, owner, string(id), "text", "text/plain", []byte(result.Text))
		if err != nil {
			return output, err
		}
		value["text"] = "MCP text is available in ordered content parts."
		value["text_parts"] = refs
		value["text_bytes"] = len(result.Text)
	}
	attachments := []any{}
	value["attachments"] = attachments
	for index, attachment := range result.Attachments {
		media := attachment.MIME
		if err := session.ValidateMediaType(media); err != nil {
			media = "application/octet-stream"
		}
		var refs []session.ContentReference
		var err error
		if hostImageMedia(media) {
			var ref session.ContentReference
			ref, err = r.publishHostImage(ctx, owner, id, index, media, attachment.Data, images)
			if err == nil {
				refs = []session.ContentReference{ref}
			}
		} else {
			refs, err = r.mcpContentParts(ctx, owner, string(id), fmt.Sprintf("attachment_%d", index), media, attachment.Data)
		}
		if err != nil {
			return output, err
		}
		attachments = append(attachments, map[string]any{"placeholder": strings.Clone(attachment.Placeholder), "media_type": media, "content_parts": refs, "bytes": len(attachment.Data)})
		value["attachments"] = attachments
	}
	value["attachments"] = attachments
	return output, nil
}
