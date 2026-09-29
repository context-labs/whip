// Package hostmodule owns the static host-operation vocabulary shared by declarations and workers.
package hostmodule

import (
	"maps"
	"slices"
)

var operations = map[string][]string{
	"context":     {"inspect", "search", "read"},
	"files":       {"list", "search", "read", "write", "patch", "diagnostics"},
	"skills":      {"read"},
	"shell":       {"run", "read", "start", "poll", "tail", "wait", "kill", "list"},
	"browser":     {"list_tabs", "open", "attach", "run", "detach", "allow_preview_port"},
	"computer":    {"run"},
	"models":      {"call", "batch"},
	"agents":      {"spawn", "submit", "wait_after_cell", "inspect", "list", "stop", "delete", "pending_reports", "read_report"},
	"mail":        {"send", "list", "read", "complete", "defer"},
	"mcp":         {"list_servers", "list_tools", "search", "describe", "instructions", "call", "refresh", "reconnect"},
	"state":       {"get", "read", "write", "append", "list", "history", "subscribe", "subscriptions", "unsubscribe"},
	"artifacts":   {"put", "inspect", "read"},
	"goals":       {"complete"},
	"schedules":   {"create", "list", "cancel"},
	"permissions": {"request", "status"},
	"user":        {"ask"},
}

// Operations returns an independent copy of the host vocabulary.
func Operations() map[string][]string {
	result := maps.Clone(operations)
	for name, values := range result {
		result[name] = slices.Clone(values)
	}
	return result
}

// Names returns the canonical module order.
func Names() []string { return slices.Sorted(maps.Keys(operations)) }

// Has reports whether a module belongs to the host vocabulary.
func Has(name string) bool { _, ok := operations[name]; return ok }

// Contains reports whether an operation belongs to a host module.
func Contains(module, operation string) bool { return slices.Contains(operations[module], operation) }
