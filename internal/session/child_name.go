package session

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxChildNameBytes = 128

// Child names are display labels. Session IDs remain the only routing identity.
func ValidateChildName(name string) error {
	if err := ValidateText(name, MaxChildNameBytes); err != nil {
		return err
	}
	if !utf8.ValidString(name) || strings.TrimSpace(name) != name || strings.ContainsFunc(name, unicode.IsControl) {
		return fmt.Errorf("%w: child name must be trimmed text without control characters", ErrInvalid)
	}
	return nil
}

func DefaultChildName(id SessionID) string {
	short := strings.TrimPrefix(string(id), "session_")
	return "agent-" + strings.ToLower(short[:min(8, len(short))])
}
