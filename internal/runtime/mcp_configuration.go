package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/mcpconfig"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

type MCPDeclaration struct {
	Name                                      string
	Transport                                 string
	Enabled                                   bool
	StartupTimeoutSeconds, ToolTimeoutSeconds int
	BrandHint, BrandKey                       string
}

// MCPConfiguration exposes declarations without commands, credentials, headers,
// working directories or secret references. Updates use the same host authority.
type MCPConfiguration struct {
	Revision   string
	Servers    []MCPDeclaration
	Imports    mcpconfig.Import
	BrandIcons bool
}

func mcpConfiguration(snapshot config.Snapshot) MCPConfiguration {
	result := MCPConfiguration{Revision: snapshot.Revision, Servers: []MCPDeclaration{}, Imports: snapshot.Host.MCP.Imports, BrandIcons: snapshot.Host.MCP.BrandIcons == nil || *snapshot.Host.MCP.BrandIcons}
	for name, value := range mcp.NativeConfigs(snapshot.Host.MCP.Servers, "host") {
		row := MCPDeclaration{Name: name, Transport: "stdio", Enabled: !value.Disabled(), StartupTimeoutSeconds: int(value.StartupTimeoutDuration().Seconds()), ToolTimeoutSeconds: int(value.ToolTimeoutDuration().Seconds())}
		if value.Remote() {
			row.Transport = "http"
		}
		row.BrandHint, row.BrandKey = mcp.BrandMetadata(value)
		result.Servers = append(result.Servers, row)
	}
	slices.SortFunc(result.Servers, func(a, b MCPDeclaration) int { return strings.Compare(a.Name, b.Name) })
	return result
}

func (r *Runtime) MCPConfiguration(ctx context.Context) (MCPConfiguration, error) {
	snapshot, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return MCPConfiguration{}, err
	}
	return mcpConfiguration(snapshot), nil
}

// MCPConfigurationPatch replaces exactly the selected declaration or import
// policy. It performs no connection effect; refresh/reload are separate actions.
type MCPConfigurationPatch struct {
	ExpectedRevision string
	Name             string
	Server           *mcpconfig.Server
	Remove           bool
	Imports          *mcpconfig.Import
	BrandIcons       *bool
}

func (r *Runtime) ConfigureMCP(ctx context.Context, request MCPConfigurationPatch) (MCPConfiguration, error) {
	if (request.Name != "") != (request.Server != nil || request.Remove) || request.Server != nil && request.Remove || request.Name != "" && !mcpconfig.ValidName(request.Name) {
		return MCPConfiguration{}, fmt.Errorf("%w: invalid MCP configuration patch", session.ErrInvalid)
	}
	snapshot, err := r.configuration.Update(ctx, request.ExpectedRevision, func(host *config.Host) error {
		if request.Server != nil {
			if host.MCP.Servers == nil {
				host.MCP.Servers = map[string]mcpconfig.Server{}
			}
			host.MCP.Servers[request.Name] = *request.Server
		}
		if request.Remove {
			delete(host.MCP.Servers, request.Name)
		}
		if request.Imports != nil {
			host.MCP.Imports = *request.Imports
		}
		if request.BrandIcons != nil {
			host.MCP.BrandIcons = new(*request.BrandIcons)
		}
		return nil
	})
	if err != nil {
		return MCPConfiguration{}, err
	}
	return mcpConfiguration(snapshot), nil
}

type MCPImportDiscovery struct {
	Revision     string
	Candidates   []mcp.Candidate
	SourceErrors map[string]string
}

func (r *Runtime) discoverMCPImports(ctx context.Context, id session.SessionID) (config.Snapshot, []mcp.Candidate, map[string]string, error) {
	cwd := ""
	if id != "" {
		owner, err := r.store.Session(ctx, id)
		if err != nil {
			return config.Snapshot{}, nil, nil, err
		}
		if owner.ParentID != nil {
			return config.Snapshot{}, nil, nil, store.ErrConflict
		}
		cwd = owner.WorkingDirectory
	}
	snapshot, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return config.Snapshot{}, nil, nil, err
	}
	native := mcp.NativeConfigs(snapshot.Host.MCP.Servers, filepath.Join(r.directory, config.FileName))
	candidates, failures := mcp.Candidates(cwd, native, mcp.ImportPolicyFrom(&snapshot.Host.MCP.Imports))
	if err := ctx.Err(); err != nil {
		return config.Snapshot{}, nil, nil, err
	}
	raw, err := json.Marshal(candidates)
	if err != nil || len(candidates) > 256 || len(raw) > session.MaxDocumentBytes/2 {
		return config.Snapshot{}, nil, nil, store.ErrLimit
	}
	errors := map[string]string{}
	for source := range failures {
		errors[mcp.SourceLabel(source)] = "source could not be read"
	}
	return snapshot, candidates, errors, nil
}

func (r *Runtime) MCPImportCandidates(ctx context.Context, id session.SessionID) (MCPImportDiscovery, error) {
	snapshot, candidates, failures, err := r.discoverMCPImports(ctx, id)
	if err != nil {
		return MCPImportDiscovery{}, err
	}
	return MCPImportDiscovery{Revision: snapshot.Revision, Candidates: candidates, SourceErrors: failures}, nil
}

type MCPImportRequest struct {
	SessionID        session.SessionID
	ExpectedRevision string
	Fingerprints     map[string]string
}
type MCPImportResult struct {
	Configuration MCPConfiguration
	Added         []string
	Skipped       map[string]string
}

func (r *Runtime) ImportMCP(ctx context.Context, request MCPImportRequest) (MCPImportResult, error) {
	if len(request.Fingerprints) > 64 {
		return MCPImportResult{}, store.ErrLimit
	}
	if len(request.Fingerprints) == 0 {
		snapshot, err := r.configuration.Update(ctx, request.ExpectedRevision, func(host *config.Host) error { host.MCP.Imports.Offered = true; return nil })
		if err != nil {
			return MCPImportResult{}, err
		}
		return MCPImportResult{Configuration: mcpConfiguration(snapshot), Added: []string{}, Skipped: map[string]string{}}, nil
	}
	snapshot, candidates, _, err := r.discoverMCPImports(ctx, request.SessionID)
	if err != nil {
		return MCPImportResult{}, err
	}
	if snapshot.Revision != request.ExpectedRevision {
		return MCPImportResult{}, config.ErrRevisionConflict
	}
	names := make([]string, 0, len(request.Fingerprints))
	for name, fingerprint := range request.Fingerprints {
		index := slices.IndexFunc(candidates, func(candidate mcp.Candidate) bool {
			return candidate.Name == name && candidate.Fingerprint == fingerprint
		})
		if index < 0 {
			return MCPImportResult{}, store.ErrConflict
		}
		names = append(names, name)
	}
	slices.Sort(names)
	result := MCPImportResult{Added: []string{}, Skipped: map[string]string{}}
	updated, err := r.configuration.Update(ctx, request.ExpectedRevision, func(host *config.Host) error {
		added, skipped, err := mcp.Apply(&host.MCP.Servers, candidates, names)
		if err != nil {
			return err
		}
		for name := range added {
			result.Added = append(result.Added, name)
		}
		result.Skipped = skipped
		host.MCP.Imports.Offered = true
		return nil
	})
	if err != nil {
		return MCPImportResult{}, err
	}
	slices.Sort(result.Added)
	result.Configuration = mcpConfiguration(updated)
	return result, nil
}
