package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/rlm"
	sessionstore "github.com/context-labs/whip/internal/session"
)

func (s *Session) AdmitAgent(ctx context.Context, admission sessionstore.AgentAdmission) error {
	admission.RootID = s.meta.ID
	_, err := routeControlOwnedValue(s, ctx, func(actorCtx context.Context) (int64, error) {
		return s.store.AdmitAgent(actorCtx, admission)
	})
	return err
}

// StartAgentTurn claims work only; the worker owns settlement before resolving input.
func (s *Session) StartAgentTurn(ctx context.Context, agentID, turnID string) (sessionstore.AgentTurnStart, error) {
	return routeControlOwnedValue(s, ctx, func(actorCtx context.Context) (sessionstore.AgentTurnStart, error) {
		return s.store.StartAgentTurn(actorCtx, s.meta.ID, agentID, turnID)
	})
}

func (s *Session) FinishAgentTurn(ctx context.Context, agentID string, commit sessionstore.AgentTurnCommit) error {
	_, err := routeControlOwnedValue(s, ctx, func(actorCtx context.Context) (struct{}, error) {
		return struct{}{}, s.store.FinishAgentTurn(actorCtx, s.meta.ID, agentID, commit)
	})
	return err
}

// SendMailboxMessage stores one message and nudges the recipient. The stored
// row is the durable wake condition; the nudge is only an optimization.
func (s *Session) SendMailboxMessage(ctx context.Context, senderAgentID, recipientAgentID string, send sessionstore.MailboxSend) (sessionstore.MailboxMessage, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.MailboxMessage, error) {
		var message sessionstore.MailboxMessage
		err := s.consumeBudgets(actorCtx, senderAgentID, durableReservations(len(send.Subject)+len(send.Body)), func() error {
			var err error
			message, err = s.store.SendMailboxMessage(actorCtx, s.meta.ID, senderAgentID, recipientAgentID, send)
			return err
		})
		if err == nil {
			s.wakeAgent(recipientAgentID)
		}
		return message, err
	})
}

func (s *Session) ListMailboxMessages(ctx context.Context, agentID, status, sender string, limit int) ([]sessionstore.MailboxMessage, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) ([]sessionstore.MailboxMessage, error) {
		return s.store.ListMailboxMessages(actorCtx, s.meta.ID, agentID, status, sender, limit)
	})
}

func (s *Session) ReadMailboxMessage(ctx context.Context, agentID, id string) (sessionstore.MailboxMessage, []byte, error) {
	type result struct {
		message sessionstore.MailboxMessage
		body    []byte
	}
	value, err := routeControlValue(s, ctx, func(actorCtx context.Context) (result, error) {
		message, err := s.store.ReadMailboxMessage(actorCtx, s.meta.ID, agentID, id)
		if err != nil {
			return result{}, err
		}
		body := append([]byte(nil), message.Body.Inline...)
		if message.Body.ReferenceID != "" {
			body, _, err = s.store.ReadContent(actorCtx, message.Body.ReferenceID, s.meta.ID, agentID, 0, sessionstore.MaxContentRead)
		}
		return result{message, body}, err
	})
	return value.message, value.body, err
}

func (s *Session) CompleteMailboxMessages(ctx context.Context, agentID string, receipts []sessionstore.MailboxReceipt) (int64, error) {
	receipts = slices.Clone(receipts)
	return routeControlValue(s, ctx, func(actorCtx context.Context) (int64, error) {
		return s.store.CompleteMailboxMessages(actorCtx, s.meta.ID, agentID, receipts)
	})
}

// DeferMailboxMessage makes a message eligible again at until and arms an
// in-memory wake for that moment; durable state remains the truth if the
// daemon restarts first.
func (s *Session) DeferMailboxMessage(ctx context.Context, agentID string, receipt sessionstore.MailboxReceipt, until time.Time) (int64, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (int64, error) {
		revision, err := s.store.DeferMailboxMessage(actorCtx, s.meta.ID, agentID, receipt, until)
		if err == nil {
			time.AfterFunc(max(time.Until(until), 0)+time.Second, func() { s.wakeAgent(agentID) })
		}
		return revision, err
	})
}

