package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidateOutputPreservesOneExactJSONValue(t *testing.T) {
	for _, tc := range []struct{ name, schema, text, want string }{
		{"object", `{"type":"object","required":["name"],"properties":{"name":{"type":"string"}},"additionalProperties":false}`, ` { "name": "Ada" } `, `{"name":"Ada"}`},
		{"json fence", `{"type":"object"}`, "```json\n{ \"a\": 1 }\n```", `{"a":1}`},
		{"plain fence", `{"type":"boolean"}`, "```\ntrue\n```", `true`},
		{"CRLF fence", `{"type":"array"}`, "```json\r\n[1, 2]\r\n```", `[1,2]`},
		{"scalar string", `{"type":"string"}`, `"hello"`, `"hello"`},
		{"null value", `{"type":"null"}`, `null`, `null`},
		{"any value", `{}`, `false`, `false`},
		{"large exact integer", `{"type":"integer"}`, `900719925474099312345678901234567890`, `900719925474099312345678901234567890`},
		{"numeric lexemes", `{"type":"array","items":{"type":"number"}}`, `[ -0, 1.2300, 7.00e+9, 9007199254740993 ]`, `[-0,1.2300,7.00e+9,9007199254740993]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateOutput(json.RawMessage(tc.schema), []Part{{Type: "text", Text: tc.text}})
			if err != nil || string(got) != tc.want {
				t.Fatalf("got=%s err=%v want=%s", got, err, tc.want)
			}
		})
	}
	// Text fragments are concatenated, without inserting bytes into a JSON token.
	got, err := ValidateOutput(json.RawMessage(`{"type":"integer"}`), []Part{{Type: "text", Text: "900719925"}, {Type: "text", Text: "4740993"}})
	if err != nil || string(got) != "9007199254740993" {
		t.Fatalf("fragmented number=%s %v", got, err)
	}
	// A returned value owns its bytes; mutating it cannot rewrite source evidence.
	parts := []Part{{Type: "text", Text: `{"x":1}`}}
	first, err := ValidateOutput(json.RawMessage(`{}`), parts)
	if err != nil {
		t.Fatal(err)
	}
	first[0] = '['
	second, err := ValidateOutput(json.RawMessage(`{}`), parts)
	if err != nil || string(second) != `{"x":1}` {
		t.Fatal("validated output aliased source", err)
	}
}

func TestValidateOutputNoContractDiffersFromJSONNull(t *testing.T) {
	for _, schema := range []json.RawMessage{nil, {}, json.RawMessage(" \n "), json.RawMessage(" null ")} {
		value, err := ValidateOutput(schema, []Part{{Type: "content", ReferenceID: "reference"}})
		if value != nil || err != nil {
			t.Fatalf("unconfigured output=%s %v", value, err)
		}
	}
	value, err := ValidateOutput(json.RawMessage(`{"type":"null"}`), []Part{{Type: "text", Text: "null"}})
	if value == nil || !bytes.Equal(value, []byte("null")) || err != nil {
		t.Fatalf("null output mistaken for absent contract: %s %v", value, err)
	}
	raw, err := json.Marshal(StructuredOutput{TurnID: "turn", MessageID: "answer", Value: value})
	if err != nil || !bytes.Contains(raw, []byte(`"value":null`)) {
		t.Fatalf("valid JSON null omitted: %s %v", raw, err)
	}
}

func TestValidateOutputRejectsAmbiguousOrIncompleteResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		parts  []Part
	}{
		{"wrong type", `{"type":"string"}`, []Part{{Type: "text", Text: "42"}}},
		{"extra JSON", `{}`, []Part{{Type: "text", Text: "{} {}"}}},
		{"trailing prose", `{}`, []Part{{Type: "text", Text: "{} done"}}},
		{"leading prose", `{}`, []Part{{Type: "text", Text: "Here: {}"}}},
		{"missing close fence", `{}`, []Part{{Type: "text", Text: "```json\n{}"}}},
		{"wrong language", `{}`, []Part{{Type: "text", Text: "```js\n{}\n```"}}},
		{"inline closing fence", `{}`, []Part{{Type: "text", Text: "```json\n{}```"}}},
		{"extra fence", `{}`, []Part{{Type: "text", Text: "```json\n{}\n```\n```"}}},
		{"empty value", `{}`, []Part{{Type: "text", Text: "   "}}},
		{"empty parts", `{}`, nil},
		{"text plus content", `{}`, []Part{{Type: "text", Text: "{}"}, {Type: "content", ReferenceID: "reference"}}},
		{"tool call", `{}`, []Part{callPart("call")}},
		{"ambiguous text fields", `{}`, []Part{{Type: "text", Text: "{}", ReferenceID: "reference"}}},
		{"invalid UTF8", `{}`, []Part{{Type: "text", Text: "\"\xff\""}}},
		{"nonobject schema", `true`, []Part{{Type: "text", Text: "{}"}}},
		{"schema null string", `"null"`, []Part{{Type: "text", Text: "{}"}}},
		{"unresolved remote schema", `{"$ref":"https://invalid.example/schema"}`, []Part{{Type: "text", Text: "{}"}}},
		{"schema trailing data", `{} {}`, []Part{{Type: "text", Text: "{}"}}},
		{"oversized text", `{}`, []Part{{Type: "text", Text: strings.Repeat("x", MaxDocumentBytes+1)}}},
		{"oversized schema", `{` + strings.Repeat(" ", MaxDocumentBytes) + `}`, []Part{{Type: "text", Text: "{}"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := ValidateOutput(json.RawMessage(tc.schema), tc.parts)
			if !errors.Is(err, ErrInvalid) || raw != nil {
				t.Fatalf("invalid response accepted: %s %v", raw, err)
			}
		})
	}
}
