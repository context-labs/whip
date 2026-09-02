// Permission gating for the tools that touch the world: bash, write, edit.
// The TUI installs Gate; a gated call blocks until the user answers Allow
// once / Allow always / Reject. "Always" records a rule at command-prefix
// arity (so "git checkout main" allows future "git checkout …", not the whole
// string). No gate (tests, headless) means allow — the gate is a UX layer,
// not a sandbox.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
	Tool    string // bash | write | edit
	Command string // the bash command or the file path
	Rule    string // the rule "always" would install (arity-collapsed)
}

// Gate is a session-scoped permission hook. Nil means allow.
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

// arity maps a command prefix to how many tokens define "the command" —
// longest prefix wins, flags never count. Compact version of opencode's
// generated table (permission/arity.ts); the common cases carry the value.
var arity = map[string]int{
	// one-token commands: the binary alone is the rule
	"ls": 1, "cat": 1, "pwd": 1, "grep": 1, "find": 1, "echo": 1,
	"rm": 1, "mv": 1, "cp": 1, "mkdir": 1, "touch": 1, "which": 1,
	// two-token: binary + subcommand
	"git": 2, "npm": 2, "pnpm": 2, "yarn": 2, "go": 2, "cargo": 2,
	"docker": 2, "kubectl": 2, "brew": 2, "apt": 2, "pip": 2,
	// three-token where the shorter prefix under-specifies
	"npm run": 3, "pnpm run": 3, "go tool": 3, "docker compose": 3, "git submodule": 3,
}

// CommandRule collapses a shell command to its arity rule: the prefix "always"
// should install. Only the first command of a pipeline/chain is considered —
// "git checkout main && rm -rf /" is not a "git checkout" rule.
func CommandRule(command string) string {
	cmd := strings.TrimSpace(command)
	// stop at the first shell operator: the rule covers one command, not a chain
	for i, r := range cmd {
		if r == '&' || r == '|' || r == ';' || r == '>' || r == '<' {
			cmd = strings.TrimSpace(cmd[:i])
			break
		}
	}
	tokens := strings.Fields(cmd)
	for i := 0; i < len(tokens); i++ {
		if strings.Contains(tokens[i], "=") && !strings.HasPrefix(tokens[i], "-") && i == 0 {
			// leading VAR=value assignments aren't part of the command
			tokens = tokens[1:]
			i = -1
		}
	}
	if len(tokens) == 0 {
		return ""
	}
	// longest matching prefix wins
	for n := len(tokens); n > 0; n-- {
		prefix := strings.Join(tokens[:n], " ")
		if a, ok := arity[prefix]; ok {
			return strings.Join(tokens[:min(a, len(tokens))], " ")
		}
	}
	return tokens[0] // unknown command: the binary is the rule
}

// CheckGate runs the installed gate; "" means proceed.
func (s *Services) CheckGate(ctx context.Context, tool, command string) string {
	s.mu.RLock()
	gate := s.gate
	s.mu.RUnlock()
	if gate == nil {
		// Direct embedded callers historically run under the local user's
		// authority. Production daemon services select external prompts, while
		// headless clients install an explicit rejecting gate.
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
	if local, _ := ctx.Value(localHumanKey{}).(bool); local {
		return capability.Decision{Allow: true, PrincipalID: "local-human"}, nil
	}
	var args struct {
		Command string `json:"command"`
		Path    string `json:"path"`
	}
	s.mu.Lock()
	if s.externalPermissions {
		if decision, ok := s.permissionEarly[prompt.ID]; ok {
			delete(s.permissionEarly, prompt.ID)
			s.mu.Unlock()
			return decision, nil
		}
		waiter := make(chan capability.Decision, 1)
		s.permissionWaiters[prompt.ID] = waiter
		s.mu.Unlock()
		select {
		case decision := <-waiter:
			return decision, nil
		case <-ctx.Done():
			s.mu.Lock()
			if s.permissionWaiters[prompt.ID] == waiter {
				delete(s.permissionWaiters, prompt.ID)
			}
			s.mu.Unlock()
			return capability.Decision{}, ctx.Err()
		}
	}
	s.mu.Unlock()
	if err := json.Unmarshal(prompt.Arguments, &args); err != nil {
		return capability.Decision{}, err
	}
	command := args.Path
	if prompt.Operation == "bash" || prompt.Operation == "workspace_process" {
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