func (s *Session) MailboxSummary(ctx context.Context, agentID string) (sessionstore.MailboxSummary, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.MailboxSummary, error) {
		return s.store.MailboxSummary(actorCtx, s.meta.ID, agentID)
	})
}

func (s *Session) ReadMailboxDigest(ctx context.Context, agentID string) (sessionstore.MailboxDigest, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.MailboxDigest, error) {
		return s.store.ReadMailboxDigest(actorCtx, s.meta.ID, agentID, time.Now())
	})
}

func (s *Session) AgentWorkStatus(ctx context.Context, agentID string) (sessionstore.AgentWork, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.AgentWork, error) {
		return s.store.AgentWorkStatus(actorCtx, s.meta.ID, agentID, time.Now())
	})
}

// HasAgentWork reports whether a node has anything runnable right now.
func (s *Session) HasAgentWork(ctx context.Context, agentID string) (bool, error) {
	work, err := s.AgentWorkStatus(ctx, agentID)
	return work.HasExplicitInput || work.HasReadyMail, err
}

func (s *Session) ClaimSteers(ctx context.Context, agentID, turnID string) ([]sessionstore.InboxItem, error) {
	return routeControlOwnedValue(s, ctx, func(actorCtx context.Context) ([]sessionstore.InboxItem, error) {
		return s.store.ClaimSteers(actorCtx, s.meta.ID, agentID, turnID)
	})
}

func (s *Session) RejectTurnInput(ctx context.Context, agentID, turnID string, seq int64, cause error) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		if err := s.store.RejectTurnInput(actorCtx, s.meta.ID, agentID, turnID, seq, cause.Error()); err != nil {
			return err
		}
		if agentID == s.authority.AgentID {
			s.settle(seq, Completion{Sequence: seq, Err: cause})
		}
		return nil
	})
}

// SubmitAgentInput enqueues explicit work for a descendant on a caller's
// behalf: kind "steer" joins a running turn at its next boundary, "submit"
// waits for its own turn. The caller pays the durable-bytes budget.
func (s *Session) SubmitAgentInput(ctx context.Context, callerAgentID, agentID, kind, text, source string) (int64, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (int64, error) {
		var sequence sessionstore.InboxSequence
		err := s.consumeBudgets(actorCtx, callerAgentID, durableReservations(len(text)), func() error {
			var err error
			sequence, err = s.store.EnqueueInbox(actorCtx, sessionstore.InboxEnqueue{
				RootID: s.meta.ID, AgentID: agentID, Kind: kind,
				Payload: sessionstore.RuntimePayload{Data: []byte(text), MediaType: "text/plain", Source: source},
			})
			return err
		})
		if err == nil {
			s.wakeAgent(agentID)
		}
		return sequence.InboxSeq, err
	})
}

// LoadAgentScratch and SaveAgentScratch persist a node's Starlark scratch
// snapshot through the root actor.
func (s *Session) LoadAgentScratch(ctx context.Context, agentID string) (string, []byte, error) {
	type result struct {
		snapshot string
		manifest []byte
	}
	value, err := routeControlValue(s, ctx, func(actorCtx context.Context) (result, error) {
		snapshot, manifest, err := s.store.LoadAgentScratch(actorCtx, s.meta.ID, agentID)
		return result{snapshot, manifest}, err
	})
	return value.snapshot, value.manifest, err
}

func (s *Session) SaveAgentScratch(ctx context.Context, agentID, snapshot string, manifest []byte) error {
	manifest = slices.Clone(manifest)
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.store.SaveAgentScratch(actorCtx, s.meta.ID, agentID, snapshot, manifest)
	})
}

