package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/context-labs/whip/internal/agent"
	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/llm"
	sessionstore "github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tools"
)

type liveSubagent struct {
	agent       *agent.Agent
	agentID     string
	executionID string
	running     bool
	inbox       []int64
}

type subagentModelBudget struct {
	root   *Session
	taskID string
}

func (b subagentModelBudget) ReserveModelCall(ctx context.Context, amount int64) (func(llm.Usage) error, error) {
	var agentID string
	err := b.root.routeControl(ctx, func(context.Context) error {
		child := b.root.children[b.taskID]
		if child == nil {
			return sessionstore.ErrAgentTerminal
		}
		agentID = child.agentID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return b.root.reserveModelCall(ctx, agentID, amount)
}

func (s *Session) AdmitSubagent(ctx context.Context, taskID string, child *agent.Agent) error {
	return s.admitSubagent(ctx, taskID, child, []string{"write", "shell", "browser", "computer"}, nil)
}

// AdmitRLMSubagent admits only the explicitly requested capability and budget
// subsets. The returned live child still uses the shared dispatcher services.
func (s *Session) AdmitRLMSubagent(ctx context.Context, taskID string, child *agent.Agent, capabilities []string, budgets []sessionstore.BudgetLimit) error {
	return s.admitSubagent(ctx, taskID, child, capabilities, budgets)
}

func (s *Session) admitSubagent(ctx context.Context, taskID string, child *agent.Agent, requested []string, budgets []sessionstore.BudgetLimit) error {
	if taskID == "" || child == nil {
		return errors.New("durable subagent requires task identity and agent")
	}
	agentID := s.authority.AgentID + ":" + taskID
	executionID := agentID + ":turn"
	authority := capability.ClassicAuthority{
		RootID: s.meta.ID, AgentID: agentID,
		Files: capability.Reference{ID: "child-files:" + agentID, Generation: 1},
		Shell: capability.Reference{ID: "child-shell:" + agentID, Generation: 1},
	}
	services, err := child.Services.CloneForAuthority(s.store, s.store.Workspaces(), s.store.Processes(), authority)
	if err != nil {
		return err
	}
	if runner, ok := s.runner.(clientPermissionRunner); ok {
		services.SetExternalPermissions(runner.ExternalPermissionsEnabled())
	}
	var fileOperations, shellOperations []string
	var browserRequested, computerRequested bool
	for _, name := range requested {
		switch name {
		case "read":
			fileOperations = append(fileOperations, "read")
		case "write":
			fileOperations = append(fileOperations, "read", "write", "edit", "workspace.write")
		case "shell":
			shellOperations = append(shellOperations, "bash", "workspace_process")
		case "browser":
			shellOperations = append(shellOperations, "browser_exec")
			browserRequested = true
		case "computer":
			shellOperations = append(shellOperations, "computer_exec")
			computerRequested = true
		default:
			return fmt.Errorf("unknown child capability %q", name)
		}
	}
	slices.Sort(fileOperations)
	fileOperations = slices.Compact(fileOperations)
	slices.Sort(shellOperations)
	shellOperations = slices.Compact(shellOperations)
	var delegations []sessionstore.CapabilityDelegation
	if len(fileOperations) > 0 {
		delegations = append(delegations, sessionstore.CapabilityDelegation{
			ID: authority.Files.ID, Issuer: s.authority.Files, AgentID: agentID, Operations: fileOperations, Scopes: []string{s.meta.CWD},
		})
	}
	if len(shellOperations) > 0 {
		delegations = append(delegations, sessionstore.CapabilityDelegation{
			ID: authority.Shell.ID, Issuer: s.authority.Shell, AgentID: agentID, Operations: shellOperations,
		})
	}
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		if _, exists := s.children[taskID]; exists {
			return fmt.Errorf("subagent %s already exists", taskID)
		}
		if _, err := s.store.AdmitChild(actorCtx, sessionstore.ChildAdmission{
			RootID: s.meta.ID, ParentAgentID: s.authority.AgentID, ChildAgentID: agentID,
			ExecutionID: executionID, Capabilities: delegations, Budgets: budgets,
		}); err != nil {
			return err
		}
		child.Services = services
		child.Tools = tools.AllWithServices(services)
		if browserRequested && !child.BrowserDisabled && services.Browser() != nil {
			child.Tools = append(child.Tools, tools.BrowserExec(services))
		}
		if computerRequested && !child.ComputerDisabled && services.ComputerPolicy() != nil {
			child.Tools = append(child.Tools, tools.ComputerExec(services))
		}
		child.SetModelCallBudget(subagentModelBudget{root: s, taskID: taskID})
		s.children[taskID] = &liveSubagent{agent: child, agentID: agentID, executionID: executionID}
		return nil
	})
	if errors.Is(err, ErrStopped) {
		return context.Canceled
	}
	return err
}

