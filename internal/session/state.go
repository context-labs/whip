package session

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type StateScope string

const (
	SessionState       StateScope = "session"
	TreeState          StateScope = "tree"
	MaxStateValueBytes            = 64 << 20
	MaxTreeStateBytes             = 1 << 30
	MaxStateVersions              = 1024
	MaxStateReadBytes             = 64 << 10
)

// StateValue identifies an immutable version. A nil SessionID grants visibility
// to the tree; a nonnil SessionID confines visibility to that session.
// Digest alone never authorizes a content read.
type StateValue struct {
	ID        string     `json:"id"`
	TreeID    TreeID     `json:"tree_id"`
	SessionID *SessionID `json:"session_id"`
	Key       string     `json:"key"`
	Revision  int64      `json:"revision,string"`
	AuthorID  SessionID  `json:"author_id"`
	Digest    string     `json:"digest"`
	Size      int64      `json:"size,string"`
	CreatedAt time.Time  `json:"created_at"`
}

// StateWrite follows publication of validated immutable JSON. ExpectedRevision
// is always explicit: zero creates a key, a positive value replaces that head.
type StateWrite struct {
	ID               string     `json:"-"`
	SessionID        SessionID  `json:"-"`
	Scope            StateScope `json:"scope"`
	Key              string     `json:"key"`
	ExpectedRevision int64      `json:"expected_revision,string"`
	Digest           string     `json:"digest"`
	Size             int64      `json:"size,string"`
	// SubmittedBytes is set by trusted staging before append merges the value.
	SubmittedBytes int64 `json:"submitted_bytes,string"`
}

func ValidateStateKey(key string) error {
	if strings.TrimSpace(key) == "" || len(key) > 256 || !utf8.ValidString(key) || strings.ContainsRune(key, 0) {
		return fmt.Errorf("%w: invalid state key", ErrInvalid)
	}
	return nil
}

func (scope StateScope) Validate() error {
	if scope != SessionState && scope != TreeState {
		return fmt.Errorf("%w: invalid state scope", ErrInvalid)
	}
	return nil
}

func (w StateWrite) Validate() error {
	if err := w.ValidateIdentity(); err != nil {
		return err
	}
	digest, err := hex.DecodeString(w.Digest)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != w.Digest || w.Size < 1 || w.Size > MaxStateValueBytes {
		return fmt.Errorf("%w: invalid state body", ErrInvalid)
	}
	if w.SubmittedBytes < 1 || w.SubmittedBytes > MaxStateValueBytes {
		return fmt.Errorf("%w: invalid submitted state size", ErrInvalid)
	}
	return nil
}

// ValidateIdentity runs before publishing a body to the content store.
func (w StateWrite) ValidateIdentity() error {
	for _, id := range []string{w.ID, string(w.SessionID)} {
		if err := ValidateID(id); err != nil {
			return err
		}
	}
	if err := w.Scope.Validate(); err != nil {
		return err
	}
	if err := ValidateStateKey(w.Key); err != nil {
		return err
	}
	if w.ExpectedRevision < 0 {
		return fmt.Errorf("%w: invalid state revision", ErrInvalid)
	}
	return nil
}

// StatePut is guest input. A pointer distinguishes a missing revision from an
// explicit zero (create); all writes compare against the caller's observation.
type StatePut struct {
	Scope            StateScope      `json:"scope"`
	Key              string          `json:"key"`
	ExpectedRevision *int64          `json:"expected_revision,string"`
	Value            json.RawMessage `json:"value"`
}

type StateKey struct {
	Scope StateScope `json:"scope"`
	Key   string     `json:"key"`
}

type StateRead struct {
	ID     string `json:"id"`
	Offset int64  `json:"offset,string"`
	Length int    `json:"length"`
}

type StateList struct {
	Scope StateScope `json:"scope"`
	After string     `json:"after"`
	Limit int        `json:"limit"`
}

type StateHistory struct {
	StateKey
	After int64 `json:"after,string"`
	Limit int   `json:"limit"`
}

type StateItems struct {
	Items []StateValue `json:"items"`
}

// StateEntry inlines small values. Large values retain only their immutable
// handle and require explicit bounded reads; absence differs from JSON null.
type StateEntry struct {
	Version StateValue      `json:"version"`
	Value   json.RawMessage `json:"value,omitempty"`
}

type StateChunk struct {
	Version StateValue `json:"version"`
	Offset  int64      `json:"offset,string"`
	Data    []byte     `json:"data"`
}
