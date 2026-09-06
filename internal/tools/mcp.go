package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/capability"
)

// MCPProvider resolves canonical tool identities and checks a call after its
// server queue, immediately before transmission.
type MCPProvider interface {
	ResolveTool(server, tool string) (capability.MCPCall, error)
	ValidateArguments(capability.MCPCall) error
	CallContext(capability.MCPCall) (context.Context, error)
	CallChecked(context.Context, capability.MCPCall, func(context.Context) error) (string, error)
}

type mcpAuthorizer interface {
	AuthorizeMCP(context.Context, string, string, capability.Reference, capability.MCPSelector) error
}

// SetMCPProvider installs a resolver for the root's current manager. Descendants
// retain this resolver so replacing the manager also updates existing children.
func (s *Services) SetMCPProvider(provider func() MCPProvider) {
	s.mu.Lock()
	s.mcpProvider = provider
	s.mu.Unlock()
}

func (s *Services) currentMCPProvider() (MCPProvider, error) {
	s.mu.RLock()
	resolve := s.mcpProvider
	s.mu.RUnlock()
	if resolve != nil {
		if provider := resolve(); provider != nil {
			return provider, nil
		}
	}
	return nil, errors.New("MCP is not configured")
}

// SetMCPAutomatic explicitly authorizes MCP consent without prompting. It does
// not extend tool capabilities or override an installed rejecting Gate.
func (s *Services) SetMCPAutomatic(enabled bool) {
	s.mu.Lock()
	if s.mcpAutomatic != enabled {
		s.invalidatePermissionPolicyLocked()
	}
	s.mcpAutomatic = enabled
	s.mu.Unlock()
}

// SetHeadlessPermissions prevents tools from waiting for interactive consent.
// Trusted MCP definitions and saved rules still use their existing authority.
func (s *Services) SetHeadlessPermissions(enabled bool) {
	s.mu.Lock()
	if s.headlessPermissions != enabled {
		s.invalidatePermissionPolicyLocked()
	}
	s.headlessPermissions = enabled
	s.mu.Unlock()
}

type mcpConsentKey struct{}

// One invocation uses one gate decision. A policy change invalidates it rather
// than repeating an interactive prompt while holding the remote server queue.
type mcpConsent struct {
	revision     uint64
	checked      bool
	hasGate      bool
	decision     capability.Decision
	permissionID string
}

