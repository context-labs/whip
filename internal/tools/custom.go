package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/context-labs/whip/internal/capability"
)

// CustomTool is one definition-declared tool. The daemon validates arguments
// against InputSchema before an executor ever sees them.
type CustomTool struct {
	Name        string
	InputSchema json.RawMessage
	// Timeout bounds one invocation; the daemon supplies the definition's
	// effective value.
	Timeout time.Duration
}

// ToolInvocation is one admitted, validated call for an executor to run. The
// operation id doubles as the invocation id so the ledger row is the durable
// record of the call.
type ToolInvocation struct {
	Definition string
	Revision   string
	RootID     string
	AgentID    string
	TurnID     string
	// OperationID is the ledger operation id and the invocation id.
	OperationID string
	TraceID     string
	Tool        string
	Arguments   json.RawMessage
	Timeout     time.Duration
	// Progress receives intermediate output; nil ignores it.
	Progress func(string)
}

// ToolExecutor runs custom tools outside the daemon.
type ToolExecutor interface {
	Invoke(context.Context, ToolInvocation) (string, error)
}

type toolTurnKey struct{}

// SetCustomTools declares the definition's tools and the executor that serves
// them. It must precede BindDispatcher; clones inherit the declaration.
func (s *Services) SetCustomTools(definition, revision string, tools []CustomTool, executor ToolExecutor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.customDefinition, s.customRevision = definition, revision
	s.customTools = append([]CustomTool(nil), tools...)
	s.toolExecutor = executor
}

// InvokeTool dispatches tools.<name> through the ledger under the agent's tools
// grant. turnID attributes the invocation for the executor.
func (s *Services) InvokeTool(ctx context.Context, name, turnID string, arguments json.RawMessage) (string, error) {
	s.mu.RLock()
	declared := false
	for _, tool := range s.customTools {
		declared = declared || tool.Name == name
	}
	s.mu.RUnlock()
	if !declared {
		return "", fmt.Errorf("unknown custom tool %q", name)
	}
	if len(bytes.TrimSpace(arguments)) == 0 || bytes.Equal(bytes.TrimSpace(arguments), []byte("null")) {
		arguments = json.RawMessage(`{}`)
	}
	return s.run(context.WithValue(ctx, toolTurnKey{}, turnID), "tools."+name, arguments)
}

// customRegistrations builds one dispatcher registration per declared tool.
// Schema validation happens in the handler, after admission, so a rejected
// argument set is recorded like any other failed operation.
func (s *Services) customRegistrations() ([]capability.Registration, error) {
	s.mu.RLock()
	tools := append([]CustomTool(nil), s.customTools...)
	s.mu.RUnlock()
	registrations := make([]capability.Registration, 0, len(tools))
	for _, tool := range tools {
		var schema jsonschema.Schema
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			return nil, fmt.Errorf("custom tool %q input schema: %w", tool.Name, err)
		}
		resolved, err := schema.Resolve(nil) // remote references never cause network requests
		if err != nil {
			return nil, fmt.Errorf("custom tool %q input schema: %w", tool.Name, err)
		}
		registrations = append(registrations, capability.Registration{
			Operation: "tools." + tool.Name, Mutation: capability.MutationNone,
			Handler: func(ctx context.Context, call capability.Call) (string, error) {
				var arguments map[string]any
				if err := json.Unmarshal(call.Arguments, &arguments); err != nil || arguments == nil {
					return "", errors.New("invalid tool arguments: expected one JSON object")
				}
				if err := resolved.Validate(arguments); err != nil {
					return "", fmt.Errorf("invalid tool arguments: %w", err)
				}
				s.mu.RLock()
				executor, definition, revision := s.toolExecutor, s.customDefinition, s.customRevision
				s.mu.RUnlock()
				if executor == nil {
					return "", errors.New("custom tools have no executor in this daemon")
				}
				turnID, _ := ctx.Value(toolTurnKey{}).(string)
				return executor.Invoke(ctx, ToolInvocation{
					Definition: definition, Revision: revision, RootID: call.Request.RootID, AgentID: call.Request.AgentID, TurnID: turnID,
					OperationID: call.Request.OperationID, TraceID: call.Request.TraceID, Tool: tool.Name, Arguments: call.Arguments,
					Timeout: tool.Timeout, Progress: OnUpdate(ctx),
				})
			},
		})
	}
	return registrations, nil
}

func isCustomToolOperation(operation string) bool { return strings.HasPrefix(operation, "tools.") }
