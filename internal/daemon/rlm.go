package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) AdmitAgent(ctx context.Context, admission sessionstore.AgentAdmission) error {
	admission.RootID = s.meta.ID
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.AdmitAgent(actorCtx, admission)
		return err
	})
}

type agentTurnItem struct {
	sessionstore.InboxItem
	Body []byte
}

// StartAgentTurn claims the next unit of work for a descendant through the
// root actor and resolves the claimed input body.
func (s *Session) StartAgentTurn(ctx context.Context, agentID, turnID string) (start sessionstore.AgentTurnStart, items []agentTurnItem, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		claimed, startErr := s.store.StartAgentTurn(actorCtx, s.meta.ID, agentID, turnID)
		if startErr != nil {
			return startErr
		}
		start = claimed
		items = make([]agentTurnItem, 0, len(claimed.Items))
		for _, item := range claimed.Items {
			body, resolveErr := s.store.ResolveRuntimeValue(actorCtx, s.meta.ID, item.Payload)
			if resolveErr != nil {
				return resolveErr
			}
			items = append(items, agentTurnItem{InboxItem: item, Body: body})
		}
		return nil
	})
	return start, items, err
}

func (s *Session) FinishAgentTurn(ctx context.Context, agentID string, commit sessionstore.AgentTurnCommit) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.store.FinishAgentTurn(actorCtx, s.meta.ID, agentID, commit)
	})
}

// SendMailboxMessage stores one message and nudges the recipient. The stored
// row is the durable wake condition; the nudge is only an optimization.
func (s *Session) SendMailboxMessage(ctx context.Context, senderAgentID, recipientAgentID string, send sessionstore.MailboxSend) (message sessionstore.MailboxMessage, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.consumeBudgets(actorCtx, senderAgentID, durableReservations(len(send.Subject)+len(send.Body)), func() error {
			message, err = s.store.SendMailboxMessage(actorCtx, s.meta.ID, senderAgentID, recipientAgentID, send)
			return err
		})
	})
	if err == nil {
		s.wakeAgent(recipientAgentID)
	}
	return message, err
}

func (s *Session) ListMailboxMessages(ctx context.Context, agentID, status, sender string, limit int) (messages []sessionstore.MailboxMessage, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		messages, err = s.store.ListMailboxMessages(actorCtx, s.meta.ID, agentID, status, sender, limit)
		return err
	})
	return messages, err
}

func (s *Session) ReadMailboxMessage(ctx context.Context, agentID, id string) (message sessionstore.MailboxMessage, body []byte, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		message, err = s.store.ReadMailboxMessage(actorCtx, s.meta.ID, agentID, id)
		if err != nil {
			return err
		}
		if message.Body.ReferenceID == "" {
			body = append([]byte(nil), message.Body.Inline...)
			return nil
		}
		body, _, err = s.store.ReadContent(actorCtx, message.Body.ReferenceID, s.meta.ID, agentID, 0, sessionstore.MaxContentRead)
		return err
	})
	return message, body, err
}

func (s *Session) CompleteMailboxMessages(ctx context.Context, agentID string, ids []string) (count int64, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		count, err = s.store.CompleteMailboxMessages(actorCtx, s.meta.ID, agentID, ids)
		return err
	})
	return count, err
}

// DeferMailboxMessage makes a message eligible again at until and arms an
// in-memory wake for that moment; durable state remains the truth if the
// daemon restarts first.
func (s *Session) DeferMailboxMessage(ctx context.Context, agentID, id string, until time.Time) error {
	err := s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.store.DeferMailboxMessage(actorCtx, s.meta.ID, agentID, id, until)
	})
	if err == nil {
		time.AfterFunc(max(time.Until(until), 0)+time.Second, func() { s.wakeAgent(agentID) })
	}
	return err
}

func (s *Session) MailboxSummary(ctx context.Context, agentID string) (summary sessionstore.MailboxSummary, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		summary, err = s.store.MailboxSummary(actorCtx, s.meta.ID, agentID)
		return err
	})
	return summary, err
}

func (s *Session) ReadMailboxDigest(ctx context.Context, agentID string) (digest sessionstore.MailboxDigest, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		digest, err = s.store.ReadMailboxDigest(actorCtx, s.meta.ID, agentID, time.Now())
		return err
	})
	return digest, err
}

func (s *Session) AgentWorkStatus(ctx context.Context, agentID string) (work sessionstore.AgentWork, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		work, err = s.store.AgentWorkStatus(actorCtx, s.meta.ID, agentID, time.Now())
		return err
	})
	return work, err
}

