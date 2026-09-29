package rpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/mcpconfig"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

var errMCPUnavailable = errors.New("MCP operation unavailable; inspect configuration and connection status")

func dispatchMCP(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (result any, err error) {
	defer func() {
		if err != nil {
			err = errors.Join(errMCPUnavailable, err)
		}
	}()

	switch method {
	case "mcp.configuration":
		return decode(raw, func(protocol.EmptyParams) (any, error) {
			value, err := r.MCPConfiguration(ctx)
			return mcpConfiguration(value), err
		})
	case "mcp.configure":
		return decode(raw, func(p protocol.ConfigureMCPParams) (any, error) {
			request := runtime.MCPConfigurationPatch{ExpectedRevision: p.Revision, Name: p.Name, Remove: p.Remove, BrandIcons: p.BrandIcons}
			if p.Server != nil {
				request.Server = new(mcpServer(*p.Server))
			}
			if p.Imports != nil {
				request.Imports = new(mcpImports(*p.Imports))
			}
			value, err := r.ConfigureMCP(ctx, request)
			return mcpConfiguration(value), err
		})
	case "mcp.import.candidates":
		return decode(raw, func(p protocol.MCPImportCandidatesParams) (any, error) {
			value, err := r.MCPImportCandidates(ctx, mcpSession(p.SessionID))
			result := protocol.MCPImportCandidatesResult{Revision: value.Revision, Candidates: []protocol.MCPImportCandidate{}, SourceErrors: value.SourceErrors}
			for _, row := range value.Candidates {
				result.Candidates = append(result.Candidates, protocol.MCPImportCandidate{Fingerprint: row.Fingerprint, Name: row.Name, Source: row.Source, State: string(row.State), Gated: row.Gated, Note: row.Note, BrandHint: row.BrandHint, BrandKey: row.BrandKey})
			}
			return result, err
		})
	case "mcp.import.apply":
		return decode(raw, func(p protocol.MCPImportParams) (any, error) {
			value, err := r.ImportMCP(ctx, runtime.MCPImportRequest{SessionID: mcpSession(p.SessionID), ExpectedRevision: p.Revision, Fingerprints: p.Fingerprints})
			return protocol.MCPImportResult{Configuration: mcpConfiguration(value.Configuration), Added: value.Added, Skipped: value.Skipped}, err
		})
	case "mcp.status":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.MCPStatus(ctx, session.SessionID(p.SessionID))
			return protocol.MCPStatusResult{Items: mcpStatuses(value)}, err
		})
	case "mcp.refresh", "mcp.reload":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			var value mcp.RefreshResult
			var err error
			if method == "mcp.refresh" {
				value, err = r.MCPRefresh(ctx, session.SessionID(p.SessionID))
			} else {
				value, err = r.MCPReload(ctx, session.SessionID(p.SessionID))
			}
			return mcpRefresh(value), err
		})
	case "mcp.reconnect", "mcp.enable", "mcp.disable":
		return decode(raw, func(p protocol.MCPServerParams) (any, error) {
			var value mcp.RefreshResult
			var err error
			switch method {
			case "mcp.reconnect":
				value, err = r.MCPReconnect(ctx, session.SessionID(p.SessionID), p.Server)
			case "mcp.enable":
				value, err = r.MCPEnable(ctx, session.SessionID(p.SessionID), p.Server)
			case "mcp.disable":
				value, err = r.MCPDisable(ctx, session.SessionID(p.SessionID), p.Server)
			}
			return mcpRefresh(value), err
		})
	case "mcp.attach":
		return decode(raw, func(p protocol.MCPAttachParams) (any, error) {
			declarations := map[string]mcpconfig.Server{}
			for name, value := range p.Servers {
				declarations[name] = mcpServer(value)
			}
			value, err := r.MCPAttach(ctx, session.SessionID(p.SessionID), mcp.NativeConfigs(declarations, ""))
			return mcpRefresh(value), err
		})
	case "mcp.tools":
		return decode(raw, func(p protocol.MCPServerParams) (any, error) {
			values, err := r.MCPTools(ctx, session.SessionID(p.SessionID), p.Server)
			result := protocol.MCPToolsResult{Items: []protocol.MCPTool{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.MCPTool{Name: value.Name, Title: value.Title, Description: value.Description, InputSchema: value.InputSchema, Server: value.Server, Generation: value.Generation, Capability: value.Capability, Resource: value.Resource})
			}
			return result, err
		})
	case "mcp.instructions":
		return decode(raw, func(p protocol.MCPServerParams) (any, error) {
			value, err := r.MCPInstructions(ctx, session.SessionID(p.SessionID), p.Server)
			result := protocol.MCPInstructionsResult{Server: value.Server, Generation: value.Generation, Resource: value.Resource, Text: value.Text, Bytes: protocol.Counter(value.Bytes), ContentParts: []protocol.ContentReference{}}
			for _, ref := range value.ContentParts {
				result.ContentParts = append(result.ContentParts, protocol.ContentReferenceFromDomain(ref))
			}
			return result, err
		})
	case "mcp.brand.icons":
		return decode(raw, func(p protocol.MCPBrandIconsParams) (any, error) {
			value, err := r.MCPBrandIcons(ctx, p.Keys)
			return protocol.MCPBrandIconsResult{Icons: value}, err
		})
	}
	return nil, ErrMethod
}

