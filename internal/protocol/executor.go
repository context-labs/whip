package protocol

import "encoding/json"

// ExecutorBindParams claims the executor lease for one definition revision.
// Tools must cover every tool the definition declares.
type ExecutorBindParams struct {
	Definition string   `json:"definition"`
	Revision   string   `json:"revision"`
	Tools      []string `json:"tools"`
	// Hooks names the hooks this executor serves; it must cover every hook the
	// definition declares.
	Hooks []string `json:"hooks,omitempty"`
}

// ExecutorBindResult returns the lease generation the executor must quote on
// every result. A later bind for the same revision replaces the holder.
type ExecutorBindResult struct {
	Generation int64    `json:"generation,string"`
	Tools      []string `json:"tools"`
	Hooks      []string `json:"hooks,omitempty"`
}

// ExecutorPendingParams lists the invocations still awaiting this lease, for an
// executor that reconnected.
type ExecutorPendingParams struct {
	Definition string `json:"definition"`
	Revision   string `json:"revision"`
	Generation int64  `json:"generation,string"`
}

type ExecutorPendingResult struct {
	Invocations []ToolInvokeParams `json:"invocations"`
	Hooks       []HookInvokeParams `json:"hooks,omitempty"`
}

// SpawnRequest is an agents.spawn call as the model wrote it, the shape a
// before_spawn hook sees and may rewrite.
type SpawnRequest struct {
	Prompt       string           `json:"prompt"`
	Name         string           `json:"name"`
	Definition   string           `json:"definition"`
	Capabilities []string         `json:"capabilities"`
	Tools        []string         `json:"tools"`
	Budgets      map[string]int64 `json:"budgets"`
	Report       string           `json:"report"`
	Model        string           `json:"model"`
	Provider     string           `json:"provider"`
	Effort       string           `json:"effort"`
	// MCPTools is the raw mcp_tools narrowing, passed through unchanged.
	MCPTools json.RawMessage `json:"mcp_tools,omitempty"`
}

// ResolvedChild is what a spawn request resolves to before narrowing is
// enforced: the named child's defaults applied under the request.
type ResolvedChild struct {
	Definition   string           `json:"definition"`
	Modules      []string         `json:"modules"`
	Capabilities []string         `json:"capabilities"`
	Tools        []string         `json:"tools"`
	Budgets      map[string]int64 `json:"budgets"`
	Report       string           `json:"report"`
}

// SpawnPreview is the before_spawn payload: the request and its resolution.
type SpawnPreview struct {
	Request  SpawnRequest  `json:"request"`
	Resolved ResolvedChild `json:"resolved"`
}

// HookInvokeParams is the hook.invoke notification. Hook names the decision
// point; Operation and Arguments are set for before_tool, Spawn for
// before_spawn, Input for turn_start.
type HookInvokeParams struct {
	InvocationID   string          `json:"invocation_id"`
	Definition     string          `json:"definition"`
	Revision       string          `json:"revision"`
	Generation     int64           `json:"generation,string"`
	RootID         string          `json:"root_id"`
	AgentID        string          `json:"agent_id"`
	TurnID         string          `json:"turn_id"`
	Hook           string          `json:"hook"`
	Operation      string          `json:"operation,omitempty"`
	Arguments      json.RawMessage `json:"arguments,omitempty"`
	Spawn          *SpawnPreview   `json:"spawn,omitempty"`
	Input          string          `json:"input,omitempty"`
	PermissionMode string          `json:"permission_mode"`
	DeadlineMillis int64           `json:"deadline_millis,string"`
}

// HookResultParams settles one hook invocation. Every field but the identity
// is optional: an empty reply allows the operation unchanged. Error marks a
// handler failure, which denies a required hook.
type HookResultParams struct {
	InvocationID string          `json:"invocation_id"`
	Generation   int64           `json:"generation,string"`
	Decision     string          `json:"decision,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	Arguments    json.RawMessage `json:"arguments,omitempty"`
	Spawn        *SpawnRequest   `json:"spawn,omitempty"`
	Context      string          `json:"context,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// ToolInvokeParams is the tool.invoke notification: one admitted, validated
// call. The invocation id is the ledger operation id.
type ToolInvokeParams struct {
	InvocationID string          `json:"invocation_id"`
	Definition   string          `json:"definition"`
	Revision     string          `json:"revision"`
	Generation   int64           `json:"generation,string"`
	RootID       string          `json:"root_id"`
	AgentID      string          `json:"agent_id"`
	TurnID       string          `json:"turn_id"`
	Tool         string          `json:"tool"`
	Input        json.RawMessage `json:"input"`
	// DeadlineMillis is the Unix time in milliseconds after which the daemon
	// settles the invocation as timed out.
	DeadlineMillis int64 `json:"deadline_millis,string"`
}

// ToolCancelParams is the tool.cancel notification. The daemon has already
// settled the invocation; the handler should stop.
type ToolCancelParams struct {
	InvocationID string `json:"invocation_id"`
	Generation   int64  `json:"generation,string"`
	Reason       string `json:"reason"`
}

// ToolResultParams settles one invocation. Output is the handler's JSON value;
// a non-empty Error fails the call instead.
type ToolResultParams struct {
	InvocationID string          `json:"invocation_id"`
	Generation   int64           `json:"generation,string"`
	Output       json.RawMessage `json:"output,omitempty"`
	Error        string          `json:"error,omitempty"`
}

// ToolProgressParams reports intermediate output for a running invocation.
type ToolProgressParams struct {
	InvocationID string `json:"invocation_id"`
	Generation   int64  `json:"generation,string"`
	Text         string `json:"text"`
}
