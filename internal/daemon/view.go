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
