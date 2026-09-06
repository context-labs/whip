package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/context-labs/whip/internal/capability"
	"github.com/context-labs/whip/internal/config"
	sessionstore "github.com/context-labs/whip/internal/session"
)

// DecidePermissionCommand gives a client decision the same durable
// idempotency boundary as every other user action.
func (s *Session) DecidePermissionCommand(ctx context.Context, command sessionstore.CommandAdmission, permissionID string, decision capability.Decision) (capability.Ticket, error) {
	command.Scope = sessionstore.CommandScopeRoot
	command.RootID = s.meta.ID
	command.AgentID = s.authority.AgentID
	command.Kind = "permission.decide"
	command.Payload.Data = slices.Clone(command.Payload.Data)
	return routeControlValue(s, ctx, func(actorCtx context.Context) (capability.Ticket, error) {
		var ticket capability.Ticket
		var rememberErr error
		finish := func(err error) (capability.Ticket, error) {
			if err == nil {
				// A committed decision may have unblocked a waiting node.
				s.reconcileAgentWork()
			}
			// Remembering a rule can fail after the decision already landed.
			return ticket, errors.Join(err, rememberErr)
		}
		admitted, admitErr := s.store.AdmitControlCommand(actorCtx, command)
		if admitErr != nil {
			return finish(admitErr)
		}
		if !admitted.New {
			if admitted.Command.Status == "queued" || admitted.Command.Status == "running" || admitted.Command.Status == "waiting" {
				return finish(errors.New("permission decision is still running"))
			}
			body, resolveErr := s.store.ResolveRuntimeValue(actorCtx, s.meta.ID, admitted.Command.Outcome)
			if resolveErr != nil {
				return finish(resolveErr)
			}
			if admitted.Command.Status != "succeeded" {
				return finish(errors.New(string(body)))
			}
			return finish(json.Unmarshal(body, &ticket))
		}
		admission, decisionErr := s.store.Pending(actorCtx, permissionID)
		if decisionErr == nil && admission.Request.RootID != s.meta.ID {
			decisionErr = capability.ErrDenied
		}
		var rules []string
		if decisionErr == nil {
			rules, decisionErr = rememberedRules(admission, decision)
		}
		if decisionErr == nil {
			ticket, decisionErr = s.resolvePermission(actorCtx, admission, permissionID, decision)
		}
		if decisionErr == nil && len(rules) > 0 {
			rememberErr = s.rememberPermissionRules(actorCtx, permissionID, admission.Request.Operation, rules, decision)
		}
		status := "succeeded"
		var outcome []byte
		var err error
		if decisionErr != nil {
			status = "failed"
			outcome = []byte(decisionErr.Error())
		} else {
			outcome, err = json.Marshal(ticket)
			if err != nil {
				return finish(err)
			}
		}
		_, finishErr := s.store.FinishCommand(context.WithoutCancel(actorCtx), command.ClientID, command.CommandID, status, sessionstore.RuntimePayload{
			Data: outcome, MediaType: "application/json", Source: "permission decision outcome",
		})
		return finish(errors.Join(decisionErr, finishErr))
	})
}

// rememberedRules validates decision.Remember and names the rules an approval
// installs; nil when nothing is to be remembered.
func rememberedRules(admission capability.Admission, decision capability.Decision) ([]string, error) {
	switch decision.Remember {
	case "":
		return nil, nil
	case "tree", "global":
	default:
		return nil, fmt.Errorf("unknown remember scope %q", decision.Remember)
	}
	if !decision.Allow {
		return nil, nil
	}
	_, rules, ok := capability.PermissionRule(admission.Request.Operation, admission.Request.Arguments, admission.CanonicalPath)
	if !ok {
		return nil, errors.New("this permission has no rule to remember")
	}
	return rules, nil
}

// resolvePermission settles one prompt: through the live agent's resolver in
// external permission mode, otherwise through the store.
func (s *Session) resolvePermission(ctx context.Context, admission capability.Admission, permissionID string, decision capability.Decision) (capability.Ticket, error) {
	if runner, ok := s.runner.(clientPermissionRunner); ok && runner.ExternalPermissionsEnabled() {
		resolver := s.permissionResolver(admission.Request.AgentID)
		if resolver == nil || !resolver.ExternalPermissionsEnabled() {
			return capability.Ticket{}, errors.New("permission owner is not live in external permission mode")
		}
		return capability.Ticket{OperationID: admission.Request.OperationID}, resolver.ResolvePermission(permissionID, decision)
	}
	return s.store.Decide(ctx, admission, permissionID, decision)
}

