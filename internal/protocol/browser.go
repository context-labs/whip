package protocol

import (
	"encoding/json"
	"github.com/context-labs/whip/internal/browser"
	"github.com/context-labs/whip/internal/capability"
)

// BrowserProviderBindParams records an explicit user-selected root association.
// Advertising willingness during initialize never implicitly selects a provider.
type BrowserProviderBindParams struct {
	Availability          bool                             `json:"availability,omitempty"`
	RootID                string                           `json:"root_id"`
	Version               int                              `json:"version"`
	DesktopID             string                           `json:"desktop_id"`
	WindowID              string                           `json:"window_id"`
	OfferRevision         string                           `json:"offer_revision"`
	CreateProfileID       string                           `json:"create_profile_id"`
	ExpectedProviderEpoch string                           `json:"expected_provider_epoch,omitempty"`
	OfferedTabs           []BrowserOfferedTab              `json:"offered_tabs"`
	OfferedPreviewHosts   []capability.BrowserPreviewScope `json:"offered_preview_hosts"`
}
type BrowserOfferedTab struct {
	TabID            string                          `json:"tab_id"`
	TabGeneration    string                          `json:"tab_generation"`
	ProfileID        string                          `json:"profile_id"`
	DocumentRevision string                          `json:"document_revision,omitempty"`
	URL              string                          `json:"url,omitempty"`
	Title            string                          `json:"title,omitempty"`
	Preview          *capability.BrowserPreviewScope `json:"preview,omitempty"`
}
type BrowserProviderBindResult struct {
	Version       int    `json:"version"`
	ProviderID    string `json:"provider_id"`
	ProviderEpoch string `json:"provider_epoch"`
}

// BrowserProviderUnbindParams releases only the current lease on its owning connection.
// A retained, explicitly released epoch is idempotent only on its original
// connection; unrelated stale epochs are rejected without affecting replacements.
type BrowserProviderUnbindParams struct {
	RootID        string `json:"root_id"`
	ProviderEpoch string `json:"provider_epoch"`
}

// BrowserProviderRevoked is an independent teardown notification, including for
// idle providers. Consumers must match both provider identity and epoch.
type BrowserProviderRevoked struct {
	RootID        string `json:"root_id"`
	ProviderID    string `json:"provider_id"`
	ProviderEpoch string `json:"provider_epoch"`
	Reason        string `json:"reason"`
}

// BrowserCommand is delivered only to the selected authenticated connection.
type BrowserCommand struct {
	CommandID        string                  `json:"command_id"`
	OperationID      string                  `json:"operation_id"`
	RootID           string                  `json:"root_id"`
	AgentID          string                  `json:"agent_id"`
	ProviderEpoch    string                  `json:"provider_epoch"`
	Scope            capability.BrowserScope `json:"scope"`
	ExpectedDocument string                  `json:"expected_document,omitempty"`
	DeadlineMillis   int64                   `json:"deadline_millis,string"`
	Kind             string                  `json:"kind"`
	Arguments        json.RawMessage         `json:"arguments"`
}
type BrowserCommandResultParams struct {
	CommandID            string                `json:"command_id"`
	RootID               string                `json:"root_id"`
	ProviderEpoch        string                `json:"provider_epoch"`
	AttachmentGeneration string                `json:"attachment_generation"`
	DocumentRevision     string                `json:"document_revision"`
	Result               json.RawMessage       `json:"result,omitempty"`
	Error                *browser.DesktopError `json:"error,omitempty"`
	Screenshot           *ContentHandle        `json:"screenshot,omitempty"`
}
type BrowserCommandCancel struct {
	CommandID            string `json:"command_id"`
	RootID               string `json:"root_id"`
	ProviderEpoch        string `json:"provider_epoch"`
	AttachmentGeneration string `json:"attachment_generation"`
	Reason               string `json:"reason"`
}

// BrowserProviderEventParams is observation only. It cannot create authority.
// Sequence is per attachment and starts at one; a gap invalidates that handle.
type BrowserProviderEventParams struct {
	RootID               string          `json:"root_id"`
	ProviderEpoch        string          `json:"provider_epoch"`
	TabID                string          `json:"tab_id"`
	TabGeneration        string          `json:"tab_generation"`
	AttachmentID         string          `json:"attachment_id"`
	AttachmentGeneration string          `json:"attachment_generation"`
	Sequence             uint64          `json:"sequence,string"`
	OperationID          string          `json:"operation_id,omitempty"`
	DocumentRevision     string          `json:"document_revision"`
	Kind                 string          `json:"kind"`
	Method               string          `json:"method,omitempty"`
	Params               json.RawMessage `json:"params,omitempty"`
	URL                  string          `json:"url,omitempty"`
	Title                string          `json:"title,omitempty"`
}

// BrowserInventoryRequest reads only the listed metadata on an exact provider.
// It grants neither page control nor authority to enumerate the whole window.
type BrowserInventoryRequest struct {
	RequestID     string                   `json:"request_id"`
	RootID        string                   `json:"root_id"`
	AgentID       string                   `json:"agent_id"`
	ProviderID    string                   `json:"provider_id"`
	ProviderEpoch string                   `json:"provider_epoch"`
	Tabs          []BrowserInventoryTarget `json:"tabs"`
}
type BrowserInventoryTarget struct {
	TabID         string `json:"tab_id"`
	TabGeneration string `json:"tab_generation"`
}
type BrowserInventoryResultParams struct {
	RequestID     string                `json:"request_id"`
	RootID        string                `json:"root_id"`
	ProviderEpoch string                `json:"provider_epoch"`
	Tabs          []browser.DesktopTab  `json:"tabs"`
	Error         *browser.DesktopError `json:"error,omitempty"`
}

// BrowserTransferArguments requests an all-or-nothing native handoff.
type BrowserTransferArguments struct {
	ChildAgentID string                      `json:"child_agent_id"`
	Attachments  []BrowserTransferAttachment `json:"attachments"`
}
type BrowserTransferAttachment struct {
	ParentScope capability.BrowserScope `json:"parent_scope"`
	ChildScope  capability.BrowserScope `json:"child_scope"`
}