func mcpSession(id *protocol.ID) session.SessionID {
	if id == nil {
		return ""
	}
	return session.SessionID(*id)
}

func mcpServer(value protocol.MCPServerInput) mcpconfig.Server {
	return mcpconfig.Server{Command: value.Command, Env: value.Env, Cwd: value.Cwd, URL: value.URL, Headers: value.Headers, Enabled: value.Enabled, Note: value.Note, StartupTimeout: value.StartupTimeoutSeconds, ToolTimeout: value.ToolTimeoutSeconds}
}

func mcpImports(value protocol.MCPImportPolicy) mcpconfig.Import {
	return mcpconfig.Import{Claude: mcpImportSource(value.Claude), Codex: mcpImportSource(value.Codex), Project: mcpImportSource(value.Project), Opencode: mcpImportSource(value.Opencode), Offered: value.Offered}
}

func mcpImportSource(value *protocol.MCPImportSource) *mcpconfig.ImportSource {
	if value == nil {
		return nil
	}
	return &mcpconfig.ImportSource{Enabled: value.Enabled, Only: value.Only, Exclude: value.Exclude}
}

func mcpImportSourceFromDomain(value *mcpconfig.ImportSource) *protocol.MCPImportSource {
	if value == nil {
		return nil
	}
	return &protocol.MCPImportSource{Enabled: value.Enabled, Only: append([]string{}, value.Only...), Exclude: append([]string{}, value.Exclude...)}
}

func mcpConfiguration(value runtime.MCPConfiguration) protocol.MCPConfiguration {
	result := protocol.MCPConfiguration{Revision: value.Revision, BrandIcons: value.BrandIcons, Servers: []protocol.MCPDeclaration{}, Imports: protocol.MCPImportPolicy{Claude: mcpImportSourceFromDomain(value.Imports.Claude), Codex: mcpImportSourceFromDomain(value.Imports.Codex), Project: mcpImportSourceFromDomain(value.Imports.Project), Opencode: mcpImportSourceFromDomain(value.Imports.Opencode), Offered: value.Imports.Offered}}
	for _, row := range value.Servers {
		result.Servers = append(result.Servers, protocol.MCPDeclaration{Name: row.Name, Transport: row.Transport, Enabled: row.Enabled, StartupTimeoutSeconds: row.StartupTimeoutSeconds, ToolTimeoutSeconds: row.ToolTimeoutSeconds, BrandHint: row.BrandHint, BrandKey: row.BrandKey})
	}
	return result
}

func mcpStatuses(values []mcp.ServerStatus) []protocol.MCPServerStatus {
	result := make([]protocol.MCPServerStatus, 0, len(values))
	for _, value := range values {
		row := protocol.MCPServerStatus{Name: value.Name, State: value.Status, Note: value.Note, Tools: value.Tools, Source: mcp.SourceLabel(value.Source)}
		if value.Error != "" {
			row.Failure = new("MCP connection or discovery is unavailable.")
		}
		result = append(result, row)
	}
	return result
}

func mcpRefresh(value mcp.RefreshResult) protocol.MCPRefreshResult {
	return protocol.MCPRefreshResult{Added: append([]string{}, value.Added...), Existing: append([]string{}, value.Existing...), Changed: append([]string{}, value.Changed...), Servers: mcpStatuses(value.Servers), Blocked: mcpStatuses(value.Blocked), SourceErrors: mcpStatuses(value.SourceErrors)}
}
