package session

import (
	"encoding/json"
	"fmt"
	"slices"
	"unicode/utf8"
)

// DesignContext identifies human-selected evidence by owner-scoped content
// reference. It is display metadata; the original parts remain the model input.
type DesignContext struct {
	ContextAttachmentID    string                 `json:"context_attachment_id"`
	ScreenshotAttachmentID string                 `json:"screenshot_attachment_id,omitempty"`
	Elements               []DesignContextElement `json:"elements"`
	ElementCount           int                    `json:"element_count"`
	PageURL                string                 `json:"page_url,omitempty"`
	PageTitle              string                 `json:"page_title,omitempty"`
}

type DesignContextElement struct {
	Label    string `json:"label"`
	Selector string `json:"selector,omitempty"`
}

// DesignContextPresentation indexes the canonical message parts, including any
// leading authored text. Callers cannot supply these derived coordinates.
type DesignContextPresentation struct {
	DesignContext
	ContextPartIndex    int  `json:"context_part_index"`
	ScreenshotPartIndex *int `json:"screenshot_part_index,omitempty"`
}

func (d *DesignContext) Validate() error {
	if d == nil {
		return nil
	}
	bounded := func(value string, limit int) bool { return len(value) <= limit && utf8.ValidString(value) }
	if ValidateID(d.ContextAttachmentID) != nil ||
		(d.ScreenshotAttachmentID != "" && ValidateID(d.ScreenshotAttachmentID) != nil) ||
		d.ContextAttachmentID == d.ScreenshotAttachmentID || len(d.Elements) > 8 ||
		d.ElementCount < len(d.Elements) || d.ElementCount > 1000 ||
		!bounded(d.PageURL, 2048) || !bounded(d.PageTitle, 256) {
		return fmt.Errorf("%w: invalid design context descriptor or bounds", ErrInvalid)
	}
	for _, element := range d.Elements {
		if element.Label == "" || !bounded(element.Label, 160) || !bounded(element.Selector, 256) {
			return fmt.Errorf("%w: invalid design context element summary", ErrInvalid)
		}
	}
	raw, err := json.Marshal(d)
	if err != nil || len(raw) > 8192 {
		return fmt.Errorf("%w: design context exceeds 8 KiB", ErrInvalid)
	}
	return nil
}

func (d *DesignContext) Clone() *DesignContext {
	if d == nil {
		return nil
	}
	result := *d
	result.Elements = slices.Clone(d.Elements)
	if result.Elements == nil {
		result.Elements = []DesignContextElement{}
	}
	return &result
}

func (d *DesignContext) Presentation(parts []Part) (*DesignContextPresentation, error) {
	if d == nil {
		return nil, nil //nolint:nilnil // An absent descriptor has no presentation.
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	contextIndex, screenshotIndex := -1, -1
	for i, part := range parts {
		if part.Type != "content" {
			continue
		}
		if part.ReferenceID == d.ContextAttachmentID {
			if contextIndex >= 0 {
				return nil, fmt.Errorf("%w: duplicate design context reference", ErrInvalid)
			}
			contextIndex = i
		}
		if d.ScreenshotAttachmentID != "" && part.ReferenceID == d.ScreenshotAttachmentID {
			if screenshotIndex >= 0 {
				return nil, fmt.Errorf("%w: duplicate design screenshot reference", ErrInvalid)
			}
			screenshotIndex = i
		}
	}
	if contextIndex < 0 || (d.ScreenshotAttachmentID != "" && screenshotIndex < 0) {
		return nil, fmt.Errorf("%w: design context evidence is missing", ErrInvalid)
	}
	result := &DesignContextPresentation{DesignContext: *d.Clone(), ContextPartIndex: contextIndex}
	if screenshotIndex >= 0 {
		result.ScreenshotPartIndex = new(screenshotIndex)
	}
	return result, nil
}