func (s *Session) StartSubagent(ctx context.Context, taskID string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		child := s.children[taskID]
		if child == nil {
			return sessionstore.ErrAgentAccess
		}
		if child.running {
			return errors.New("subagent turn is already running")
		}
		if _, err := s.store.StartChildTurn(actorCtx, s.meta.ID, s.authority.AgentID, child.executionID); err != nil {
			return err
		}
		child.running = true
		if err := s.deliverChildInbox(actorCtx, child); err != nil {
			child.running = false
			_, finishErr := s.store.FinishChildTurn(actorCtx, s.meta.ID, s.authority.AgentID, child.executionID, "interrupted")
			return errors.Join(err, finishErr)
		}
		return nil
	})
}

func (s *Session) deliverChildInbox(ctx context.Context, child *liveSubagent) error {
	var after int64
	for {
		items, err := s.store.LoadQueuedInbox(ctx, s.meta.ID, child.agentID, after, sessionstore.MaxInboxBatch)
		if err != nil {
			return err
		}
		for _, item := range items {
			body := item.Payload.Inline
			if item.Payload.ReferenceID != "" {
				var err error
				body, _, err = s.store.ReadContent(ctx, item.Payload.ReferenceID, s.meta.ID, child.agentID, 0, sessionstore.MaxContentRead)
				if err != nil {
					return err
				}
			}
			child.agent.Steer(string(body))
			child.inbox = append(child.inbox, item.Seq)
		}
		if len(items) < sessionstore.MaxInboxBatch {
			return nil
		}
		after = items[len(items)-1].Seq
	}
}

func (s *Session) FinishSubagent(ctx context.Context, taskID string, status agent.TaskStatus) error {
	if durableStatus(status) == "" {
		return fmt.Errorf("invalid subagent status %q", status)
	}
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		child := s.children[taskID]
		if child == nil {
			return sessionstore.ErrAgentAccess
		}
		return s.finishSubagent(actorCtx, child, status)
	})
}

// FinishRLMSubagent persists the standalone child's transcript before the
// durable execution is terminalized and its incorporated inbox is consumed.
func (s *Session) FinishRLMSubagent(ctx context.Context, taskID, prompt, report string, status agent.TaskStatus, started time.Time, transcript []llm.Message, model, provider string) error {
	if durableStatus(status) == "" {
		return fmt.Errorf("invalid subagent status %q", status)
	}
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		child := s.children[taskID]
		if child == nil {
			return sessionstore.ErrAgentAccess
		}
		task := sessionstore.Task{
			ID: taskID, Description: "RLM durable child", Prompt: prompt, Status: string(status), Report: report,
			StartedAt: started, EndedAt: time.Now(),
		}
		if err := s.store.RecordClassicTaskTranscript(actorCtx, s.meta.ID, child.agentID, task, transcript, model, provider); err != nil {
			return err
		}
		return s.finishSubagent(actorCtx, child, status)
	})
}

func (s *Session) finishSubagent(ctx context.Context, child *liveSubagent, status agent.TaskStatus) error {
	if !child.running {
		return nil
	}
	acknowledged := child.inbox
	if status != agent.TaskDone {
		acknowledged = nil
	}
	_, err := s.store.FinishChildTurnWithInbox(ctx, s.meta.ID, s.authority.AgentID, child.executionID, durableStatus(status), acknowledged)
	if errors.Is(err, sessionstore.ErrAgentTerminal) && status == agent.TaskCancelled {
		child.running = false
		return nil
	}
	if err == nil {
		child.running = false
		child.inbox = nil
	}
	return err
}

func durableStatus(status agent.TaskStatus) string {
	switch status {
	case agent.TaskDone:
		return "succeeded"
	case agent.TaskError:
		return "failed"
	case agent.TaskCancelled:
		return "cancelled"
	default:
		return ""
	}
}

func (s *Session) SteerSubagent(ctx context.Context, taskID, text string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		child := s.children[taskID]
		if child == nil || !child.running {
			return sessionstore.ErrAgentTerminal
		}
		sequence, err := s.store.SendAgentMessage(actorCtx, s.meta.ID, s.authority.AgentID, child.agentID, sessionstore.AgentMessage{
			Delivery: sessionstore.DeliveryImmediate, Body: text,
		})
		if err != nil {
			return err
		}
		child.agent.Steer(text)
		_, err = s.store.ConsumeInbox(actorCtx, s.meta.ID, child.agentID, sequence.InboxSeq)
		return err
	})
}

