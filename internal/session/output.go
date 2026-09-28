package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// StructuredOutput is a read projection of a succeeded turn's final message
// under its captured schema. Value may be the JSON null value.
type StructuredOutput struct {
	TurnID    TurnID          `json:"turn_id"`
	MessageID MessageID       `json:"message_id"`
	Value     json.RawMessage `json:"value"`
}

// ValidateOutput returns compact JSON without converting numeric lexemes through
// floating point. A nil/empty/null schema means no contract and returns nil, nil.
// A configured contract requires the entire assistant response to be text and
// contain one JSON value, optionally inside one matching Markdown code fence.
func ValidateOutput(schema json.RawMessage, parts []Part) (json.RawMessage, error) {
	resolved, err := compileSchema(schema, true)
	if err != nil || resolved == nil {
		return nil, err
	}
	var text strings.Builder
	for _, part := range parts {
		if part.Type != "text" || !utf8.ValidString(part.Text) || len(part.Text) > MaxDocumentBytes-text.Len() {
			return nil, fmt.Errorf("%w: structured output requires bounded UTF-8 text parts only", ErrInvalid)
		}
		text.WriteString(part.Text)
	}
	if err := ValidateMessage(Assistant, parts); err != nil {
		return nil, err
	}
	body, err := outputBody(text.String())
	if err != nil {
		return nil, err
	}
	value, err := decodeExactJSON([]byte(body))
	if err != nil {
		return nil, err
	}
	if err := resolved.Validate(value); err != nil {
		return nil, fmt.Errorf("%w: output does not match schema: %w", ErrInvalid, err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(body)); err != nil {
		return nil, fmt.Errorf("%w: output JSON: %w", ErrInvalid, err)
	}
	return json.RawMessage(compact.Bytes()), nil
}

func outputBody(text string) (string, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text, nil
	}
	header, body, found := strings.Cut(text, "\n")
	header = strings.TrimSuffix(header, "\r")
	if !found || (header != "```" && header != "```json") {
		return "", fmt.Errorf("%w: output fence must start with ``` or ```json on its own line", ErrInvalid)
	}
	closing := strings.LastIndexByte(body, '\n')
	if closing < 0 || strings.TrimSpace(body[closing+1:]) != "```" {
		return "", fmt.Errorf("%w: output fence requires a matching closing line", ErrInvalid)
	}
	return strings.TrimSpace(body[:closing]), nil
}
