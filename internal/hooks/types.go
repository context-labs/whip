// Package hooks discovers and runs portable command-based lifecycle hooks.
package hooks

import (
	"context"
	"encoding/json"
	"time"
)

// Event is a lifecycle boundary exposed to hook commands.
type Event string

const (
	UserPromptSubmit   Event = "UserPromptSubmit"
	PreToolUse         Event = "PreToolUse"
	PostToolUse        Event = "PostToolUse"
	PostToolUseFailure Event = "PostToolUseFailure"
	Stop               Event = "Stop"
)

var eventOrder = []Event{
	UserPromptSubmit,
	PreToolUse,
	PostToolUse,
	PostToolUseFailure,
	Stop,
}

// Request is the event data supplied by the agent loop. Fields that do not
// apply to an event are omitted from the command's JSON payload.
type Request struct {
	Event                Event
	SessionID            string
	WorkingDir           string
	MatcherContext       string
	ToolName             string
	ToolInput            json.RawMessage
	ToolResponse         string
	ToolError            string
	Message              string
	LastAssistantMessage string
	ToolCallID           string
}

// Outcome is the composed result of every matching command for one event.
// Failures are telemetry: they do not block except for a PreToolUse command
// explicitly configured with on_failure=block.
type Outcome struct {
	Blocked           bool     `json:"blocked"`
	Reason            string   `json:"reason,omitempty"`
	AdditionalContext string   `json:"additional_context,omitempty"`
	Ran               int      `json:"ran"`
	Failures          []string `json:"failures,omitempty"`
}

// Entry describes one loaded command for status views.
type Entry struct {
	Event   Event
	Source  string
	Matcher string
	Command string
}

type action struct {
	event          Event
	source         string
	pluginRoot     string
	command        string
	timeout        time.Duration
	matcher        matcher
	onFailureBlock bool
}

// Runner is the narrow contract consumed by the agent loop.
type Runner interface {
	Run(ctx context.Context, req Request) Outcome
}
