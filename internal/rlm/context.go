package rlm

import (
	"encoding/json"

	"github.com/context-labs/whip/internal/llm"
)

func MarshalHistory(history []llm.Message) ([]byte, error) {
	return json.Marshal(history)
}