// RecordScratchRestore appends the durable scratch.restored event for a node.
func (s *Session) RecordScratchRestore(ctx context.Context, agentID string, report rlm.RestoreReport) error {
	report.Restored = slices.Clone(report.Restored)
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

func (s *Session) LoadAgentTranscript(ctx context.Context, agentID string) ([]llm.Message, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) ([]llm.Message, error) {
		return s.store.LoadAgentTranscript(actorCtx, s.meta.ID, agentID)
	})
}

func (s *Session) LoadRetainedAgents(ctx context.Context) ([]sessionstore.RuntimeAgent, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) ([]sessionstore.RuntimeAgent, error) {
		return s.store.LoadRetainedAgents(actorCtx, s.meta.ID)
	})
}

func (s *Session) LoadAgentAuthority(ctx context.Context, agentID string) (capability.Authority, []string, error) {
	type result struct {
		authority capability.Authority
		names     []string
	}
	value, err := routeControlValue(s, ctx, func(actorCtx context.Context) (result, error) {
		authority, names, err := s.store.LoadAgentAuthority(actorCtx, s.meta.ID, agentID)
		return result{authority, names}, err
	})
	return value.authority, value.names, err
}

func (s *Session) ListAgentRelatives(ctx context.Context, callerAgentID string) (sessionstore.AgentRelatives, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.AgentRelatives, error) {
		return s.store.ListAgentRelatives(actorCtx, s.meta.ID, callerAgentID)
	})
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

func (s *Session) StoreContent(ctx context.Context, callerAgentID string, payload sessionstore.RuntimePayload) (sessionstore.RuntimeValue, error) {
	payload.Data = slices.Clone(payload.Data)
	if callerAgentID == "" {
		return sessionstore.RuntimeValue{}, errors.New("content caller is required")
	}
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.RuntimeValue, error) {
		var value sessionstore.RuntimeValue
		err := s.consumeBudgets(actorCtx, callerAgentID, durableReservations(len(payload.Data)), func() error {
			var err error
			value, err = s.store.StoreContent(actorCtx, sessionstore.ContentGrant{RootID: s.meta.ID, AgentID: callerAgentID, Scope: sessionstore.ContentGrantAgent}, payload)
			return err
		})
		return value, err
	})
}

func (s *Session) ReadContent(ctx context.Context, callerAgentID, referenceID string, offset int64, length int) ([]byte, sessionstore.ContentMetadata, error) {
	type result struct {
		body     []byte
		metadata sessionstore.ContentMetadata
	}
	value, err := routeControlValue(s, ctx, func(actorCtx context.Context) (result, error) {
		body, metadata, err := s.store.ReadContent(actorCtx, referenceID, s.meta.ID, callerAgentID, offset, length)
		return result{body, metadata}, err
	})
	return value.body, value.metadata, err
}

func (s *Session) AddSchedule(ctx context.Context, expression, prompt string, anchor time.Time) (int, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (int, error) {
		reservations := append(durableReservations(len(expression)+len(prompt)), capability.Reservation{Kind: string(sessionstore.BudgetSchedulesSubscriptions), Amount: 1, Consume: true})
		var id int
		err := s.consumeBudgets(actorCtx, s.authority.AgentID, reservations, func() error {
			var err error
			id, err = s.store.AddSchedule(s.meta.ID, expression, prompt, anchor)
			return err
		})
		return id, err
	})
}

func (s *Session) ListSchedules(ctx context.Context) ([]sessionstore.Schedule, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) ([]sessionstore.Schedule, error) {
		return s.store.SchedulesContext(actorCtx, s.meta.ID)
	})
}

func (s *Session) CancelSchedule(ctx context.Context, id int) error {
	return s.routeControl(ctx, func(context.Context) error { return s.store.DeleteSchedule(s.meta.ID, id) })
}

func (s *Session) LaunchRuntimeWorker(kind string, work func()) bool {
	return s.supervisor.launchWorker(kind, work)
}
