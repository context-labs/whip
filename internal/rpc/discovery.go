package rpc

import (
	"context"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
)

func listTrees(ctx context.Context, r *runtime.Runtime, raw json.RawMessage) (any, error) {
	return decode(raw, func(p protocol.ListTreesParams) (any, error) {
		request := store.TreeList{Limit: p.Limit, Archived: p.Archived, Pinned: p.Pinned}
		if p.ExpectedRevision != nil {
			request.ExpectedRevision = new(session.Revision(*p.ExpectedRevision))
		}
		if p.After != nil {
			request.After = session.TreeID(*p.After)
		}
		page, err := r.Trees(ctx, request)
		if err != nil {
			return nil, err
		}
		result := protocol.ListTreesResult{Revision: protocol.Counter(page.Revision), Items: []protocol.TreeSummary{}}
		for _, item := range page.Items {
			result.Items = append(result.Items, protocol.TreeSummary{Tree: protocol.TreeFromDomain(item.Tree), RootID: protocol.ID(item.RootID)})
		}
		if page.Next != nil {
			result.NextCursor = new(protocol.ID(*page.Next))
		}
		return result, nil
	})
}

func listDefinitions(ctx context.Context, r *runtime.Runtime, raw json.RawMessage) (any, error) {
	return decode(raw, func(p protocol.ListDefinitionsParams) (any, error) {
		var after *session.DefinitionRef
		if p.After != nil {
			after = new(definitionRef(*p.After))
		}
		page, err := r.DefinitionSummaries(ctx, after, p.Limit)
		if err != nil {
			return nil, err
		}
		result := protocol.ListDefinitionsResult{Items: []protocol.DefinitionSummary{}}
		for _, item := range page.Items {
			result.Items = append(result.Items, protocol.DefinitionSummary{Ref: protocol.DefinitionRef{ID: protocol.ID(item.Ref.ID), Revision: item.Ref.Revision}, Name: item.Name, CreatedAt: item.CreatedAt.Format(time.RFC3339Nano)})
		}
		if page.Next != nil {
			result.NextCursor = &protocol.DefinitionRef{ID: protocol.ID(page.Next.ID), Revision: page.Next.Revision}
		}
		return result, nil
	})
}
