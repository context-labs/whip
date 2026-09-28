package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

const MaxContinuationBytes = 1 << 20

var ErrContinuationLimit = errors.New("provider continuation context exceeds the size limit")

// ModelContinuation is immutable, private provider state coupled to an authored
// assistant message. It is never part of a public message or compaction source.
// Scope is a credential-keyed digest of the provider route and model, not a key.
type ModelContinuation struct {
	Scope string `json:"scope"`
	Data  string `json:"data"`
}

func (c ModelContinuation) Validate() error {
	if len(c.Scope) != 64 {
		return fmt.Errorf("%w: invalid continuation scope", ErrInvalid)
	}
	for _, char := range c.Scope {
		if char < '0' || char > '9' && char < 'a' || char > 'f' {
			return fmt.Errorf("%w: invalid continuation scope", ErrInvalid)
		}
	}
	var items []json.RawMessage
	if len(c.Data) > MaxContinuationBytes || !utf8.ValidString(c.Data) || json.Unmarshal([]byte(c.Data), &items) != nil || items == nil {
		return fmt.Errorf("%w: invalid continuation data", ErrInvalid)
	}
	raw, err := json.Marshal(c)
	if err != nil || len(raw) > MaxContinuationBytes {
		return fmt.Errorf("%w: continuation exceeds the size limit", ErrInvalid)
	}
	return nil
}
