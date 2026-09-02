package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/context-labs/whip/internal/capability"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) StoreContent(ctx context.Context, callerAgentID string, payload sessionstore.RuntimePayload) (value sessionstore.RuntimeValue, err error) {
	if callerAgentID == "" {
		return sessionstore.RuntimeValue{}, errors.New("content caller is required")
	}
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.consumeBudgets(actorCtx, callerAgentID, durableReservations(len(payload.Data)), func() error {
			value, err = s.store.StoreContent(actorCtx, sessionstore.ContentGrant{
				RootID: s.meta.ID, AgentID: callerAgentID, Scope: sessionstore.ContentGrantAgent,
			}, payload)
			return err
		})
	})
	return value, err
}

func (s *Session) ReadContent(ctx context.Context, callerAgentID, referenceID string, offset int64, length int) (body []byte, metadata sessionstore.ContentMetadata, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		body, metadata, err = s.store.ReadContent(actorCtx, referenceID, s.meta.ID, callerAgentID, offset, length)
		return err
	})
	return body, metadata, err
}

func (s *Session) AddSchedule(ctx context.Context, expression, prompt string, anchor time.Time) (id int, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		reservations := append(durableReservations(len(expression)+len(prompt)), capability.Reservation{
			Kind: string(sessionstore.BudgetSchedulesSubscriptions), Amount: 1, Consume: true,
		})
		return s.consumeBudgets(actorCtx, s.authority.AgentID, reservations, func() error {
			id, err = s.store.AddSchedule(s.meta.ID, expression, prompt, anchor)
			return err
		})
	})
	return id, err
}

func (s *Session) ListSchedules(ctx context.Context) (schedules []sessionstore.Schedule, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		schedules, err = s.store.SchedulesContext(actorCtx, s.meta.ID)
		return err
	})
	return schedules, err
}

func (s *Session) CancelSchedule(ctx context.Context, id int) error {
	return s.routeControl(ctx, func(context.Context) error { return s.store.DeleteSchedule(s.meta.ID, id) })
}

func (s *Session) LaunchRuntimeWorker(kind string, work func()) bool {
	return s.supervisor.launchWorker(kind, work)
}

// ReceiveAgentMessages consumes queued peer messages addressed to the root
// while its current turn is active. Immediate messages continue to use the
// ordinary steering boundary instead of polling.
func (s *Session) ReceiveAgentMessages(ctx context.Context, callerAgentID string, limit int) (messages []sessionstore.AgentMessageEnvelope, err error) {
	if callerAgentID != s.authority.AgentID {
		return nil, sessionstore.ErrAgentAccess
	}
	if limit < 1 || limit > sessionstore.MaxInboxBatch {
		return nil, errors.New("message receive limit is out of range")
	}
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		kept := make([]sessionstore.InboxItem, 0, len(s.pending))
		for _, item := range s.pending {
			if item.Kind != "peer.message" || len(messages) == limit {
				kept = append(kept, item)
				continue
			}
			body, resolveErr := s.store.ResolveRuntimeValue(actorCtx, s.meta.ID, item.Payload)
			if resolveErr != nil {
				return resolveErr
			}
			var message sessionstore.AgentMessageEnvelope
			if decodeErr := json.Unmarshal(body, &message); decodeErr != nil {
				return decodeErr
			}
			if _, consumeErr := s.store.ConsumeInbox(actorCtx, s.meta.ID, callerAgentID, item.Seq); consumeErr != nil {
				return consumeErr
			}
			messages = append(messages, message)
		}
		s.pending = kept
		return nil
	})
	return messages, err
}