func (s *Session) StopSubagent(taskID string) {
	err := s.routeControl(context.Background(), func(actorCtx context.Context) error {
		child := s.children[taskID]
		if child == nil {
			return sessionstore.ErrAgentAccess
		}
		child.running = false
		_, err := s.store.TerminalizeSubtree(actorCtx, s.meta.ID, s.authority.AgentID, child.agentID, "stopped")
		if errors.Is(err, sessionstore.ErrAgentTerminal) {
			return nil
		}
		return err
	})
	if err != nil && !errors.Is(err, ErrStopped) {
		s.supervisor.report("stop subagent", err)
	}
}

func (s *Session) ReleaseSubagent(taskID string) {
	err := s.routeControl(context.Background(), func(context.Context) error {
		delete(s.children, taskID)
		return nil
	})
	if err != nil && !errors.Is(err, ErrStopped) {
		s.supervisor.report("release subagent", err)
	}
}

func (s *Session) AdmitChild(ctx context.Context, parentAgentID, childAgentID, executionID string, budgets ...sessionstore.BudgetLimit) error {
	return s.AdmitChildWithCapabilities(ctx, parentAgentID, childAgentID, executionID, nil, budgets...)
}

// AdmitChildWithCapabilities commits the child and delegated grants together;
// callers may start child work only after this actor-routed call returns.
func (s *Session) AdmitChildWithCapabilities(ctx context.Context, parentAgentID, childAgentID, executionID string, capabilities []sessionstore.CapabilityDelegation, budgets ...sessionstore.BudgetLimit) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.AdmitChild(actorCtx, sessionstore.ChildAdmission{
			RootID: s.meta.ID, ParentAgentID: parentAgentID, ChildAgentID: childAgentID, ExecutionID: executionID,
			Budgets: budgets, Capabilities: capabilities,
		})
		return err
	})
}

func (s *Session) StartChildTurn(ctx context.Context, callerAgentID, executionID string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.StartChildTurn(actorCtx, s.meta.ID, callerAgentID, executionID)
		return err
	})
}

func (s *Session) FinishChildTurn(ctx context.Context, callerAgentID, executionID, status string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.FinishChildTurn(actorCtx, s.meta.ID, callerAgentID, executionID, status)
		return err
	})
}

func (s *Session) ListAgentRelatives(ctx context.Context, callerAgentID string) (relatives sessionstore.AgentRelatives, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		relatives, err = s.store.ListAgentRelatives(actorCtx, s.meta.ID, callerAgentID)
		return err
	})
	return relatives, err
}

func (s *Session) SendAgentMessage(ctx context.Context, senderAgentID, recipientAgentID string, message sessionstore.AgentMessage) (sequence sessionstore.InboxSequence, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		return s.consumeBudgets(actorCtx, senderAgentID, durableReservations(len(message.Body)), func() error {
			sequence, err = s.store.SendAgentMessage(actorCtx, s.meta.ID, senderAgentID, recipientAgentID, message)
			if err != nil {
				return err
			}
			for _, child := range s.children {
				if child.agentID == recipientAgentID && child.running {
					payload, marshalErr := json.Marshal(sessionstore.AgentMessageEnvelope{
						SenderAgentID: senderAgentID, RecipientAgentID: recipientAgentID, Delivery: message.Delivery,
						Body: message.Body, EvidenceReferenceID: message.EvidenceReferenceID,
					})
					if marshalErr != nil {
						return marshalErr
					}
					child.agent.Steer(string(payload))
					child.inbox = append(child.inbox, sequence.InboxSeq)
					return nil
				}
			}
			return nil
		})
	})
	return sequence, err
}

func (s *Session) TerminalizeSubtree(ctx context.Context, callerAgentID, targetAgentID, status string) error {
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		_, err := s.store.TerminalizeSubtree(actorCtx, s.meta.ID, callerAgentID, targetAgentID, status)
		return err
	})
}

// routeControl admits one reply-bearing operation to the existing actor queue.
// Once admitted, it waits for the durable outcome rather than returning an
// uncertain cancellation to the caller.
func (s *Session) routeControl(ctx context.Context, control func(context.Context) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.admitMu.RLock()
	if s.stopping {
		s.admitMu.RUnlock()
		return ErrStopped
	}
	reply := make(chan error, 1)
	s.supervisor.post(workerEnvelope{kind: workerControl, control: control, reply: reply})
	s.admitMu.RUnlock()
	return <-reply
}