// InvokeMCP resolves trusted metadata internally; callers supply only the exact
// configured server/tool names and the remote tool's arguments.
func (s *Services) InvokeMCP(ctx context.Context, server, tool string, arguments json.RawMessage) (string, error) {
	arguments = bytes.TrimSpace(arguments)
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if !json.Valid(arguments) || arguments[0] != '{' {
		return "", errors.New("MCP arguments must be a JSON object")
	}
	provider, err := s.currentMCPProvider()
	if err != nil {
		return "", err
	}
	call, err := provider.ResolveTool(server, tool)
	if err != nil {
		return "", err
	}
	if call.Server != server || call.Tool != tool || call.Definition == "" || call.Generation == "" {
		return "", errors.New("MCP provider returned an invalid tool identity")
	}
	call.Arguments = append(json.RawMessage(nil), arguments...)
	if err := provider.ValidateArguments(call); err != nil {
		return "", err
	}
	lifetime, err := provider.CallContext(call)
	if err != nil {
		return "", err
	}
	if lifetime == nil {
		return "", errors.New("MCP provider returned no generation lifetime")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(lifetime, cancel)
	defer stop()
	if err := lifetime.Err(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	envelope, err := json.Marshal(call)
	if err != nil {
		return "", err
	}
	s.mu.RLock()
	consent := &mcpConsent{revision: s.permissionRevision}
	s.mu.RUnlock()
	defer func() {
		s.mu.Lock()
		delete(s.permissionEarly, consent.permissionID)
		s.mu.Unlock()
	}()
	ctx = context.WithValue(ctx, mcpConsentKey{}, consent)
	return s.run(ctx, "mcp.call", envelope)
}

func (s *Services) mcpRegistration(ledger capability.Ledger) capability.Registration {
	return capability.Registration{
		Operation: "mcp.call",
		Mutation:  capability.MutationNone,
		PermissionRequired: func(arguments json.RawMessage) bool {
			var call capability.MCPCall
			return json.Unmarshal(arguments, &call) != nil || !call.Trusted
		},
		Handler: func(ctx context.Context, dispatched capability.Call) (string, error) {
			authorizer, ok := ledger.(mcpAuthorizer)
			if !ok {
				return "", fmt.Errorf("MCP ledger cannot revalidate authority: %w", capability.ErrDenied)
			}
			var call capability.MCPCall
			if err := json.Unmarshal(dispatched.Arguments, &call); err != nil {
				return "", err
			}
			consent, err := s.mcpGateDecision(ctx, dispatched.Arguments)
			if err != nil {
				return "", err
			}
			if !consent.decision.Allow {
				return "", &capability.PermissionDeniedError{Reason: consent.decision.Reason}
			}
			provider, err := s.currentMCPProvider()
			if err != nil {
				return "", err
			}
			return provider.CallChecked(ctx, call, func(ctx context.Context) error {
				current, err := s.currentMCPProvider()
				if err != nil {
					return err
				}
				descriptor, err := current.ResolveTool(call.Server, call.Tool)
				if err != nil {
					return err
				}
				if descriptor.MCPSelector != call.MCPSelector || descriptor.Generation != call.Generation || descriptor.Trusted != call.Trusted || descriptor.Source != call.Source {
					return capability.ErrStaleAdmission
				}
				request := dispatched.Request
				ref := capability.Reference{ID: request.CapabilityID, Generation: request.CapabilityGeneration}
				if err := authorizer.AuthorizeMCP(ctx, request.RootID, request.AgentID, ref, call.MCPSelector); err != nil {
					return err
				}
				return s.checkMCPRevision(ctx, consent)
			})
		},
	}
}

func (s *Services) checkMCPRevision(ctx context.Context, consent *mcpConsent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.RLock()
	current := s.permissionRevision
	s.mu.RUnlock()
	if current != consent.revision {
		return fmt.Errorf("permission policy changed: %w", capability.ErrStaleAdmission)
	}
	return nil
}

func (s *Services) mcpGateDecision(ctx context.Context, arguments json.RawMessage) (*mcpConsent, error) {
	consent, _ := ctx.Value(mcpConsentKey{}).(*mcpConsent)
	if consent == nil {
		return nil, capability.ErrDenied
	}
	if err := s.checkMCPRevision(ctx, consent); err != nil {
		return nil, err
	}
	if consent.checked {
		return consent, nil
	}
	s.mu.RLock()
	gate, headless := s.gate, s.headlessPermissions
	s.mu.RUnlock()
	consent.checked = true
	consent.hasGate = gate != nil
	consent.decision = capability.Decision{Allow: true, PrincipalID: "local-client"}
	if gate != nil {
		if headless {
			consent.decision = capability.Decision{PrincipalID: "headless", Reason: "headless execution cannot consult an embedded permission gate"}
		} else {
			command, rules, ok := capability.PermissionRule("mcp.call", arguments, "")
			if !ok {
				return nil, capability.ErrDenied
			}
			decision, reason := gate(ctx, GateRequest{Tool: "mcp.call", Command: command, Rule: capability.RuleLabel(rules)})
			consent.decision = capability.Decision{Allow: decision != GateReject, PrincipalID: "local-human", Reason: reason}
		}
	}
	return consent, s.checkMCPRevision(ctx, consent)
}

func (s *Services) decideMCP(ctx context.Context, prompt capability.PermissionPrompt) (capability.Decision, error) {
	consent, _ := ctx.Value(mcpConsentKey{}).(*mcpConsent)
	if consent != nil {
		consent.permissionID = prompt.ID
	}
	consent, err := s.mcpGateDecision(ctx, prompt.Arguments)
	if err != nil {
		return capability.Decision{}, err
	}
	if !consent.decision.Allow {
		return consent.decision, nil
	}
	s.mu.RLock()
	external, automatic, headless := s.externalPermissions, s.mcpAutomatic, s.headlessPermissions
	s.mu.RUnlock()
	if automatic {
		return capability.Decision{Allow: true, PrincipalID: "automatic-mode"}, nil
	}
	if headless {
		return capability.Decision{PrincipalID: "headless", Reason: "MCP call requires preauthorization in headless execution"}, nil
	}
	if external {
		return s.waitPermission(ctx, prompt.ID)
	}
	if consent.hasGate {
		return consent.decision, nil
	}
	return capability.Decision{PrincipalID: "local-client", Reason: "MCP call requires explicit consent or automatic permission mode"}, nil
}
