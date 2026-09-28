package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func callPart(id string) Part {
	return Part{Type: "tool_call", Call: &ToolCall{ID: id, Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}
}

func TestMessagePartsHaveDisjointRoleOwnership(t *testing.T) {
	text := Part{Type: "text", Text: "working"}
	content := Part{Type: "content", ReferenceID: "reference"}
	call := callPart("call")
	result := Part{Type: "tool_result", Result: &ToolResult{CallID: "call", Output: "", IsError: false}}
	for _, test := range []struct {
		name  string
		role  Role
		parts []Part
		valid bool
	}{
		{"user input", User, []Part{text, content}, true},
		{"system instructions", System, []Part{text}, true},
		{"assistant call with text", Assistant, []Part{text, call}, true},
		{"empty result is valid", Tool, []Part{result}, true},
		{"user cannot call", User, []Part{call}, false},
		{"system cannot call", System, []Part{call}, false},
		{"assistant cannot report result", Assistant, []Part{result}, false},
		{"tool must report structured result", Tool, []Part{text}, false},
		{"one result per tool message", Tool, []Part{result, result}, false},
		{"unknown role", Role("other"), []Part{text}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateMessage(test.role, test.parts); (err == nil) != test.valid {
				t.Fatalf("ValidateMessage() = %v, valid=%v", err, test.valid)
			}
		})
	}
	if err := ValidateInputParts([]Part{call}); err == nil {
		t.Fatal("admitted an authored tool call as input")
	}
	if err := ValidateInputParts([]Part{text, content}); err != nil {
		t.Fatal(err)
	}
}

func TestToolArgumentsAndPartPayloadsFailClosed(t *testing.T) {
	for _, arguments := range []string{"", "null", "[]", `"text"`, "1", "{", `{} {}`, strings.Repeat(" ", MaxDocumentBytes) + `{}`} {
		part := callPart("call")
		part.Call.Arguments = json.RawMessage(arguments)
		if err := ValidateParts([]Part{part}); err == nil {
			t.Fatalf("accepted invalid or oversized arguments %q", arguments[:min(len(arguments), 32)])
		}
	}
	for _, name := range []string{"", "execute.shell", "execute:remote", strings.Repeat("x", 65)} {
		part := callPart("call")
		part.Call.Name = name
		if err := ValidateParts([]Part{part}); err == nil {
			t.Fatalf("accepted tool name %q", name)
		}
	}
	for _, part := range []Part{
		{Type: "tool_call"},
		{Type: "tool_result"},
		{Type: "text", Text: "text", Call: callPart("call").Call},
		{Type: "content", ReferenceID: "reference", Result: &ToolResult{CallID: "call"}},
		{Type: "tool_call", Call: callPart("call").Call, Text: "extra"},
		{Type: "tool_result", Result: &ToolResult{CallID: "../invalid"}},
		{Type: "tool_result", Result: &ToolResult{CallID: "call", Output: string([]byte{0xff})}},
	} {
		if err := ValidateParts([]Part{part}); err == nil {
			t.Fatalf("accepted ambiguous/invalid part: %+v", part)
		}
	}
}

func TestToolCallCountAndIdentityBounds(t *testing.T) {
	parts := make([]Part, 0, MaxToolCalls)
	for i := range MaxToolCalls {
		parts = append(parts, callPart(fmt.Sprintf("call_%d", i)))
	}
	if err := ValidateMessage(Assistant, parts); err != nil {
		t.Fatal(err)
	}
	if err := ValidateMessage(Assistant, append(parts, callPart("overflow"))); err == nil {
		t.Fatal("accepted too many tool calls")
	}
	parts[1].Call.ID = parts[0].Call.ID
	parts[1].Call.Arguments = json.RawMessage(`{"code":"different()"}`)
	if err := ValidateMessage(Assistant, parts); err == nil {
		t.Fatal("accepted duplicate call identity with different arguments")
	}
}
