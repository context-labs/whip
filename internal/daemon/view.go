package daemon

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

type HistoryPageParams = protocol.HistoryPageParams

func (s *Session) SnapshotView(ctx context.Context) (session.RootSnapshot, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (session.RootSnapshot, error) {
		s.questions.mu.Lock()
		defer s.questions.mu.Unlock()
		snapshot, err := s.store.SnapshotRootView(actorCtx, s.meta.ID, session.SnapshotViewOptions{RecentMessages: 64, CollectionLimit: 128, MaxBytes: 384 << 10})
		if err != nil {
			return snapshot, err
		}
		budget := 96 << 10
		snapshot.Questions = []session.LifecycleEvent{}
		for _, question := range s.questions.openLocked() {
			data, err := json.Marshal(question)
			if err != nil {
				return snapshot, err
			}
			if len(data) > budget {
				snapshot.Omitted["questions"] = true
				continue
			}
			budget -= len(data)
			snapshot.Questions = append(snapshot.Questions, question)
		}
		return snapshot, nil
	})
}

func (s *Server) historyPage(ctx context.Context, params HistoryPageParams) (session.BoundedTranscriptPage, error) {
	if params.Limit == 0 {
		params.Limit = 64
	}
	if params.MaxBytes == 0 {
		params.MaxBytes = 256 << 10
	}
	if params.AgentID == "" {
		params.AgentID = params.RootID
	}
	if params.ThroughSeq == 0 {
		params.ThroughSeq = -1
	}
	return s.daemon.store.ReadTranscriptPage(ctx, params.RootID, params.AgentID, session.TranscriptReadOptions{
		AfterSeq: params.AfterSeq, BeforeSeq: params.BeforeSeq, ThroughSeq: params.ThroughSeq, Revision: params.Revision, Limit: params.Limit, MaxBytes: params.MaxBytes, Recent: params.Recent,
	})
}

func (c *Client) HistoryPage(ctx context.Context, params HistoryPageParams) (session.BoundedTranscriptPage, error) {
	var page session.BoundedTranscriptPage
	err := c.Call(ctx, "history.page", params, &page)
	return page, err
}

// TracePage reads a root's spans after a cursor; see protocol.TracePageParams.
func (c *Client) TracePage(ctx context.Context, params protocol.TracePageParams) (session.SpanPage, error) {
	var page session.SpanPage
	err := c.Call(ctx, "trace.page", params, &page)
	return page, err
}

// traceExport renders the OTLP/JSON export and parks it in the root's content
// store, so any client fetches it through the existing bounded content reads.
func (s *Server) traceExport(ctx context.Context, params protocol.TraceExportParams) (protocol.TraceExportResult, error) {
	data, summary, err := s.daemon.store.ExportOTLP(ctx, params.RootID, session.ExportOptions{TraceID: params.TraceID, ServiceVersion: s.options.BuildID})
	if err != nil {
		return protocol.TraceExportResult{}, err
	}
	value, err := s.daemon.store.StoreContent(ctx, session.ContentGrant{RootID: params.RootID, Scope: session.ContentGrantRoot},
		session.RuntimePayload{Data: data, MediaType: "application/json", Source: "otlp export"})
	if err != nil {
		return protocol.TraceExportResult{}, err
	}
	result := protocol.TraceExportResult{Spans: summary.Spans, Traces: summary.Traces}
	if value.ReferenceID == "" {
		result.Inline = data
	} else {
		result.Content = ContentHandle{ReferenceID: value.ReferenceID, Digest: value.Digest, Size: value.Size, MediaType: value.MediaType, Source: value.Source}
	}
	return result, nil
}

// TraceExport renders a session as OTLP/JSON; see protocol.TraceExportParams.
func (c *Client) TraceExport(ctx context.Context, params protocol.TraceExportParams) (protocol.TraceExportResult, error) {
	var result protocol.TraceExportResult
	err := c.Call(ctx, "trace.export", params, &result)
	return result, err
}
