package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchContext(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "sessions.compact":
		return decode(raw, func(p protocol.CompactParams) (any, error) {
			value, err := r.AdmitCompaction(ctx, identity(p.Identity), session.SessionID(p.SessionID))
			return admission(value), err
		})
	case "context.head":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.ContextHead(ctx, session.SessionID(p.SessionID))
			return protocol.ContextHeadFromDomain(value), err
		})
	case "context.compaction":
		return decode(raw, func(p protocol.CompactionParams) (any, error) {
			value, err := r.Compaction(ctx, session.SessionID(p.SessionID), session.CompactionID(p.CompactionID))
			return protocol.CompactionResult{Metadata: protocol.CompactionFromDomain(value.CompactionMetadata), Text: value.Text}, err
		})
	case "context.compactions":
		return decode(raw, func(p protocol.CompactionsParams) (any, error) {
			var after session.CompactionID
			if p.After != nil {
				after = session.CompactionID(*p.After)
			}
			values, err := r.Compactions(ctx, session.SessionID(p.SessionID), after, p.Limit)
			result := protocol.CompactionsResult{Items: []protocol.CompactionMetadata{}}
			for _, value := range values {
				result.Items = append(result.Items, protocol.CompactionFromDomain(value))
			}
			return result, err
		})
	case "context.select":
		return decode(raw, func(p protocol.SelectCompactionParams) (any, error) {
			var id *session.CompactionID
			if p.CompactionID != nil {
				id = new(session.CompactionID(*p.CompactionID))
			}
			value, err := r.SelectCompaction(ctx, session.SessionID(p.SessionID), int64(p.ExpectedRevision), id)
			return protocol.ContextHeadFromDomain(value), err
		})
	case "context.snapshot":
		return decode(raw, func(p protocol.SessionParams) (any, error) {
			value, err := r.HistorySnapshot(ctx, session.SessionID(p.SessionID))
			return protocol.HistorySnapshotFromDomain(value), err
		})
	case "context.list":
		return decode(raw, func(p protocol.ContextHistoryParams) (any, error) {
			value, err := r.HistoryMetadataAtRevision(ctx, session.SessionID(p.SessionID), int64(p.After), int64(p.ThroughSequence), p.Limit, expectedHistoryRevision(p.ExpectedRevision))
			return protocol.HistoryPageFromDomain(value), err
		})
	case "context.read":
		return decode(raw, func(p protocol.ReadHistoryParams) (any, error) {
			value, err := r.ReadHistoryMessage(ctx, session.SessionID(p.SessionID), session.MessageID(p.MessageID), int64(p.Offset), p.Length)
			return protocol.HistoryReadFromDomain(value), err
		})
	case "context.search":
		return decode(raw, func(p protocol.SearchHistoryParams) (any, error) {
			value, err := r.SearchHistoryAtRevision(ctx, session.SessionID(p.SessionID), int64(p.After), int64(p.ThroughSequence), p.Query, p.Limit, expectedHistoryRevision(p.ExpectedRevision))
			return protocol.HistorySearchFromDomain(value), err
		})
	default:
		return nil, ErrMethod
	}
}
