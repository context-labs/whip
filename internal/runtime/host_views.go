package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/hostview"
	"github.com/context-labs/whip/internal/instruction"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/theme"
)

func (r *Runtime) HostDirectories(ctx context.Context, request hostview.DirectoryParams) (hostview.DirectoryResult, error) {
	if err := r.Err(); err != nil {
		return hostview.DirectoryResult{}, err
	}
	return hostview.Directories(ctx, request)
}

func (r *Runtime) PickHostDirectory(ctx context.Context, start string) (hostview.PickResult, error) {
	return r.hostPicker.Pick(ctx, start)
}

func (r *Runtime) HostThemes(ctx context.Context) (theme.CatalogResult, error) {
	if err := ctx.Err(); err != nil {
		return theme.CatalogResult{}, err
	}
	return theme.CatalogContext(ctx, filepath.Join(r.directory, "themes"))
}

func (r *Runtime) ResolveHostTheme(ctx context.Context, name, document string) (theme.Resolved, error) {
	if err := ctx.Err(); err != nil {
		return theme.Resolved{}, err
	}
	if (name == "") == (document == "") || len(name) > 256 || len(document) > theme.MaxJSONBytes || !utf8.ValidString(name+document) || strings.ContainsRune(name, 0) {
		return theme.Resolved{}, fmt.Errorf("%w: theme requires exactly one name or JSON document up to 65536 bytes", session.ErrInvalid)
	}
	if document != "" {
		value, err := theme.ResolveJSON([]byte(document))
		if err != nil {
			err = fmt.Errorf("%w: %w", session.ErrInvalid, err)
		}
		return value, err
	}
	return theme.ResolveContext(ctx, name, filepath.Join(r.directory, "themes"))
}

type HostSkillsRequest struct {
	Scope, CWD, Prefix string
	Definition         *session.DefinitionRef
	Limit              int
}
type HostSkillCandidate struct {
	Text        string `json:"text"`
	Description string `json:"description"`
}
type HostSkillsResult struct {
	Candidates []HostSkillCandidate `json:"candidates"`
	Truncated  bool                 `json:"truncated"`
}

// CompleteHostSkills previews metadata before root creation. Explicit human
// inspection never publishes grants or changes the session instruction catalog.
func (r *Runtime) CompleteHostSkills(ctx context.Context, p HostSkillsRequest) (HostSkillsResult, error) {
	result := HostSkillsResult{Candidates: []HostSkillCandidate{}}
	if p.Scope != "global" && p.Scope != "project" || p.Scope == "global" && p.CWD != "" || p.Scope == "project" && !filepath.IsAbs(p.CWD) || len(p.CWD) > 4096 || len(p.Prefix) > 4096 || !utf8.ValidString(p.CWD+p.Prefix) || strings.ContainsRune(p.CWD+p.Prefix, 0) || p.Limit < 1 || p.Limit > 1024 {
		return result, fmt.Errorf("%w: skill preview requires global scope without cwd or project scope with absolute cwd, bounded prefix and limit 1..1024", session.ErrInvalid)
	}
	snapshot, err := r.configuration.Snapshot(ctx)
	if err != nil {
		return result, err
	}
	document := session.Builtins()[0]
	if p.Definition != nil {
		definition, err := r.store.Definition(ctx, *p.Definition)
		if err != nil {
			return result, err
		}
		document = definition.Document
	}
	policy := snapshot.Host.Defaults.Instructions
	if document.Defaults.Instructions != nil {
		policy = *document.Defaults.Instructions
	}
	if err := policy.Validate(); err != nil {
		return result, err
	}
	var roots []instruction.Root
	defer func() {
		for _, root := range roots {
			_ = root.FS.Close()
		}
	}()
	if p.Scope == "project" {
		root, err := os.OpenRoot(p.CWD)
		if err != nil {
			return result, err
		}
		roots = append(roots, instruction.Root{FS: root})
	}
	if !policy.DiscoverSkills {
		return result, ctx.Err()
	}
	for _, id := range policy.SkillRoots {
		path, ok := snapshot.Host.SkillRoots[id]
		if !ok {
			return result, fmt.Errorf("%w: unknown skill root", session.ErrInvalid)
		}
		root, err := os.OpenRoot(path)
		if err != nil {
			return result, fmt.Errorf("host skill root %q unavailable", id)
		}
		roots = append(roots, instruction.Root{ID: id, FS: root})
	}
	if p.Scope == "project" && policy.ProjectRoot != nil {
		id := *policy.ProjectRoot
		boundary, ok := snapshot.Host.ProjectRoots[id]
		if !ok {
			return result, fmt.Errorf("%w: unknown project root", session.ErrInvalid)
		}
		project, err := openProjectInstructions(ctx, id, boundary, p.CWD)
		if err != nil {
			return result, err
		}
		if project != nil {
			_ = roots[0].FS.Close()
			roots[0] = *project
		}
	}
	catalog, err := instruction.Catalog(ctx, roots)
	if err != nil {
		return result, err
	}
	for _, skill := range catalog.Skills {
		if !strings.HasPrefix(skill.Name, p.Prefix) {
			continue
		}
		if len(result.Candidates) == p.Limit {
			result.Truncated = true
			break
		}
		description := []rune(skill.Description)
		if len(description) > 80 {
			description = description[:80]
		}
		result.Candidates = append(result.Candidates, HostSkillCandidate{Text: "$" + skill.Name, Description: string(description)})
	}
	return result, ctx.Err()
}
