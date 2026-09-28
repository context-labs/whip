package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestToolPartConversionsOwnTheirArguments(t *testing.T) {
	original := session.Part{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}
	wire := PartFromDomain(original)
	domain := wire.Domain()
	if !reflect.DeepEqual(original, domain) {
		t.Fatalf("tool call did not round trip: %+v", domain)
	}
	original.Call.Arguments[0] = '['
	if wire.Call.Arguments[0] != '{' {
		t.Fatal("wire part aliases the domain argument buffer")
	}
	wire.Call.Arguments[0] = '['
	if domain.Call.Arguments[0] != '{' {
		t.Fatal("domain part aliases the wire argument buffer")
	}
	result := session.Part{Type: "tool_result", Result: &session.ToolResult{CallID: "call", Output: "failed", IsError: true}}
	if got := PartFromDomain(result).Domain(); !reflect.DeepEqual(got, result) {
		t.Fatalf("tool result did not round trip: %+v", got)
	}
}

func TestMessageSchemasEnforceToolRoleOwnership(t *testing.T) {
	call := Part{Type: "tool_call", Call: &ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{"code":"print(1)"}`)}}
	result := Part{Type: "tool_result", Result: &ToolResult{CallID: "call", Output: "", IsError: false}}
	for _, tc := range []struct {
		role  string
		parts []Part
		valid bool
	}{
		{"assistant", []Part{call}, true},
		{"tool", []Part{result}, true},
		{"user", []Part{call}, false},
		{"system", []Part{call}, false},
		{"assistant", []Part{result}, false},
		{"tool", []Part{result, result}, false},
		{"tool", []Part{{Type: "text", Text: "unstructured"}}, false},
	} {
		value := HistoryResult{Items: []Message{{ID: "message", SessionID: "session", TurnID: "turn", Sequence: 1, Role: tc.role, Parts: tc.parts, CreatedAt: "2026-09-27T00:00:00Z"}}}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate("HistoryResult", raw); (err == nil) != tc.valid {
			t.Fatalf("role=%s parts=%+v valid=%v: %v", tc.role, tc.parts, tc.valid, err)
		}
	}
}

func TestAdmissionSchemaCannotSmuggleToolCallsInInput(t *testing.T) {
	value := Admission{Input: &Input{
		ID: "input", SessionID: "session", Source: "user", Kind: "prompt", State: "queued", CreatedAt: "2026-09-27T00:00:00Z",
		Parts: []Part{{Type: "tool_call", Call: &ToolCall{ID: "call", Name: "execute", Arguments: json.RawMessage(`{}`)}}},
	}, Receipt: Receipt{Identity: RequestIdentity{ClientID: "client", RequestID: "request"}, Digest: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", CreatedAt: "2026-09-27T00:00:00Z"}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate("Admission", raw); err == nil {
		t.Fatal("admission schema accepted a user-authored tool call")
	}
}

func TestInputSchemaSeparatesPromptAndCompaction(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  string
		parts []Part
		valid bool
	}{
		{"prompt", "prompt", []Part{{Type: "text", Text: "Work"}}, true},
		{"empty prompt", "prompt", []Part{}, false},
		{"compact", "compact", []Part{}, true},
		{"compact with authored text", "compact", []Part{{Type: "text", Text: "Work"}}, false},
		{"null compact parts", "compact", nil, false},
		{"unknown kind", "other", []Part{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := Input{ID: "input", SessionID: "session", Source: "user", Kind: tc.kind, State: "queued", Parts: tc.parts, CreatedAt: "2026-09-28T00:00:00Z"}
			raw, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := Validate("Input", raw); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}
