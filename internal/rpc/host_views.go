package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/hostview"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/theme"
)

func dispatchHostViews(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "host.directories.list":
		return decode(raw, func(p protocol.HostDirectoriesParams) (any, error) {
			value, err := r.HostDirectories(ctx, hostview.DirectoryParams{Path: p.Path, After: p.After, Prefix: p.Prefix, ShowHidden: p.ShowHidden, Limit: p.Limit})
			result := protocol.HostDirectoriesResult{Path: value.Path, Parent: value.Parent, Entries: []protocol.HostDirectoryEntry{}, NextAfter: value.NextAfter, HasMore: value.HasMore, Truncated: value.Truncated}
			for _, entry := range value.Entries {
				result.Entries = append(result.Entries, protocol.HostDirectoryEntry(entry))
			}
			return result, err
		})
	case "host.directory.pick":
		return decode(raw, func(p protocol.HostDirectoryPickParams) (any, error) {
			value, err := r.PickHostDirectory(ctx, p.Start)
			return protocol.HostDirectoryPickResult(value), err
		})
	case "host.skills.complete":
		return decode(raw, func(p protocol.HostSkillsParams) (any, error) {
			request := runtime.HostSkillsRequest{Scope: p.Scope, CWD: p.CWD, Prefix: p.Prefix, Limit: p.Limit}
			if p.Definition != nil {
				request.Definition = &session.DefinitionRef{ID: string(p.Definition.ID), Revision: p.Definition.Revision}
			}
			value, err := r.CompleteHostSkills(ctx, request)
			result := protocol.HostSkillsResult{Candidates: []protocol.HostSkillCandidate{}, Truncated: value.Truncated}
			for _, candidate := range value.Candidates {
				result.Candidates = append(result.Candidates, protocol.HostSkillCandidate(candidate))
			}
			return result, err
		})
	case "host.themes.list":
		value, err := r.HostThemes(ctx)
		result := protocol.HostThemesResult{Themes: []protocol.HostThemeMetadata{}, Errors: []protocol.HostThemeError{}, Truncated: value.Truncated}
		for _, item := range value.Themes {
			result.Themes = append(result.Themes, protocol.HostThemeMetadata(item))
		}
		for _, item := range value.Errors {
			result.Errors = append(result.Errors, protocol.HostThemeError(item))
		}
		return result, err
	case "host.themes.resolve":
		return decode(raw, func(p protocol.HostThemeResolveParams) (any, error) {
			value, err := r.ResolveHostTheme(ctx, p.Name, p.JSON)
			return resolvedTheme(value), err
		})
	default:
		return nil, ErrMethod
	}
}

func resolvedTheme(value theme.Resolved) protocol.HostThemeResolved {
	result := protocol.HostThemeResolved{ID: value.ID, Name: value.Name, Dark: value.Dark, Colors: protocol.HostThemeColors(value.Colors), Syntax: protocol.HostThemeSyntaxSpec(value.Syntax), Markdown: protocol.HostThemeMarkdownSpec(value.Markdown), Code: protocol.HostThemeCodeStyle{Foreground: value.Code.Foreground, Background: value.Code.Background, Tokens: map[string]protocol.HostThemeTokenStyle{}}}
	for key, token := range value.Code.Tokens {
		result.Code.Tokens[key] = protocol.HostThemeTokenStyle(token)
	}
	if value.Web != nil {
		result.Web = new(protocol.HostThemeWebColors(*value.Web))
	}
	return result
}
