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

// DecidePermissionCommand gives a signed human decision the same durable
// idempotency boundary as every other user action.
func (s *Session) DecidePermissionCommand(ctx context.Context, command sessionstore.CommandAdmission, permissionID string, decision capability.Decision) (ticket capability.Ticket, err error) {
	command.Scope = sessionstore.CommandScopeRoot
	command.RootID = s.meta.ID
	command.AgentID = s.authority.AgentID
	command.Kind = "permission.decide"
	var rememberErr error
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
	if err == nil {
		// A decision may have unblocked a waiting node; re-derive readiness.
		s.reconcileAgentWork()
	}
	// The decision itself landed even when remembering it did not; the
	// command outcome stays authoritative and the caller hears about the rest.
	return ticket, errors.Join(err, rememberErr)
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
