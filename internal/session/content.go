package session

import (
	"encoding/hex"
	"fmt"
	"mime"
	"strings"
	"time"
)

const (
	MaxContentBytes        = 4 << 20
	MaxContentReferences   = 1024
	MaxSessionContentBytes = 64 << 20
)

// ContentReference projects immutable body metadata with a session's access
// reference. ID is unique only within SessionID; another owner may use the same
// opaque ID for different bytes. Possession of the digest grants no access.
type ContentReference struct {
	ID        string
	SessionID SessionID
	Digest    string
	Size      int64
	MediaType string
	CreatedAt time.Time
}

func (r ContentReference) Validate() error {
	if err := ValidateID(r.ID); err != nil {
		return err
	}
	if err := ValidateID(string(r.SessionID)); err != nil {
		return err
	}
	digest, err := hex.DecodeString(r.Digest)
	if err != nil || len(digest) != 32 || hex.EncodeToString(digest) != r.Digest {
		return fmt.Errorf("%w: content digest must be lowercase SHA-256", ErrInvalid)
	}
	if r.Size < 0 || r.Size > MaxContentBytes {
		return fmt.Errorf("%w: content exceeds the 4 MiB limit", ErrInvalid)
	}
	return ValidateMediaType(r.MediaType)
}

func ValidateMediaType(value string) error {
	media, params, err := mime.ParseMediaType(value)
	if err != nil || len(value) > 255 || media != value || len(params) != 0 || !strings.Contains(media, "/") {
		return fmt.Errorf("%w: content requires a canonical media type without parameters", ErrInvalid)
	}
	return nil
}
