package llm

import (
	"encoding/json"
	"errors"
	"slices"
	"unicode/utf8"
)

// DesignContextInput identifies uploaded evidence explicitly, never by filename.
// It is display-only metadata; the attachment bodies remain the model input.
type DesignContextInput struct {
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

// DesignContextPresentation indexes the serialized Message.content array,
// including its leading authored text, not the Go Message.Parts slice.
type DesignContextPresentation struct {
	DesignContextInput
	ContextPartIndex    int  `json:"context_part_index"`
	ScreenshotPartIndex *int `json:"screenshot_part_index,omitempty"`
}

func (d *DesignContextInput) Validate() error {
	if d == nil {
		return nil
	}
	bounded := func(value string, limit int) bool { return len(value) <= limit && utf8.ValidString(value) }
	if d.ContextAttachmentID == "" || !bounded(d.ContextAttachmentID, 128) || !bounded(d.ScreenshotAttachmentID, 128) ||
		d.ContextAttachmentID == d.ScreenshotAttachmentID || len(d.Elements) > 8 || d.ElementCount < len(d.Elements) || d.ElementCount > 1000 ||
		!bounded(d.PageURL, 2048) || !bounded(d.PageTitle, 256) {
		return errors.New("invalid design context descriptor or bounds")
	}
	for _, element := range d.Elements {
		if element.Label == "" || !bounded(element.Label, 160) || !bounded(element.Selector, 256) {
			return errors.New("invalid design context element summary")
		}
	}
	encoded, _ := json.Marshal(d)
	if len(encoded) > 8192 {
		return errors.New("design context descriptor exceeds 8 KiB")
	}
	return nil
}

func (d *DesignContextInput) Clone() *DesignContextInput {
	if d == nil {
		return nil
	}
	copy := *d
	copy.Elements = slices.Clone(d.Elements)
	if copy.Elements == nil {
		copy.Elements = []DesignContextElement{}
	}
	return &copy
}
