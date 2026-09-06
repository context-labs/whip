// Permission gating for the tools that touch the world: bash, write, edit.
// The TUI installs Gate; a gated call blocks until the user answers Allow
// once / Allow always / Reject. "Always" records a rule at command-prefix
// arity (so "git checkout main" allows future "git checkout …", not the whole
// string). MCP consent also binds the exact server and tool definition.
package tools

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/context-labs/whip/internal/capability"
)

// GateDecision is the user's answer to a permission prompt.
type GateDecision int

const (
	GateAllowOnce GateDecision = iota
	GateAllowAlways
	GateReject
)

// GateRequest describes one gated tool call for the prompt.
type GateRequest struct {
	Tool    string // bash | write | edit | mcp.call
	Command string // command, file path, or MCP identity and arguments
	Rule    string // the rule "always" would install (arity-collapsed)
}

// Gate is a session-scoped permission hook. MCP additionally requires explicit
// consent, native trust, a saved rule, or automatic permission mode.
type Gate func(context.Context, GateRequest) (GateDecision, string)

type (
	servicesKey   struct{}
	localHumanKey struct{}
)

// WithServices exposes the calling agent's services to custom tools.
func WithServices(ctx context.Context, services *Services) context.Context {
	return context.WithValue(ctx, servicesKey{}, services)
}

// WithLocalHuman marks an operation already initiated directly by the user.
func WithLocalHuman(ctx context.Context) context.Context {
	return context.WithValue(ctx, localHumanKey{}, true)
}

func servicesFromContext(ctx context.Context) *Services {
	services, _ := ctx.Value(servicesKey{}).(*Services)
	return services
}

// CheckGate follows the calling agent's permission policy. Custom tools use
// this when they need the same consent behavior as built-ins.
func CheckGate(ctx context.Context, tool, command string) string {
	services := servicesFromContext(ctx)
	if services == nil {
		return ""
	}
	return services.CheckGate(ctx, tool, command)
}

// CommandRule collapses a shell command to its arity rule; the table lives
// with the durable rule store in capability.
func CommandRule(command string) string { return capability.CommandRule(command) }

// CheckGate runs the installed gate; "" means proceed.
func (s *Services) CheckGate(ctx context.Context, tool, command string) string {
	s.mu.RLock()
	gate, headless := s.gate, s.headlessPermissions
	s.mu.RUnlock()
	if headless {
		return "Permission denied: headless execution cannot request consent"
	}
	if gate == nil {
		// Direct embedded callers historically run under the local user's
		// authority. Production daemon services select external prompts.
		return ""
	}
	decision, redirect := gate(ctx, GateRequest{Tool: tool, Command: command, Rule: CommandRule(command)})
	if decision == GateReject {
		if redirect == "" {
			redirect = "the user rejected this action"
		}
		return "Permission denied: " + redirect
	}
	return ""
}

// Decide adapts the session gate to durable dispatcher permission decisions.
func (s *Services) Decide(ctx context.Context, prompt capability.PermissionPrompt) (capability.Decision, error) {
	if prompt.Operation == "mcp.call" {
		return s.decideMCP(ctx, prompt)
	}
	s.mu.RLock()
	headless, external := s.headlessPermissions, s.externalPermissions
	s.mu.RUnlock()
	if headless {
		return capability.Decision{PrincipalID: "headless", Reason: "headless execution cannot request consent"}, nil
	}
	if local, _ := ctx.Value(localHumanKey{}).(bool); local {
		return capability.Decision{Allow: true, PrincipalID: "local-human"}, nil
	}
	var args struct {
		Command string `json:"command"`
		Path    string `json:"path"`
	}
	if external {
		return s.waitPermission(ctx, prompt.ID)
	}
	if err := json.Unmarshal(prompt.Arguments, &args); err != nil {
		return capability.Decision{}, err
	}
	command := args.Path
	if prompt.Operation == "bash" || prompt.Operation == "workspace_process" || prompt.Operation == "shell_start" {
		command = args.Command
	} else if prompt.CanonicalPath != "" {
		command = prompt.CanonicalPath
	}
	if command == "" {
		return capability.Decision{}, errors.New("permission request target is empty")
	}
	s.mu.RLock()
	gate := s.gate
	s.mu.RUnlock()
	if gate == nil {
		// See CheckGate: nil is the direct-embedding policy, not the daemon
		// default.
		return capability.Decision{Allow: true, PrincipalID: "local-client"}, nil
	}
	decision, reason := gate(ctx, GateRequest{Tool: prompt.Operation, Command: command, Rule: CommandRule(command)})
	return capability.Decision{Allow: decision != GateReject, PrincipalID: "local-human", Reason: reason}, nil
}

func (s *Services) waitPermission(ctx context.Context, permissionID string) (capability.Decision, error) {
	s.mu.Lock()
	consent, _ := ctx.Value(mcpConsentKey{}).(*mcpConsent)
	staleMCP := consent != nil && consent.revision != s.permissionRevision
	if !s.externalPermissions || s.headlessPermissions || staleMCP {
		s.mu.Unlock()
		return capability.Decision{}, capability.ErrStaleAdmission
	}
	if decision, ok := s.permissionEarly[permissionID]; ok {
		delete(s.permissionEarly, permissionID)
		s.mu.Unlock()
		return decision, nil
	}
	waiter := make(chan capability.Decision, 1)
	s.permissionWaiters[permissionID] = waiter
	s.mu.Unlock()
	select {
	case decision := <-waiter:
		return decision, nil
	case <-ctx.Done():
		s.mu.Lock()
		if s.permissionWaiters[permissionID] == waiter {
			delete(s.permissionWaiters, permissionID)
		}
		s.mu.Unlock()
		return capability.Decision{}, ctx.Err()
	}
}

// Policy changes invalidate admitted consent and wake existing prompts. Every
// waiter has capacity one; removing it under the lock excludes ResolvePermission
// from sending a second decision to that channel.
func (s *Services) invalidatePermissionPolicyLocked() {
	s.permissionRevision++
	for id, waiter := range s.permissionWaiters {
		delete(s.permissionWaiters, id)
		waiter <- capability.Decision{PrincipalID: "permission-policy", Reason: "permission policy changed"}
	}
	clear(s.permissionEarly)
}
