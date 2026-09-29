package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/context-labs/whip/internal/session"
)

func validateDesignContext(ctx context.Context, q querier, request Submission) error {
	design := request.DesignContext
	if design == nil {
		return nil
	}
	if request.Kind != session.PromptInput || request.Source != session.UserInput {
		return fmt.Errorf("%w: design context belongs to authored prompt input", session.ErrInvalid)
	}
	if _, err := design.Presentation(request.Parts); err != nil {
		return err
	}
	for _, evidence := range []struct{ id, prefix string }{
		{design.ContextAttachmentID, "text/"}, {design.ScreenshotAttachmentID, "image/"},
	} {
		if evidence.id == "" {
			continue
		}
		ref, err := readContent(ctx, q, request.SessionID, evidence.id)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(ref.MediaType, evidence.prefix) {
			return fmt.Errorf("%w: design context evidence has the wrong media type", session.ErrInvalid)
		}
	}
	return nil
}

func encodeDesignContext(value *session.DesignContext) (*string, error) {
	if value == nil {
		return nil, nil //nolint:nilnil // SQL NULL preserves absent display metadata.
	}
	raw, err := encode(value)
	if err != nil {
		return nil, err
	}
	return &raw, nil
}

func decodeDesignContext(raw *string, parts []session.Part) (*session.DesignContextPresentation, error) {
	if raw == nil {
		return nil, nil //nolint:nilnil // SQL NULL preserves absent display metadata.
	}
	var value session.DesignContext
	if err := json.Unmarshal([]byte(*raw), &value); err != nil {
		return nil, err
	}
	return value.Presentation(parts)
}
