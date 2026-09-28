package session

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const AutomaticTitlePurpose = "automatic_title"

const AutomaticTitleClientID = "automatic-title"

// AutomaticTitleIdentity addresses independently admitted naming maintenance.
// A decision can precede its receipt; reading either never admits work.
func AutomaticTitleIdentity(tree TreeID) RequestIdentity {
	return RequestIdentity{ClientID: AutomaticTitleClientID, RequestID: string(tree)}
}

// AutomaticTitleDecision records the single initialization decision for a tree.
// It is immutable, including when later manual metadata supersedes its revision.
type AutomaticTitleDecision struct {
	TreeID           TreeID         `json:"tree_id"`
	SessionID        SessionID      `json:"session_id"`
	InputID          *InputID       `json:"input_id"`
	ConfigRevision   Revision       `json:"config_revision,string"`
	ExpectedRevision Revision       `json:"expected_revision,string"`
	Enabled          bool           `json:"enabled"`
	Model            ModelSelection `json:"model"`
	Source           string         `json:"source"`
	Reason           string         `json:"reason"`
	CreatedAt        time.Time      `json:"created_at"`
}

type AutomaticTitleDraft struct{ Text string }

// AutomaticTitleResult is attempt-linked evidence, never another selected title.
// Applied remains true for an exact retry even after later manual metadata edits.
type AutomaticTitleResult struct {
	TreeID    TreeID         `json:"tree_id"`
	AttemptID ModelAttemptID `json:"attempt_id"`
	Text      string         `json:"text"`
	Applied   bool           `json:"applied"`
	CreatedAt time.Time      `json:"created_at"`
}

type AutomaticTitleSettlement struct {
	Attempt   ModelAttempt
	Candidate *AutomaticTitleResult
	Rejection *string
}

// TitleSource uses only authored text. Attachments are never expanded for naming.
func TitleSource(parts []Part) string {
	var text strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			text.WriteString(part.Text)
			text.WriteByte(' ')
		}
	}
	runes := []rune(strings.Join(strings.Fields(text.String()), " "))
	return string(runes[:min(300, len(runes))])
}

func TitleFallback(source string) string {
	runes := []rune(source)
	return string(runes[:min(64, len(runes))])
}

func ValidateAutomaticTitle(text string) error {
	if !utf8.ValidString(text) || strings.TrimSpace(text) != text || utf8.RuneCountInString(text) > 80 || text == "" {
		return fmt.Errorf("%w: generated title requires one nonempty line of at most 80 runes", ErrInvalid)
	}
	for _, r := range text {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("%w: generated title cannot contain line breaks or control characters", ErrInvalid)
		}
	}
	return nil
}
