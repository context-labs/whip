package model

import (
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func encodeTools(tools []Tool, remaining *int) ([]chatTool, error) {
	if len(tools) > session.MaxToolCalls {
		return nil, errors.New("provider tool count exceeds limit")
	}
	var result []chatTool
	names := map[string]bool{}
	for _, tool := range tools {
		if session.ValidateToolName(tool.Name) != nil || names[tool.Name] {
			return nil, errors.New("provider tools require valid unique names")
		}
		if len(tool.Description) > 16384 || !utf8.ValidString(tool.Description) {
			return nil, errors.New("provider tool description exceeds supported bounds")
		}
		var object map[string]json.RawMessage
		if len(tool.InputSchema) > session.MaxDocumentBytes || !utf8.Valid(tool.InputSchema) || json.Unmarshal(tool.InputSchema, &object) != nil || object == nil || string(object["type"]) != `"object"` {
			return nil, errors.New("provider tool parameters require a bounded object schema")
		}
		*remaining -= len(tool.InputSchema) + len(tool.Description)
		if *remaining < 0 {
			return nil, errors.New("provider tool declarations exceed context limit")
		}
		names[tool.Name] = true
		result = append(result, chatTool{Type: "function", Function: chatFunction{
			Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema,
		}})
	}
	return result, nil
}

func decodeChatParts(text *string, rawCalls json.RawMessage, finish string, allowed map[string]bool) ([]session.Part, error) {
	var calls []chatCall
	if len(rawCalls) > 0 && json.Unmarshal(rawCalls, &calls) != nil {
		return nil, &CallError{Message: "provider returned malformed tool calls"}
	}
	if len(calls) > session.MaxToolCalls || (finish == "tool_calls") != (len(calls) > 0) {
		return nil, &CallError{Message: "provider returned an invalid tool-call completion boundary"}
	}
	var parts []session.Part
	if text != nil && *text != "" {
		parts = append(parts, session.Part{Type: "text", Text: *text})
	}
	for _, call := range calls {
		if call.Type != "function" || !allowed[call.Function.Name] {
			return nil, &CallError{Message: "provider returned an undeclared tool call"}
		}
		parts = append(parts, session.Part{Type: "tool_call", Call: &session.ToolCall{
			ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments),
		}})
	}
	return parts, nil
}
