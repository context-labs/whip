package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/tools"
)

// Hook notices ride in the turn's ephemeral system text so the model learns
// what a hook rewrote, denied, or skipped before its next request. They are
// bounded so a chatty hook cannot crowd the prompt; the event stream keeps the
// full record.
const (
	maxHookNotices      = 8
	maxHookNoticesBytes = 2 << 10
)

// hookCall is the identity every hook invocation carries.
func (node *AgentSession) hookInvocation(ctx context.Context, hook *agentdef.Hook, name string) (hookInvocation, error) {
	root := node.root
	mode, err := root.store.PermissionMode(ctx, root.ID())
	if err != nil {
		return hookInvocation{}, err
	}
	node.mu.Lock()
	turnID := node.turn.TurnID
	node.mu.Unlock()
	return hookInvocation{
		Definition: root.definition.ID, Revision: root.meta.DefinitionRevision, RootID: root.ID(), AgentID: node.id, TurnID: turnID,
		Hook: name, PermissionMode: mode, Timeout: hook.Timeout(),
	}, nil
}

// askHook invokes one declared hook and returns its decision. An unanswered
// hook (no executor, timeout, disconnect) is an error for a required hook and
// a skipped decision for an optional one; a failed handler follows the same
// rule. Deny, rewrite, and skip are emitted as stream.hook.decision.
func (node *AgentSession) askHook(ctx context.Context, hook *agentdef.Hook, invocation hookInvocation, callID string) (hookDecision, error) {
	subject := invocation.Operation
	if subject == "" {
		subject = invocation.Hook
	}
	var decision hookDecision
	var err error
	if node.root.executors == nil {
		err = fmt.Errorf("hook %s: no executor registry serves this daemon", invocation.Hook)
	} else {
		decision, err = node.root.executors.InvokeHook(ctx, invocation)
	}
	if err == nil && decision.Failed && hook.Optional {
		err = fmt.Errorf("hook %s handler failed: %s", invocation.Hook, decision.Reason)
	}
	if err != nil {
		if ctx.Err() != nil {
			return hookDecision{}, err
		}
		if !hook.Optional {
			node.emitHookDecision(callID, invocation, "", "deny", err.Error())
			return hookDecision{}, err
		}
		node.emitHookDecision(callID, invocation, "", "skipped", err.Error())
		node.addHookNotice(fmt.Sprintf("Hook %s was skipped for %s: %s", invocation.Hook, subject, err.Error()))
		return hookDecision{Skipped: true, Reason: err.Error()}, nil
	}
	if decision.Deny {
		node.emitHookDecision(callID, invocation, decision.InvocationID, "deny", decision.Reason)
	}
	return decision, nil
}

// beforeTool gates one host operation. It returns the arguments to run with,
// which are the originals unless the hook rewrote them.
func (node *AgentSession) beforeTool(ctx context.Context, module, operation string, arguments map[string]any) (map[string]any, error) {
	hook := node.effectiveDefinition().Hook(agentdef.HookBeforeTool)
	if hook == nil {
		return arguments, nil
	}
	name := module + "." + operation
	if hook.Operations != nil && !slices.Contains(hook.Operations, name) {
		return arguments, nil
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	body, err := json.Marshal(arguments)
	if err != nil {
		return nil, err
	}
	invocation, err := node.hookInvocation(ctx, hook, agentdef.HookBeforeTool)
	if err != nil {
		return nil, err
	}
	invocation.Operation, invocation.Arguments = name, body
	callID := tools.ToolCallID(ctx)
	decision, err := node.askHook(ctx, hook, invocation, callID)
	if err != nil {
		return nil, err
	}
	if decision.Deny {
		reason := decision.Reason
		if reason == "" {
			reason = "denied by the agent's before_tool hook"
		}
		return nil, fmt.Errorf("hook before_tool denied %s: %s", name, reason)
	}
	if decision.Arguments == nil {
		return arguments, nil
	}
	decoder := json.NewDecoder(strings.NewReader(string(decision.Arguments)))
	decoder.UseNumber()
	var rewritten map[string]any
	if err := decoder.Decode(&rewritten); err != nil || rewritten == nil {
		return nil, errors.New("hook before_tool returned arguments that are not a JSON object")
	}
	node.emitHookDecision(callID, invocation, decision.InvocationID, "rewrite", decision.Reason)
	notice := fmt.Sprintf("Hook before_tool rewrote %s arguments to %s", name, utf8PrefixRuntime(string(decision.Arguments), 512))
	if decision.Reason != "" {
		notice += " (reason: " + decision.Reason + ")"
	}
	node.addHookNotice(notice)
	return rewritten, nil
}

// emitHookDecision publishes one hook outcome to the session stream. Plain
// allows emit nothing.
func (node *AgentSession) emitHookDecision(callID string, invocation hookInvocation, invocationID, decision, reason string) {
	emit := node.emit
	if emit == nil {
		return
	}
	emit("stream.hook.decision", StreamEvent{
		ID: callID, Name: invocation.Hook, Args: invocation.Operation, Text: decision, Result: reason,
		TurnID: invocation.TurnID, InvocationID: invocationID,
	})
}

// addHookNotice queues one line for the model's next request this turn.
func (node *AgentSession) addHookNotice(text string) {
	node.mu.Lock()
	defer node.mu.Unlock()
	node.turn.HookNotices = append(node.turn.HookNotices, text)
}

// hookNotices renders the queued notices, bounded in count and bytes.
func (node *AgentSession) hookNotices() string {
	node.mu.Lock()
	notices := slices.Clone(node.turn.HookNotices)
	node.mu.Unlock()
	if len(notices) == 0 {
		return ""
	}
	var b strings.Builder
	shown := 0
	for _, notice := range notices {
		if shown == maxHookNotices || b.Len()+len(notice)+1 > maxHookNoticesBytes {
			break
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(notice)
		shown++
	}
	if shown < len(notices) {
		fmt.Fprintf(&b, "\n(%d more hook notices omitted)", len(notices)-shown)
	}
	return b.String()
}