// HasAgentWork reports whether a node has anything runnable right now.
func (s *Session) HasAgentWork(ctx context.Context, agentID string) (bool, error) {
	work, err := s.AgentWorkStatus(ctx, agentID)
	return work.HasExplicitInput || work.HasReadyMail, err
}

func (s *Session) PendingSteers(ctx context.Context, agentID string) (items []sessionstore.InboxItem, mail []sessionstore.MailboxMessage, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		items, mail, err = s.store.PendingSteers(actorCtx, s.meta.ID, agentID, time.Now())
		return err
	})
	return items, mail, err
}

// SubmitAgentInput enqueues explicit work for a descendant on a caller's
// behalf: kind "steer" joins a running turn at its next boundary, "submit"
// waits for its own turn. The caller pays the durable-bytes budget.
func (s *Session) SubmitAgentInput(ctx context.Context, callerAgentID, agentID, kind, text, source string) (seq int64, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.consumeBudgets(actorCtx, callerAgentID, durableReservations(len(text)), func() error {
			sequence, enqueueErr := s.store.EnqueueInbox(actorCtx, sessionstore.InboxEnqueue{
				RootID: s.meta.ID, AgentID: agentID, Kind: kind,
				Payload: sessionstore.RuntimePayload{Data: []byte(text), MediaType: "text/plain", Source: source},
			})
			seq = sequence.InboxSeq
			return enqueueErr
		})
	})
	if err == nil {
		s.wakeAgent(agentID)
	}
	return seq, err
}

// LoadAgentScratch and SaveAgentScratch persist a node's Starlark scratch
// snapshot through the root actor.
func (s *Session) LoadAgentScratch(ctx context.Context, agentID string) (program string, manifest []byte, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		program, manifest, err = s.store.LoadAgentScratch(actorCtx, s.meta.ID, agentID)
		return err
	})
	return program, manifest, err
}

func (s *Session) SaveAgentScratch(ctx context.Context, agentID, program string, manifest []byte) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.store.SaveAgentScratch(actorCtx, s.meta.ID, agentID, program, manifest)
	})
}

// RecordScratchRestore appends the durable scratch.restored event for a node.
func (s *Session) RecordScratchRestore(ctx context.Context, agentID string, report rlm.RestoreReport) error {
	notRestored := make([]sessionstore.ScratchSkip, 0, len(report.Failed))
	for _, item := range report.Failed {
		notRestored = append(notRestored, sessionstore.ScratchSkip{Name: item.Name, Reason: item.Reason})
	}
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.RecordScratchRestore(actorCtx, s.meta.ID, agentID, report.Restored, notRestored)
		return err
	})
}

func (s *Session) wakeAgent(agentID string) {
	if agentID == s.authority.AgentID {
		s.notify()
		return
	}
	if runtime, ok := s.runtime.(interface{ WakeAgent(string) }); ok {
		runtime.WakeAgent(agentID)
	}
}

// reconcileAgentWork re-derives readiness for every node after a control
// change (permission decision, budget cap) that may have unblocked work. It
// runs off the actor so it is safe to call from a control callback.
func (s *Session) reconcileAgentWork() {
	s.supervisor.launchWorker("agent work reconciliation", func() {
		if runtime, ok := s.runtime.(interface{ WakeQueuedAgents() }); ok {
			runtime.WakeQueuedAgents()
		}
		s.notify()
	})
}

func (s *Session) LoadAgentTranscript(ctx context.Context, agentID string) (messages []llm.Message, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		messages, err = s.store.LoadAgentTranscript(actorCtx, s.meta.ID, agentID)
		return err
	})
	return messages, err
}

func (s *Session) LoadRetainedAgents(ctx context.Context) (agents []sessionstore.RuntimeAgent, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		agents, err = s.store.LoadRetainedAgents(actorCtx, s.meta.ID)
		return err
	})
	return agents, err
}

func (s *Session) LoadAgentAuthority(ctx context.Context, agentID string) (authority capability.Authority, names []string, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		authority, names, err = s.store.LoadAgentAuthority(actorCtx, s.meta.ID, agentID)
		return err
	})
	return authority, names, err
}

func (s *Session) ListAgentRelatives(ctx context.Context, callerAgentID string) (relatives sessionstore.AgentRelatives, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		relatives, err = s.store.ListAgentRelatives(actorCtx, s.meta.ID, callerAgentID)
		return err
	})
	return relatives, err
}

func (s *Session) TerminalizeSubtree(ctx context.Context, callerAgentID, targetAgentID, status string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.TerminalizeSubtree(actorCtx, s.meta.ID, callerAgentID, targetAgentID, status)
		return err
	})
}

func agentTurnID(agentID string) string {
	return fmt.Sprintf("%s:%d", agentID, time.Now().UnixNano())
}

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
