package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const MaxModelCaptureBytes = 16 << 20

// ModelCapture is inspection evidence for one prepared request, never an input
// authority. It excludes private continuations, credentials and attachment bytes.
type ModelCapture struct {
	RequestDigest   string            `json:"request_digest"`
	SourceDigest    string            `json:"source_digest"`
	Instructions    CapturedText      `json:"instructions"`
	Notices         CapturedText      `json:"notices"`
	Messages        []CapturedMessage `json:"messages"`
	ToolsDigest     string            `json:"tools_digest"`
	ToolsCount      int               `json:"tools_count"`
	ContextComplete bool              `json:"context_complete"`
}

type CapturedText struct {
	Digest string             `json:"digest"`
	Bytes  int64              `json:"bytes"`
	Status string             `json:"status"`
	Chunks []ContentReference `json:"chunks"`
	// Data exists only between preparation and durable body publication.
	Data []byte `json:"-"`
}

type CapturedMessage struct {
	ID          MessageID `json:"id"`
	Role        Role      `json:"role"`
	PartsDigest string    `json:"parts_digest"`
	PartsCount  int       `json:"parts_count"`
}

func CaptureDigest(data []byte) string {
	value := sha256.Sum256(data)
	return hex.EncodeToString(value[:])
}

// Seal binds only immutable source evidence. Availability can change from
// prepared to quota/storage-error during publication without changing identity.
func (c *ModelCapture) Seal() {
	source := *c
	source.RequestDigest, source.SourceDigest = "", ""
	for _, body := range []*CapturedText{&source.Instructions, &source.Notices} {
		body.Status, body.Chunks, body.Data = "", nil, nil
	}
	raw, _ := json.Marshal(source)
	c.SourceDigest = CaptureDigest(raw)
}

func (c ModelCapture) Validate(owner SessionID) error {
	validDigest := func(value string) bool {
		raw, err := hex.DecodeString(value)
		return err == nil && len(raw) == sha256.Size && hex.EncodeToString(raw) == value
	}
	if !validDigest(c.RequestDigest) || !validDigest(c.SourceDigest) || !validDigest(c.ToolsDigest) || len(c.Messages) > 128 || c.ToolsCount < 0 || c.ToolsCount > 1024 {
		return ErrInvalid
	}
	sealed := c
	sealed.Seal()
	if sealed.SourceDigest != c.SourceDigest {
		return fmt.Errorf("%w: capture source digest mismatch", ErrInvalid)
	}
	for _, message := range c.Messages {
		if message.ID != "" && ValidateID(string(message.ID)) != nil {
			return ErrInvalid
		}
		if message.Role != User && message.Role != Assistant && message.Role != Tool && message.Role != System || !validDigest(message.PartsDigest) || message.PartsCount < 0 || message.PartsCount > 1024 {
			return ErrInvalid
		}
	}
	var total int64
	for _, body := range []CapturedText{c.Instructions, c.Notices} {
		if !validDigest(body.Digest) || body.Bytes < 0 || len(body.Chunks) > 4 {
			return ErrInvalid
		}
		switch body.Status {
		case "available":
			total += body.Bytes
		case "oversized", "quota", "storage_error":
			if len(body.Chunks) != 0 {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		var size int64
		for _, chunk := range body.Chunks {
			if chunk.Validate() != nil || chunk.SessionID != owner || chunk.MediaType != "text/plain" || chunk.ID != "model_"+chunk.Digest {
				return ErrInvalid
			}
			size += chunk.Size
		}
		if body.Status == "available" && (size != body.Bytes || body.Bytes > MaxModelCaptureBytes) {
			return ErrInvalid
		}
	}
	if total > MaxModelCaptureBytes {
		return ErrInvalid
	}
	return nil
}

type ModelInspection struct {
	SessionID     SessionID
	AttemptID     ModelAttemptID
	TurnID        TurnID
	RequestDigest string
	Capture       *ModelCapture
	Compaction    *CompactionMetadata
}
