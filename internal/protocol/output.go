package protocol

import (
	"encoding/base64"

	"github.com/context-labs/whip/internal/session"
)

// StructuredOutput carries validated JSON bytes without coercing user-defined
// numbers through a transport decoder's floating-point representation.
type StructuredOutput struct {
	TurnID     ID     `json:"turn_id"`
	MessageID  ID     `json:"message_id"`
	DataBase64 string `json:"data_base64"`
}

type TurnOutputResult struct {
	Output *StructuredOutput `json:"output"`
}

func TurnOutputFromDomain(value *session.StructuredOutput) TurnOutputResult {
	if value == nil {
		return TurnOutputResult{}
	}
	return TurnOutputResult{Output: &StructuredOutput{
		TurnID: ID(value.TurnID), MessageID: ID(value.MessageID), DataBase64: base64.StdEncoding.EncodeToString(value.Value),
	}}
}
