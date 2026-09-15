package daemon

import (
	"errors"
	"maps"
	"path/filepath"
	"slices"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/mcp"
	"github.com/context-labs/whip/internal/protocol"
)

// The import screen's two host-level operations. They live beside the
// configuration service rather than among the session-scoped mcp.* actions
// because the New session screen has a host but no session yet.

// MCPImportCandidates lists the servers other agents configured on this
// host. It reads files only: no candidate is dialed or launched.
func (s *ProviderService) MCPImportCandidates(p protocol.MCPImportCandidatesParams) (protocol.MCPImportCandidatesResult, error) {
	if err := validateImportCWD(p.CWD); err != nil {
		return protocol.MCPImportCandidatesResult{}, err
	}
	cfg, _, err := config.ReadVersioned()
	if err != nil {
		return protocol.MCPImportCandidatesResult{}, err
	}
	cands, errs := mcp.Candidates(p.CWD, mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
	result := protocol.MCPImportCandidatesResult{
		Candidates: make([]protocol.MCPImportCandidate, 0, len(cands)),
		Offered:    cfg.MCPImport != nil && cfg.MCPImport.Offered,
	}
	result.ConfigPath, _ = config.Path()
	for _, c := range cands {
		result.Candidates = append(result.Candidates, protocol.MCPImportCandidate{
			Name: c.Name, Source: c.Source, State: string(c.State), Note: c.Note, BrandHint: c.BrandHint,
		})
	}
	if len(errs) > 0 {
		result.Errors = make(map[string]string, len(errs))
		for path, err := range errs {
			result.Errors[path] = err.Error()
		}
	}
	return result, nil
}

// MCPImportApply copies the named candidates into the host's native mcp block
// and records that the offer was answered. A name no source defines fails
// the whole call before anything is written; names that cannot be imported
// (already native, unsupported) come back in Skipped.
func (s *ProviderService) MCPImportApply(p protocol.MCPImportApplyParams) (protocol.MCPImportApplyResult, error) {
	if err := validateImportCWD(p.CWD); err != nil {
		return protocol.MCPImportApplyResult{}, err
	}
	if len(p.Names) > 256 {
		return protocol.MCPImportApplyResult{}, errors.New("too many server names")
	}
	var result protocol.MCPImportApplyResult
	// A second Skip or an apply with nothing new leaves the file alone.
	_, _, err := config.UpdateVersionedIfChanged("", func(cfg *config.Config) error {
		cands, _ := mcp.Candidates(p.CWD, mcp.FromConfigMap(cfg.MCPServers), mcp.ImportPolicyFrom(cfg.MCPImport))
		added, skipped, err := mcp.Apply(cfg, cands, p.Names)
		if err != nil {
			return err
		}
		if cfg.MCPImport == nil {
			cfg.MCPImport = &config.MCPImport{}
		}
		cfg.MCPImport.Offered = true
		result = protocol.MCPImportApplyResult{Imported: slices.Sorted(maps.Keys(added))}
		if len(skipped) > 0 {
			result.Skipped = skipped
		}
		return nil
	})
	return result, err
}

func validateImportCWD(cwd string) error {
	if cwd != "" && !filepath.IsAbs(cwd) {
		return errors.New("cwd must be an absolute path")
	}
	return nil
}
