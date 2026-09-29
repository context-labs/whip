package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchTrace(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "trace.page":
		return decode(raw, func(p protocol.TracePageParams) (any, error) {
			query := session.TraceQuery{RootID: session.SessionID(p.RootID), After: int64(p.After), TraceID: p.TraceID, RootsOnly: p.RootsOnly, Limit: p.Limit, MaxBytes: p.MaxBytes}
			if p.ExpectedRevision != nil {
				query.ExpectedRevision = new(int64(*p.ExpectedRevision))
			}
			result, err := r.TracePage(ctx, query)
			return protocol.TracePageFromDomain(result), err
		})
	case "trace.export":
		return decode(raw, func(p protocol.TraceExportParams) (any, error) {
			var expected *int64
			if p.ExpectedRevision != nil {
				expected = new(int64(*p.ExpectedRevision))
			}
			result, err := r.ExportTrace(ctx, session.SessionID(p.RootID), p.TraceID, expected)
			return protocol.TraceExportResult{Reference: protocol.ContentReferenceFromDomain(result.Reference), Revision: protocol.Counter(result.Revision), Spans: result.Spans, Traces: result.Traces}, err
		})
	default:
		return nil, ErrMethod
	}
}