// rememberPermissionRules installs the rules behind an approval and settles
// the other prompts the rules now cover. Those are best effort: a prompt that
// fails to resolve simply stays pending for the human.
func (s *Session) rememberPermissionRules(ctx context.Context, permissionID, operation string, rules []string, decision capability.Decision) error {
	for _, rule := range rules {
		if _, err := s.store.AddPermissionRule(ctx, s.meta.ID, operation, rule, decision.PrincipalID); err != nil {
			return err
		}
	}
	if decision.Remember == "global" {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		added := false
		for _, rule := range rules {
			if entry := operation + ":" + rule; !slices.Contains(cfg.Permissions.Allow, entry) {
				cfg.Permissions.Allow = append(cfg.Permissions.Allow, entry)
				added = true
			}
		}
		if added {
			if err := cfg.Save(); err != nil {
				return err
			}
		}
		s.store.SetGlobalPermissionRules(cfg.Permissions.Allow)
	}
	pending, err := s.store.ListPendingPermissions(ctx, s.meta.ID)
	if err != nil {
		return err
	}
	covered := capability.Decision{Allow: true, PrincipalID: decision.PrincipalID, Reason: "covered by rule " + operation + " " + capability.RuleLabel(rules)}
	for _, prompt := range pending {
		// In external mode the dispatcher commits the primary decision
		// asynchronously, so the store may still list it as pending here.
		if prompt.ID == permissionID || prompt.Operation != operation {
			continue
		}
		admission, err := s.store.Pending(ctx, prompt.ID)
		if err != nil {
			continue
		}
		// Re-run the check Begin would make now, so a chain whose other
		// commands are still uncovered keeps waiting for the human.
		_, promptRules, ok := capability.PermissionRule(admission.Request.Operation, admission.Request.Arguments, admission.CanonicalPath)
		if !ok {
			continue
		}
		if source, err := s.store.PermissionRuleSource(ctx, s.meta.ID, operation, promptRules); err != nil || source == "" {
			continue
		}
		_, _ = s.resolvePermission(ctx, admission, prompt.ID, covered)
	}
	return nil
}

func (s *Session) permissionResolver(agentID string) clientPermissionRunner {
	if runtime, ok := s.runtime.(interface {
		PermissionResolver(string) clientPermissionRunner
	}); ok {
		return runtime.PermissionResolver(agentID)
	}
	resolver, _ := s.runner.(clientPermissionRunner)
	return resolver
}

func (s *Session) DecidePermission(ctx context.Context, permissionID string, decision capability.Decision) (capability.Ticket, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (capability.Ticket, error) {
		admission, err := s.store.Pending(actorCtx, permissionID)
		if err != nil {
			return capability.Ticket{}, err
		}
		if admission.Request.RootID != s.meta.ID {
			return capability.Ticket{}, capability.ErrDenied
		}
		return s.store.Decide(actorCtx, admission, permissionID, decision)
	})
}

func (s *Session) InspectPermission(ctx context.Context, permissionID string) (capability.Admission, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (capability.Admission, error) {
		admission, err := s.store.Pending(actorCtx, permissionID)
		if err == nil && admission.Request.RootID != s.meta.ID {
			return capability.Admission{}, capability.ErrDenied
		}
		return admission, err
	})
}

func (s *Session) InspectCapability(ctx context.Context, callerAgentID, capabilityID string) (sessionstore.CapabilityRecord, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.CapabilityRecord, error) {
		return s.store.InspectCapability(actorCtx, s.meta.ID, callerAgentID, capabilityID)
	})
}

func (s *Session) DelegateCapability(ctx context.Context, callerAgentID string, delegation sessionstore.CapabilityDelegation) (sessionstore.CapabilityRecord, error) {
	delegation.Operations = slices.Clone(delegation.Operations)
	delegation.Scopes = slices.Clone(delegation.Scopes)
	delegation.MCP = slices.Clone(delegation.MCP)
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.CapabilityRecord, error) {
		return s.store.DelegateCapability(actorCtx, s.meta.ID, callerAgentID, delegation)
	})
}

func (s *Session) RevokeCapability(ctx context.Context, callerAgentID, capabilityID string) (sessionstore.CapabilityRecord, error) {
	return routeControlValue(s, ctx, func(actorCtx context.Context) (sessionstore.CapabilityRecord, error) {
		return s.revokeCapability(actorCtx, callerAgentID, capabilityID)
	})
}

func (s *Session) revokeCapability(ctx context.Context, callerAgentID, capabilityID string) (sessionstore.CapabilityRecord, error) {
	pending, err := s.store.ListPendingPermissions(ctx, s.meta.ID)
	if err != nil {
		return sessionstore.CapabilityRecord{}, err
	}
	record, err := s.store.RevokeCapabilityFor(ctx, s.meta.ID, callerAgentID, capabilityID)
	if err != nil {
		return record, err
	}
	for _, prompt := range pending {
		if _, err := s.store.Pending(ctx, prompt.ID); !errors.Is(err, capability.ErrDenied) {
			continue
		}
		if resolver := s.permissionResolver(prompt.AgentID); resolver != nil && resolver.ExternalPermissionsEnabled() {
			_ = resolver.ResolvePermission(prompt.ID, capability.Decision{PrincipalID: "capability-revoked", Reason: "capability revoked"})
		}
	}
	return record, nil
}
