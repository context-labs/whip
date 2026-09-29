package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/trace"
)

func (r *Runtime) TracePage(ctx context.Context, query session.TraceQuery) (session.TracePage, error) {
	return r.store.TracePage(ctx, query)
}

type TraceExport struct {
	Reference session.ContentReference
	Revision  int64
	Spans     int
	Traces    int
}

// ExportTrace materializes a complete fixed-revision OTLP body into the existing
// owner-scoped content store. It does not transmit it or automatically retry a
// conflicted read. Captured instructions are labeled separately from the full
// provider input, which can contain private state and is never reconstructed.
func (r *Runtime) ExportTrace(ctx context.Context, root session.SessionID, traceID string, expected *int64) (TraceExport, error) {
	var result TraceExport
	query := session.TraceQuery{RootID: root, TraceID: traceID, ExpectedRevision: expected, Limit: 2048, MaxBytes: 512 << 10}
	if err := query.Validate(); err != nil {
		return result, err
	}
	owner, err := r.store.Session(ctx, root)
	if err != nil {
		return result, err
	}
	if owner.ParentID != nil {
		return result, store.ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var export *trace.Export
	// Limit scan work as well as retained spans and bytes, including tombstones.
	for range 16 {
		page, err := r.store.TracePage(ctx, query)
		if err != nil {
			return result, err
		}
		if export == nil {
			result.Revision = page.Revision
			query.ExpectedRevision = &result.Revision
			export = trace.NewExport(root, result.Revision)
		}
		for _, row := range page.Items {
			var input, output *string
			if row.Span != nil && (row.SourceKind == "operation" || row.SourceKind == "cell" || row.SourceKind == "attempt") {
				input, output, err = r.store.TraceBodies(ctx, row, result.Revision)
				if err != nil {
					return result, err
				}
			}
			if row.Span != nil && row.SourceKind == "attempt" {
				evidence, err := r.ModelInspection(ctx, row.SessionID, session.ModelAttemptID(row.SourceID))
				if err != nil {
					return result, err
				}
				if capture := evidence.Capture; capture != nil {
					if capture.Instructions.Status == "available" && capture.Notices.Status == "available" {
						if capture.Instructions.Bytes+capture.Notices.Bytes > 1<<20 {
							return result, fmt.Errorf("%w: captured instructions exceed 1 MiB export limit", store.ErrLimit)
						}
						instructions, err := r.readCapturedText(ctx, row.SessionID, capture.Instructions, 1<<20)
						if err != nil {
							return result, err
						}
						notices, err := r.readCapturedText(ctx, row.SessionID, capture.Notices, 1<<20)
						if err != nil {
							return result, err
						}
						raw, err := json.Marshal(struct {
							Scope        string `json:"scope"`
							Instructions string `json:"instructions"`
							Notices      string `json:"notices"`
						}{"captured_instructions_only", instructions, notices})
						if err != nil {
							return result, err
						}
						if len(raw) > 1<<20 {
							return result, fmt.Errorf("%w: captured instruction JSON exceeds 1 MiB export limit", store.ErrLimit)
						}
						input = new(string(raw))
					}
				}
			}
			if err := export.Add(row, input, output); err != nil {
				return result, fmt.Errorf("%w: %w", store.ErrLimit, err)
			}
		}
		if !page.HasMore {
			body := export.Bytes()
			digest := sha256.Sum256(body)
			result.Reference, err = r.PutContent(ctx, root, "trace_"+hex.EncodeToString(digest[:]), "application/json", body)
			result.Spans, result.Traces = export.Counts()
			return result, err
		}
		query.After = page.Next
	}
	return result, fmt.Errorf("%w: trace export exceeds 16 bounded pages", store.ErrLimit)
}
