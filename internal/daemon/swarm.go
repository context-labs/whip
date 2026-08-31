package daemon

import (
	"context"
	"errors"
	"fmt"

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
}

type subagentModelBudget struct {
	root   *Session
	taskID string
}

func (b subagentModelBudget) ReserveModelCall(ctx context.Context, amount int64) (func(llm.Usage) error, error) {
	reservation := []capability.Reservation{{Kind: string(sessionstore.BudgetTokens), Amount: amount}}
	err := b.root.routeControl(ctx, func(actorCtx context.Context) error {
		child := b.root.children[b.taskID]
		if child == nil {
			return sessionstore.ErrAgentTerminal
		}
		return b.root.store.ReserveBudget(actorCtx, b.root.meta.ID, child.agentID, reservation)
	})
	if err != nil {
		return nil, err
	}
	return func(usage llm.Usage) error {
		var actual []capability.Usage
		if tokens := usage.PromptTokens + usage.CompletionTokens; tokens > 0 {
			actual = []capability.Usage{{Kind: string(sessionstore.BudgetTokens), Amount: int64(tokens)}}
		}
		return b.root.routeControl(context.Background(), func(actorCtx context.Context) error {
			child := b.root.children[b.taskID]
			if child == nil {
				return sessionstore.ErrAgentAccess
			}
			return b.root.store.ReconcileBudget(actorCtx, b.root.meta.ID, child.agentID, reservation, actual)
		})
	}, nil
}

func (s *Session) AdmitSubagent(ctx context.Context, taskID string, child *agent.Agent) error {
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
	delegations := []sessionstore.CapabilityDelegation{
		{ID: authority.Files.ID, Issuer: s.authority.Files, AgentID: agentID, Operations: []string{"read", "write", "edit", "workspace.write"}, Scopes: []string{s.meta.CWD}},
		{ID: authority.Shell.ID, Issuer: s.authority.Shell, AgentID: agentID, Operations: []string{"bash", "browser_exec", "computer_exec", "workspace_process"}},
	}
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		if _, exists := s.children[taskID]; exists {
			return fmt.Errorf("subagent %s already exists", taskID)
		}
		if _, err := s.store.AdmitChild(actorCtx, sessionstore.ChildAdmission{
			RootID: s.meta.ID, ParentAgentID: s.authority.AgentID, ChildAgentID: agentID,
			ExecutionID: executionID, Capabilities: delegations,
		}); err != nil {
			return err
		}
		child.Services = services
		child.Tools = tools.AllWithServices(services)
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
		return nil
	})
}

func (s *Session) FinishSubagent(ctx context.Context, taskID string, status agent.TaskStatus) error {
	durableStatus := map[agent.TaskStatus]string{
		agent.TaskDone: "succeeded", agent.TaskError: "failed", agent.TaskCancelled: "cancelled",
	}[status]
	if durableStatus == "" {
		return fmt.Errorf("invalid subagent status %q", status)
	}
	return s.routeControl(ctx, func(actorCtx context.Context) error {
		child := s.children[taskID]
		if child == nil {
			return sessionstore.ErrAgentAccess
		}
		if !child.running {
			return nil
		}
		child.running = false
		_, err := s.store.FinishChildTurn(actorCtx, s.meta.ID, s.authority.AgentID, child.executionID, durableStatus)
		if errors.Is(err, sessionstore.ErrAgentTerminal) && status == agent.TaskCancelled {
			return nil
		}
		return err
	})
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
		sequence, err = s.store.SendAgentMessage(actorCtx, s.meta.ID, senderAgentID, recipientAgentID, message)
		return err
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
