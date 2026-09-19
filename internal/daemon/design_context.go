package daemon

import (
	"fmt"

	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

// Every resolved attachment appends exactly one part. Caller-provided indices
// are never used; references must identify a unique attachment of the right kind.
func designContextPresentation(payload SubmitPayload) (llm.TranscriptPresentation, error) {
	d := payload.DesignContext
	if d == nil {
		return llm.TranscriptPresentation{}, nil
	}
	if err := d.Validate(); err != nil {
		return llm.TranscriptPresentation{}, fmt.Errorf("%w: %w", session.ErrInvalidInput, err)
	}
	contextIndex, screenshotIndex := -1, -1
	offset := len(payload.Parts)
	if payload.Text != "" {
		offset++
	}
	for i, attachment := range payload.Attachments {
		id := attachment.Content.ReferenceID
		if id == d.ContextAttachmentID {
			if attachment.Kind != "text" || contextIndex >= 0 {
				return llm.TranscriptPresentation{}, fmt.Errorf("%w: design context must reference one text attachment", session.ErrInvalidInput)
			}
			contextIndex = offset + i
		}
		if d.ScreenshotAttachmentID != "" && id == d.ScreenshotAttachmentID {
			if attachment.Kind != "image" || screenshotIndex >= 0 {
				return llm.TranscriptPresentation{}, fmt.Errorf("%w: design screenshot must reference one image attachment", session.ErrInvalidInput)
			}
			screenshotIndex = offset + i
		}
	}
	if contextIndex < 0 || d.ScreenshotAttachmentID != "" && screenshotIndex < 0 {
		return llm.TranscriptPresentation{}, fmt.Errorf("%w: design context attachment reference is missing", session.ErrInvalidInput)
	}
	design := &llm.DesignContextPresentation{DesignContextInput: *d.Clone(), ContextPartIndex: contextIndex}
	if screenshotIndex >= 0 {
		design.ScreenshotPartIndex = &screenshotIndex
	}
	return llm.TranscriptPresentation{Version: 1, DesignContext: design}, nil
}
