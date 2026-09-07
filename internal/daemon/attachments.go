package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io/fs"
	"mime"
	"slices"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

const (
	maxInputAttachments = 16
	maxAttachmentBytes  = 20 << 20
	maxTextAttachment   = 256 << 10
)

func attachmentBounds(attachments []protocol.InputAttachment) error {
	if len(attachments) > maxInputAttachments {
		return errors.New("input supports at most 16 attachments")
	}
	var total int64
	for _, attachment := range attachments {
		content := attachment.Content
		if attachment.Kind != "image" && attachment.Kind != "text" {
			return errors.New("attachment kind must be image or text")
		}
		if content.ReferenceID == "" || !validSHA256(content.Digest) || content.Size < 0 || content.Size > maxAttachmentBytes || len(attachment.Name) > 256 {
			return errors.New("attachment requires a content identity, size up to 20 MiB and a name up to 256 bytes")
		}
		if attachment.Kind == "text" && content.Size > maxTextAttachment {
			return errors.New("text attachment exceeds 256 KiB")
		}
		total += content.Size
		if total > maxAttachmentBytes {
			return errors.New("combined attachments exceed 20 MiB")
		}
	}
	return nil
}

func sameAttachmentContent(expected protocol.ContentHandle, actual session.ContentMetadata) bool {
	return expected.ReferenceID == actual.ReferenceID && expected.Digest == actual.Digest && expected.Size == actual.Size && expected.MediaType == actual.MediaType
}

func (s *Server) validateCommandAttachments(ctx context.Context, clientID string, params CommandParams, digest string, root *Session) (*CommandResult, error) {
	if params.Operation != "submit" && params.Operation != "steer" && params.Operation != "agent.submit" {
		return nil, nil
	}
	var payload struct {
		ID          string                     `json:"id"`
		Attachments []protocol.InputAttachment `json:"attachments"`
	}
	if err := json.Unmarshal(params.Payload, &payload); err != nil {
		return nil, err
	}
	if len(payload.Attachments) == 0 {
		return nil, nil
	}
	// A matching retry observes accepted work even if its grant was subsequently
	// revoked. Revalidating a historical command would misreport its delivery.
	if record, err := s.daemon.store.LoadCommand(ctx, clientID, params.CommandID); err == nil {
		if record.RequestDigest != digest {
			return nil, session.ErrCommandConflict
		}
		result, err := s.commandRecordResult(ctx, record, nil)
		return &result, err
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	agentID := root.meta.ID
	if params.Operation == "agent.submit" {
		agentID = payload.ID
		if agentID == "" {
			return nil, rpcFailure(-32602, "child attachment requires an agent ID")
		}
	}
	return nil, root.validateAttachmentReferences(ctx, agentID, payload.Attachments)
}

// Admission checks only metadata and existing grants. Body decoding/hashing runs
// on the turn worker, while the accepted journal keeps the compact original refs.
func (s *Session) validateAttachmentReferences(ctx context.Context, agentID string, attachments []protocol.InputAttachment) error {
	if err := attachmentBounds(attachments); err != nil {
		return rpcFailure(-32602, err.Error())
	}
	for _, attachment := range attachments {
		_, meta, err := s.store.ReadContent(ctx, attachment.Content.ReferenceID, s.meta.ID, agentID, 0, 1)
		if err != nil {
			return err
		}
		if !sameAttachmentContent(attachment.Content, meta) {
			return rpcFailure(-32602, "attachment content identity does not match stored metadata")
		}
	}
	return nil
}

func (s *Session) resolveAttachments(ctx context.Context, agentID string, payload SubmitPayload) (string, []llm.ContentPart, error) {
	if err := attachmentBounds(payload.Attachments); err != nil {
		return "", nil, fmt.Errorf("%w: %w", session.ErrInvalidInput, err)
	}
	parts := slices.Clone(payload.Parts)
	for _, attachment := range payload.Attachments {
		content := attachment.Content
		data := make([]byte, 0, int(content.Size))
		for offset := int64(0); ; {
			chunk, meta, err := s.store.ReadContent(ctx, content.ReferenceID, s.meta.ID, agentID, offset, min(MaxContentChunk, max(1, int(content.Size-offset))))
			if err != nil {
				if errors.Is(err, session.ErrContentAccess) || errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
					return "", nil, fmt.Errorf("%w: read attachment: %w", session.ErrInvalidInput, err)
				}
				return "", nil, fmt.Errorf("read attachment: %w", err)
			}
			if !sameAttachmentContent(content, meta) || int64(len(data)+len(chunk)) > content.Size {
				return "", nil, fmt.Errorf("%w: attachment metadata changed", session.ErrInvalidInput)
			}
			data = append(data, chunk...)
			// One read at the declared end proves there are no surplus bytes,
			// including for empty content and exact chunk-size multiples.
			if offset == content.Size {
				break
			}
			if len(chunk) == 0 {
				return "", nil, fmt.Errorf("%w: attachment transfer is incomplete", session.ErrInvalidInput)
			}
			offset += int64(len(chunk))
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != content.Digest {
			return "", nil, fmt.Errorf("%w: attachment digest mismatch", session.ErrInvalidInput)
		}
		if attachment.Kind == "text" {
			if !utf8.Valid(data) {
				return "", nil, fmt.Errorf("%w: text attachment must contain UTF-8", session.ErrInvalidInput)
			}
			text := string(data)
			if attachment.Name != "" {
				text = fmt.Sprintf("Attachment %q:\n%s", attachment.Name, text)
			}
			// Keep attachment text out of authored-input mention/skill expansion.
			parts = append(parts, llm.ContentPart{Type: "text", Text: text})
			continue
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(data))
		mediaType, _, mimeErr := mime.ParseMediaType(content.MediaType)
		if err != nil || mimeErr != nil || mediaType != "image/"+format || config.Width <= 0 || config.Height <= 0 || config.Width > 32768 || config.Height > 32768 || int64(config.Width)*int64(config.Height) > 64<<20 {
			return "", nil, fmt.Errorf("%w: image attachment has unsupported format, media type or dimensions", session.ErrInvalidInput)
		}
		parts = append(parts, llm.ImagePart(format, data))
	}
	return payload.Text, parts, nil
}

func (s *AgentSession) validateImageInput(parts []llm.ContentPart) error {
	if !s.agent.Vision && slices.ContainsFunc(parts, func(part llm.ContentPart) bool { return part.Type == "image_url" }) {
		return fmt.Errorf("%w: selected model does not support image inputs; choose a vision model", session.ErrInvalidInput)
	}
	return nil
}
