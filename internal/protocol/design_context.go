package protocol

import "github.com/context-labs/whip/internal/session"

type DesignContext struct {
	ContextAttachmentID    ID                     `json:"context_attachment_id"`
	ScreenshotAttachmentID *ID                    `json:"screenshot_attachment_id,omitempty"`
	Elements               []DesignContextElement `json:"elements" max:"8"`
	ElementCount           int                    `json:"element_count" min:"0" max:"1000"`
	PageURL                string                 `json:"page_url,omitempty" max:"2048"`
	PageTitle              string                 `json:"page_title,omitempty" max:"256"`
}

type DesignContextElement struct {
	Label    string `json:"label" min:"1" max:"160"`
	Selector string `json:"selector,omitempty" max:"256"`
}

type DesignContextPresentation struct {
	DesignContext
	ContextPartIndex    int  `json:"context_part_index" min:"0"`
	ScreenshotPartIndex *int `json:"screenshot_part_index,omitempty" min:"0"`
}

func (d *DesignContext) Domain() *session.DesignContext {
	if d == nil {
		return nil
	}
	result := &session.DesignContext{ContextAttachmentID: string(d.ContextAttachmentID), ElementCount: d.ElementCount, PageURL: d.PageURL, PageTitle: d.PageTitle, Elements: []session.DesignContextElement{}}
	if d.ScreenshotAttachmentID != nil {
		result.ScreenshotAttachmentID = string(*d.ScreenshotAttachmentID)
	}
	for _, element := range d.Elements {
		result.Elements = append(result.Elements, session.DesignContextElement{Label: element.Label, Selector: element.Selector})
	}
	return result
}

func designContextFromDomain(d *session.DesignContext) *DesignContext {
	if d == nil {
		return nil
	}
	result := &DesignContext{ContextAttachmentID: ID(d.ContextAttachmentID), ElementCount: d.ElementCount, PageURL: d.PageURL, PageTitle: d.PageTitle, Elements: []DesignContextElement{}}
	if d.ScreenshotAttachmentID != "" {
		result.ScreenshotAttachmentID = new(ID(d.ScreenshotAttachmentID))
	}
	for _, element := range d.Elements {
		result.Elements = append(result.Elements, DesignContextElement{Label: element.Label, Selector: element.Selector})
	}
	return result
}

func designPresentationFromDomain(d *session.DesignContextPresentation) *DesignContextPresentation {
	if d == nil {
		return nil
	}
	return &DesignContextPresentation{DesignContext: *designContextFromDomain(&d.DesignContext), ContextPartIndex: d.ContextPartIndex, ScreenshotPartIndex: d.ScreenshotPartIndex}
}
