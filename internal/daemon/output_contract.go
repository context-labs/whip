package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// outputContract validates a turn's final assistant message against the
// definition's output schema. The first mismatch queues one corrective notice
// and asks the loop for another round; the second fails the turn. A valid
// message is stored on the turn journal for the command result.
func (session *AgentSession) outputContract(schema json.RawMessage) (func(string) (bool, error), error) {
	var definition jsonschema.Schema
	if err := json.Unmarshal(schema, &definition); err != nil {
		return nil, fmt.Errorf("output contract: %w", err)
	}
	resolved, err := definition.Resolve(nil) // remote references never cause network requests
	if err != nil {
		return nil, fmt.Errorf("output contract: %w", err)
	}
	attempts := 0
	return func(text string) (bool, error) {
		raw, value, err := decodeOutputMessage(text)
		if err == nil {
			err = resolved.Validate(value)
		}
		if err == nil {
			session.mu.Lock()
			session.turn.Output = raw
			session.mu.Unlock()
			return false, nil
		}
		attempts++
		if attempts > 1 {
			return false, fmt.Errorf("output_invalid: the final message does not match the definition's output schema: %v", err)
		}
		session.addHookNotice(fmt.Sprintf("Output contract: your previous final message did not match the required schema (%v). Reply with exactly one JSON value matching the schema and nothing else.", err))
		return true, nil
	}, nil
}

// decodeOutputMessage reads one JSON value from a final message, tolerating a
// surrounding code fence.
func decodeOutputMessage(text string) (json.RawMessage, any, error) {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```")
		if newline := strings.IndexByte(trimmed, '\n'); newline >= 0 && !strings.ContainsAny(trimmed[:newline], "{[\"") {
			trimmed = trimmed[newline+1:]
		}
		trimmed = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(trimmed), "```"))
	}
	if trimmed == "" {
		return nil, nil, fmt.Errorf("the message is empty")
	}
	var value any
	if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
		return nil, nil, fmt.Errorf("the message is not one JSON value: %v", err)
	}
	return json.RawMessage(trimmed), value, nil
}
