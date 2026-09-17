package browser

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/capability"
)

// DesktopIdentity is server-owned caller identity, never supplied by a model.
type DesktopIdentity struct {
	RootID  string
	AgentID string
}

// DesktopArguments is the model-visible lifecycle surface. Scope and grant
// references are resolved by the selected provider before durable admission.
type DesktopArguments struct {
	URL              string  `json:"url,omitempty"`
	PreviewHostID    string  `json:"preview_host_id,omitempty"`
	TabID            string  `json:"tab_id,omitempty"`
	AttachmentID     string  `json:"attachment_id,omitempty"`
	Code             string  `json:"code,omitempty"`
	ExpectedDocument string  `json:"expected_document,omitempty"`
	Timeout          float64 `json:"timeout,omitempty"`
	Port             int     `json:"port,omitempty"`
}

type DesktopError struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (e *DesktopError) Error() string { return e.Kind + ": " + e.Message }

type DesktopNetwork struct {
	Kind   string `json:"kind"`
	HostID string `json:"host_id,omitempty"`
	Ports  []int  `json:"ports"`
}

type DesktopResult struct {
	AttachmentID        string         `json:"attachment_id,omitempty"`
	TabID               string         `json:"tab_id,omitempty"`
	DocumentRevision    string         `json:"document_revision,omitempty"`
	URL                 string         `json:"url,omitempty"`
	Title               string         `json:"title,omitempty"`
	Network             DesktopNetwork `json:"network"`
	SupportedOperations []string       `json:"supported_operations"`
	Output              string         `json:"output,omitempty"`
	Media               []string       `json:"media"`
	Error               *DesktopError  `json:"error,omitempty"`
}

// DesktopRequest preserves the admitted operation identity throughout a batch.
type DesktopRequest struct {
	Identity    DesktopIdentity
	OperationID string
	Operation   string
	Call        capability.BrowserCall
}

// DesktopProvider is an existing-connection broker. Resolve must have no native
// resource effects. Execute revalidates the immutable resolved scope after
// permission, owns a whole-batch queue, and never falls back to another driver.
type DesktopProvider interface {
	Resolve(context.Context, DesktopIdentity, string, DesktopArguments) (capability.BrowserCall, error)
	CallContext(capability.BrowserCall) (context.Context, error)
	Execute(context.Context, DesktopRequest, func(context.Context, Backend) (string, error)) (DesktopResult, error)
	Transfer(context.Context, DesktopIdentity, DesktopIdentity, []string) ([]DesktopResult, error)
	Attachments(context.Context, DesktopIdentity) []DesktopResult
	RevokeAgent(context.Context, DesktopIdentity) error
}

// DecodeDesktopArguments accepts no authority-bearing caller fields.
func DecodeDesktopArguments(data json.RawMessage) (DesktopArguments, error) {
	var args DesktopArguments
	err := json.Unmarshal(data, &args)
	return args, err
}
