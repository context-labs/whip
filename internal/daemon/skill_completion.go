package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/rlm"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/skills"
)

const (
	maxSkillCompletionLimit = 1024
	maxSkillCompletionBytes = 1 << 20
)

func (s *Server) completeHostSkills(ctx context.Context, p protocol.HostSkillCompletionParams) (CompletionResult, error) {
	switch p.Scope {
	case "":
		if p.CWD == "" {
			return CompletionResult{}, errors.New("project skill completion requires cwd")
		}
	case "global":
		if p.CWD != "" {
			return CompletionResult{}, errors.New("global skill completion forbids cwd")
		}
	default:
		return CompletionResult{}, errors.New("invalid skill completion scope")
	}
	if len(p.CWD) > 4096 || len(p.Definition) > 256 || len(p.Prefix) > 4096 ||
		p.Limit < 1 || p.Limit > maxSkillCompletionLimit {
		return CompletionResult{}, errors.New("skill completion bounds are cwd and prefix up to 4096 bytes, definition up to 256 bytes and limit 1..1024")
	}
	if p.PermissionMode == "" {
		p.PermissionMode = session.PermissionModePrompt
	}
	if p.PermissionMode != session.PermissionModePrompt && p.PermissionMode != session.PermissionModeAutomatic {
		return CompletionResult{}, errors.New("invalid session permission mode")
	}
	if err := ctx.Err(); err != nil {
		return CompletionResult{}, err
	}
	if p.Definition == "" {
		p.Definition = "coding"
	}
	definition, _, err := latestDefinition(ctx, s.daemon.store, p.Definition)
	if err != nil {
		return CompletionResult{}, err
	}
	if p.Scope == "global" {
		roster, err := rlm.LoadGlobalPromptSkillsContext(ctx, definition.Instructions.SkillDiscovery)
		if err != nil {
			return CompletionResult{}, err
		}
		return completeSkillRoster(ctx, roster, p.Prefix, p.Limit)
	}
	// Open is the same read-only canonicalization used by root authority bootstrap.
	workspace, err := s.daemon.store.Workspaces().Open(p.CWD)
	if err != nil {
		return CompletionResult{}, err
	}
	root := workspace.Root()
	options := definition.PromptOptions(rlm.PromptOptions{WorkingDirectory: root, ProjectRoots: []string{root}})
	canRead := slices.Contains(rootGrants(definition, true).Files, "read")
	options.ProjectDirectoryAllowed = func(path string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !canRead {
			return false, nil
		}
		if p.PermissionMode == session.PermissionModeAutomatic {
			return true, nil
		}
		rel, err := filepath.Rel(root, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel), err
	}
	return completePromptSkills(ctx, options, p.Prefix, p.Limit)
}

// completionPromptOptions reconstructs only persisted definition and authority.
// Opening a picker must not open a runtime, restore children or issue new grants.
func (s *Server) completionPromptOptions(ctx context.Context, agent session.RuntimeAgent) (rlm.PromptOptions, error) {
	store := s.daemon.store
	meta, err := store.SessionDefinitionMeta(ctx, agent.RootID)
	if err != nil {
		return rlm.PromptOptions{}, err
	}
	definition, _, err := DefinitionFor(ctx, store, meta)
	if err != nil {
		return rlm.PromptOptions{}, err
	}
	var chain []session.RuntimeAgent
	for node := agent; node.ID != agent.RootID; {
		if len(chain) >= 256 || node.ParentID == "" {
			return rlm.PromptOptions{}, errors.New("invalid agent ancestry")
		}
		chain = append(chain, node)
		node, err = store.LoadAgent(ctx, agent.RootID, node.ParentID)
		if err != nil {
			return rlm.PromptOptions{}, err
		}
	}
	for _, node := range slices.Backward(chain) {
		name, err := store.AgentDefinitionName(ctx, agent.RootID, node.ID)
		if err != nil {
			return rlm.PromptOptions{}, err
		}
		// Instructions inherit exactly as at spawn/restore. Read authority is checked
		// separately against the durable grant chain, never the root's larger scope.
		definition, err = definition.Child(name, agentdef.ChildOverrides{})
		if err != nil {
			return rlm.PromptOptions{}, err
		}
	}
	authority, _, err := store.LoadAgentAuthority(ctx, agent.RootID, agent.ID)
	if err != nil {
		return rlm.PromptOptions{}, err
	}
	rootAuthority, _, err := store.LoadAgentAuthority(ctx, agent.RootID, agent.RootID)
	if err != nil {
		return rlm.PromptOptions{}, err
	}
	roots, err := store.CapabilityPaths(ctx, agent.RootID, agent.RootID, rootAuthority.Files)
	if err != nil && !errors.Is(err, capability.ErrDenied) {
		return rlm.PromptOptions{}, err
	}
	options := definition.PromptOptions(rlm.PromptOptions{WorkingDirectory: agent.CWD, ProjectRoots: roots})
	options.ProjectDirectoryAllowed = func(path string) (bool, error) {
		err := store.AuthorizeCapability(ctx, agent.RootID, agent.ID, authority.Files, "read", path)
		if errors.Is(err, capability.ErrDenied) {
			return false, nil
		}
		return err == nil, err
	}
	return options, nil
}

func completePromptSkills(ctx context.Context, options rlm.PromptOptions, prefix string, limit int) (CompletionResult, error) {
	result := CompletionResult{Candidates: []protocol.CompletionCandidate{}}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	roster, err := rlm.LoadPromptSkillsContext(ctx, options)
	if err != nil {
		return result, err
	}
	return completeSkillRoster(ctx, roster, prefix, limit)
}

func completeSkillRoster(ctx context.Context, roster []skills.Skill, prefix string, limit int) (CompletionResult, error) {
	result := CompletionResult{Candidates: []protocol.CompletionCandidate{}}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	// Explicit invocation's map assignment makes the last catalog entry win.
	winners := make(map[string]skills.Skill, len(roster))
	for _, skill := range roster {
		winners[skill.Name] = skill
		if skill.Warning != "" {
			if len(result.Warnings) < 16 {
				result.Warnings = append(result.Warnings, completionExcerpt(skill.Name+": "+skill.Warning, 512))
			} else {
				result.Truncated = true
			}
		}
	}
	names := make([]string, 0, len(winners))
	for name := range winners {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		skill := winners[name]
		if len(name) > 256 {
			result.Truncated = true
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if len(result.Candidates) == limit {
			result.Truncated = true
			continue
		}
		result.Candidates = append(result.Candidates, protocol.CompletionCandidate{Text: "$" + name, Description: string([]rune(skill.Description)[:min(80, len([]rune(skill.Description)))])})
	}
	result, err := boundSkillCompletion(result)
	if err != nil {
		return result, err
	}
	return result, ctx.Err()
}

// Count serialized bytes, including JSON escaping, fields and warnings. Drop only
// trailing candidates so a bounded response preserves the catalog ordering.
func boundSkillCompletion(result CompletionResult) (CompletionResult, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	size := len(encoded)
	if size <= maxSkillCompletionBytes {
		return result, nil
	}
	for size > maxSkillCompletionBytes && len(result.Candidates) > 0 {
		if !result.Truncated {
			size-- // JSON true is one byte shorter than false.
		}
		result.Truncated = true
		last := len(result.Candidates) - 1
		encoded, err := json.Marshal(result.Candidates[last])
		if err != nil {
			return result, err
		}
		size -= len(encoded)
		if last > 0 {
			size-- // Separating comma.
		}
		result.Candidates = result.Candidates[:last]
	}
	return result, nil
}

func completionExcerpt(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}
