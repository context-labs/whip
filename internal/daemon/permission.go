package daemon

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/capability"
	sessionstore "github.com/context-labs/whip/internal/session"
)

// DecidePermissionCommand gives a signed human decision the same durable
// idempotency boundary as every other user action.
func (s *Session) DecidePermissionCommand(ctx context.Context, command sessionstore.CommandAdmission, permissionID string, decision capability.Decision) (ticket capability.Ticket, err error) {
	command.Scope = sessionstore.CommandScopeRoot
	command.RootID = s.meta.ID
	command.AgentID = s.authority.AgentID
	command.Kind = "permission.decide"
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		admitted, admitErr := s.store.AdmitControlCommand(actorCtx, command)
		if admitErr != nil {
			return admitErr
		}
		if !admitted.New {
			if admitted.Command.Status == "queued" || admitted.Command.Status == "running" || admitted.Command.Status == "waiting" {
				return errors.New("permission decision is still running")
			}
			body, resolveErr := s.store.ResolveRuntimeValue(actorCtx, s.meta.ID, admitted.Command.Outcome)
			if resolveErr != nil {
				return resolveErr
			}
			if admitted.Command.Status != "succeeded" {
				return errors.New(string(body))
			}
			return json.Unmarshal(body, &ticket)
		}
		admission, decisionErr := s.store.Pending(actorCtx, permissionID)
		if decisionErr == nil && admission.Request.RootID != s.meta.ID {
			decisionErr = capability.ErrDenied
		}
		if decisionErr == nil {
			if runner, ok := s.runner.(clientPermissionRunner); ok && runner.ExternalPermissionsEnabled() {
				resolver := s.permissionResolver(admission.Request.AgentID)
				if resolver == nil || !resolver.ExternalPermissionsEnabled() {
					decisionErr = errors.New("permission owner is not live in external permission mode")
				} else {
					decisionErr = resolver.ResolvePermission(permissionID, decision)
				}
				ticket.OperationID = admission.Request.OperationID
			} else {
				ticket, decisionErr = s.store.Decide(actorCtx, admission, permissionID, decision)
			}
		}
		status := "succeeded"
		var outcome []byte
		if decisionErr != nil {
			status = "failed"
			outcome = []byte(decisionErr.Error())
		} else {
			outcome, err = json.Marshal(ticket)
			if err != nil {
				return err
			}
		}
		_, finishErr := s.store.FinishCommand(actorCtx, command.ClientID, command.CommandID, status, sessionstore.RuntimePayload{
			Data: outcome, MediaType: "application/json", Source: "permission decision outcome",
		})
		return errors.Join(decisionErr, finishErr)
	})
	return ticket, err
}

func (s *Session) permissionResolver(agentID string) clientPermissionRunner {
	if agentID == s.authority.AgentID {
		resolver, _ := s.runner.(clientPermissionRunner)
		return resolver
	}
	for _, child := range s.children {
		if child.agentID == agentID && child.agent != nil && child.agent.Services != nil {
			return child.agent.Services
		}
	}
	return nil
}

func (s *Session) DecidePermission(ctx context.Context, permissionID string, decision capability.Decision) (ticket capability.Ticket, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		admission, err := s.store.Pending(actorCtx, permissionID)
		if err != nil {
			return err
		}
		if admission.Request.RootID != s.meta.ID {
			return capability.ErrDenied
		}
		ticket, err = s.store.Decide(actorCtx, admission, permissionID, decision)
		return err
	})
	return ticket, err
}

func (s *Session) InspectPermission(ctx context.Context, permissionID string) (admission capability.Admission, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		admission, err = s.store.Pending(actorCtx, permissionID)
		if err == nil && admission.Request.RootID != s.meta.ID {
			return capability.ErrDenied
		}
		return err
	})
	return admission, err
}

func (s *Session) InspectCapability(ctx context.Context, callerAgentID, capabilityID string) (record sessionstore.CapabilityRecord, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		record, err = s.store.InspectCapability(actorCtx, s.meta.ID, callerAgentID, capabilityID)
		return err
	})
	return record, err
}

func (s *Session) DelegateCapability(ctx context.Context, callerAgentID string, delegation sessionstore.CapabilityDelegation) (record sessionstore.CapabilityRecord, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		record, err = s.store.DelegateCapability(actorCtx, s.meta.ID, callerAgentID, delegation)
		return err
	})
	return record, err
}

func (s *Session) RevokeCapability(ctx context.Context, callerAgentID, capabilityID string) (record sessionstore.CapabilityRecord, err error) {
	err = s.routeControl(ctx, func(actorCtx context.Context) error {
		record, err = s.store.RevokeCapabilityFor(actorCtx, s.meta.ID, callerAgentID, capabilityID)
		return err
	})
	return record, err
}
