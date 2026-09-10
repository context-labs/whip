package protocol

import "encoding/json"

// ExecutorBindParams claims the executor lease for one definition revision.
// Tools must cover every tool the definition declares.
type ExecutorBindParams struct {
	Definition string   `json:"definition"`
	Revision   string   `json:"revision"`
	Tools      []string `json:"tools"`
}

// ExecutorBindResult returns the lease generation the executor must quote on
// every result. A later bind for the same revision replaces the holder.
type ExecutorBindResult struct {
	Generation int64    `json:"generation,string"`
	Tools      []string `json:"tools"`
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
