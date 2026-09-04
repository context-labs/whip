package rlm

import (
	"context"
	"fmt"
	"slices"
)

// Host is the only authority visible to a kernel worker. Implementations live
// in the daemon process and route calls through daemon APIs or the shared
// capability dispatcher.
type Host interface {
	Call(context.Context, string, string, map[string]any) (any, error)
}

type HostFunc func(context.Context, string, string, map[string]any) (any, error)

func (f HostFunc) Call(ctx context.Context, module, operation string, args map[string]any) (any, error) {
	return f(ctx, module, operation, args)
}

var moduleRegistry = map[string][]string{
	"context":     {"inspect", "search", "read"},
	"files":       {"list", "search", "read", "write", "patch"},
	"shell":       {"run", "read", "start", "poll", "tail", "wait", "kill", "list"},
	"browser":     {"run"},
	"computer":    {"run"},
	"models":      {"call", "batch"},
	"agents":      {"spawn", "submit", "wait", "inspect", "list", "stop", "delete"},
	"messages":    {"send", "list", "read", "complete", "ack", "defer"},
	"mcp":         {"list_servers", "list_tools", "call"},
	"state":       {"private_get", "private_set", "private_append", "private_cas", "private_list", "blackboard_get", "blackboard_set", "blackboard_append", "blackboard_cas", "blackboard_history", "subscribe", "subscriptions", "cancel_subscription"},
	"artifacts":   {"put", "inspect", "read"},
	"schedules":   {"create", "list", "cancel"},
	"permissions": {"request", "status"},
}

func Modules() map[string][]string {
	result := make(map[string][]string, len(moduleRegistry))
	for name, operations := range moduleRegistry {
		result[name] = append([]string(nil), operations...)
	}
	return result
}

func validateModuleOperation(module, operation string) error {
	operations, ok := moduleRegistry[module]
	if !ok {
		return fmt.Errorf("unknown RLM module %q", module)
	}
	if slices.Contains(operations, operation) {
		return nil
	}
	return fmt.Errorf("unknown RLM operation %s.%s", module, operation)
}
