package protocol

import (
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

type ExecutorBindParams struct {
	Definition DefinitionRef `json:"definition"`
	Tools      []ID          `json:"tools"`
	Hooks      []ID          `json:"hooks"`
}

type ExecutorLease struct {
	Epoch      ID            `json:"epoch"`
	Definition DefinitionRef `json:"definition"`
	Generation Counter       `json:"generation"`
	Tools      []ID          `json:"tools"`
	Hooks      []ID          `json:"hooks"`
}

type ExecutorPendingParams struct {
	Epoch      ID            `json:"epoch"`
	Definition DefinitionRef `json:"definition"`
	Generation Counter       `json:"generation"`
	After      *ID           `json:"after"`
}

// JSON payloads are bytes so the same exact numbers cross Go and JavaScript.
type ExecutorInvocation struct {
	Origin          string        `json:"origin" enum:"cell,host_operation,turn"`
	InvocationID    ID            `json:"invocation_id"`
	Lease           ExecutorLease `json:"lease"`
	Kind            string        `json:"kind" enum:"tool,hook"`
	Name            ID            `json:"name"`
	SessionID       ID            `json:"session_id"`
	TurnID          ID            `json:"turn_id"`
	CellID          *ID           `json:"cell_id"`
	OperationID     *ID           `json:"operation_id"`
	Operation       string        `json:"operation"`
	ArgumentsBase64 *string       `json:"arguments_base64"`
	SpawnBase64     *string       `json:"spawn_base64"`
	InputPreview    string        `json:"input_preview"`
	PermissionMode  string        `json:"permission_mode"`
	DeadlineMillis  Counter       `json:"deadline_millis"`
}

type ExecutorPendingResult struct {
	Items     []ExecutorInvocation `json:"items"`
	NextAfter *ID                  `json:"next_after"`
}

type ExecutorToolResultParams struct {
	Epoch        ID      `json:"epoch"`
	Generation   Counter `json:"generation"`
	InvocationID ID      `json:"invocation_id"`
	OutputBase64 *string `json:"output_base64"`
	Failure      string  `json:"failure"`
}

type ExecutorHookResultParams struct {
	Epoch           ID      `json:"epoch"`
	Generation      Counter `json:"generation"`
	InvocationID    ID      `json:"invocation_id"`
	Decision        string  `json:"decision" enum:",allow,deny"`
	Reason          string  `json:"reason"`
	ArgumentsBase64 *string `json:"arguments_base64"`
	SpawnBase64     *string `json:"spawn_base64"`
	Context         string  `json:"context"`
	Failure         string  `json:"failure"`
}

type ExecutorProgressParams struct {
	Epoch        ID      `json:"epoch"`
	Generation   Counter `json:"generation"`
	InvocationID ID      `json:"invocation_id"`
	Text         string  `json:"text"`
}

type ExecutorAccepted struct {
	Accepted bool `json:"accepted"`
}

type HookDecision struct {
	InvocationID *ID    `json:"invocation_id"`
	Hook         string `json:"hook" enum:"before_tool,before_spawn,turn_start"`
	Operation    string `json:"operation"`
	Decision     string `json:"decision" enum:"deny,rewrite,skipped"`
	Reason       string `json:"reason"`
}

type ExecutorProgress struct {
	InvocationID ID     `json:"invocation_id"`
	OperationID  ID     `json:"operation_id"`
	Text         string `json:"text"`
}

type ExecutorActivity struct {
	Epoch     ID                `json:"epoch"`
	TurnID    ID                `json:"turn_id"`
	Revision  Counter           `json:"revision"`
	Decisions []HookDecision    `json:"decisions"`
	Truncated bool              `json:"truncated"`
	Progress  *ExecutorProgress `json:"progress"`
}

type ExecutorActivityResult struct {
	Activity *ExecutorActivity `json:"activity"`
}

// Notifications exist only on an explicitly bound persistent connection.
// Cancellation settles local authority before its best-effort notification.
type ExecutorEvent struct {
	JSONRPC      string              `json:"jsonrpc" enum:"2.0"`
	Method       string              `json:"method" enum:"executor.invoke,executor.cancel"`
	Epoch        ID                  `json:"epoch"`
	Generation   Counter             `json:"generation"`
	InvocationID ID                  `json:"invocation_id"`
	Invocation   *ExecutorInvocation `json:"invocation"`
}

func executorSchema(schema *jsonschema.Schema, t reflect.Type) {
	if t == reflect.TypeFor[ExecutorEvent]() {
		schema.OneOf = []*jsonschema.Schema{
			{Properties: map[string]*jsonschema.Schema{"method": {Enum: []any{"executor.invoke"}}, "invocation": {Not: &jsonschema.Schema{Type: "null"}}}},
			{Properties: map[string]*jsonschema.Schema{"method": {Enum: []any{"executor.cancel"}}, "invocation": {Type: "null"}}},
		}
	}
	if t == reflect.TypeFor[HookDecision]() {
		schema.Properties["operation"].MaxLength = new(128)
		schema.Properties["reason"].MaxLength = new(512)
	}
	if t == reflect.TypeFor[ExecutorProgress]() {
		schema.Properties["text"].MaxLength = new(2048)
	}
	if t == reflect.TypeFor[ExecutorActivity]() {
		field := schema.Properties["decisions"]
		field.Type, field.Types, field.MaxItems = "array", nil, new(8)
	}
	if t == reflect.TypeFor[ExecutorBindParams]() || t == reflect.TypeFor[ExecutorLease]() {
		for _, name := range []string{"tools", "hooks"} {
			field := schema.Properties[name]
			field.Type, field.Types, field.UniqueItems = "array", nil, true
			field.MaxItems = new(128)
			if name == "hooks" {
				field.MaxItems = new(3)
			}
		}
	}
	if t == reflect.TypeFor[ExecutorInvocation]() {
		for name, limit := range map[string]int{"operation": 128, "arguments_base64": 1398104, "spawn_base64": 1398104, "input_preview": 2048, "permission_mode": 64} {
			schema.Properties[name].MaxLength = new(limit)
		}
	}
	if t == reflect.TypeFor[ExecutorToolResultParams]() {
		schema.Properties["output_base64"].MaxLength = new(699052)
	}
	if t == reflect.TypeFor[ExecutorHookResultParams]() {
		for name, limit := range map[string]int{"arguments_base64": 699052, "spawn_base64": 699052, "context": 4096, "reason": 2048} {
			schema.Properties[name].MaxLength = new(limit)
		}
	}
	if t == reflect.TypeFor[ExecutorToolResultParams]() || t == reflect.TypeFor[ExecutorHookResultParams]() {
		schema.Properties["failure"].MaxLength = new(2048)
	}
	if t == reflect.TypeFor[ExecutorProgressParams]() {
		schema.Properties["text"].MaxLength = new(2048)
	}
	if t == reflect.TypeFor[ExecutorPendingResult]() {
		field := schema.Properties["items"]
		field.Type, field.Types, field.MaxItems = "array", nil, new(4)
	}
	if t == reflect.TypeFor[ExecutorAccepted]() {
		schema.Properties["accepted"].Enum = []any{true}
	}
}
