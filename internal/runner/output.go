package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

func outputInstructions(schema json.RawMessage) string {
	raw := bytes.TrimSpace(schema)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	return "\nOutput contract: your final assistant response must contain exactly one JSON value matching this schema, with text parts only. Tool calls may precede the final response. Schema:\n" + string(raw)
}

func outputCorrection(err error) string {
	detail := *Failure(err).Failure
	if len(detail) > 1024 {
		detail = detail[:1024]
		for !utf8.ValidString(detail) {
			detail = detail[:len(detail)-1]
		}
	}
	return fmt.Sprintf("Output contract: your previous final response did not match the required schema (%s). Correct it once: your next response must contain exactly one JSON value matching the schema, without tool calls or any other content.", detail)
}
