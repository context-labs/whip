package capability

import "encoding/json"

// BrowserScope is a server-resolved grant for one desktop browser attachment.
// Opaque identities and generations are never inferred from a model's arguments.
type BrowserScope struct {
	ProviderID           string               `json:"provider_id"`
	ProviderEpoch        string               `json:"provider_epoch"`
	TabID                string               `json:"tab_id"`
	TabGeneration        string               `json:"tab_generation"`
	ProfileID            string               `json:"profile_id"`
	AttachmentID         string               `json:"attachment_id,omitempty"`
	AttachmentGeneration string               `json:"attachment_generation,omitempty"`
	Rights               []string             `json:"rights"`
	Preview              *BrowserPreviewScope `json:"preview,omitempty"`
}

// BrowserPreviewScope binds preview access to an offered SSH environment.
// HostIdentity is the verified remote runtime ID, not an SSH key fingerprint.
type BrowserPreviewScope struct {
	HostID               string `json:"host_id"`
	HostIdentity         string `json:"host_identity"`
	ConnectionGeneration string `json:"connection_generation"`
	EnvironmentID        string `json:"environment_id"`
	Loopback             string `json:"loopback"`
	Ports                []int  `json:"ports"`
}

// BrowserCall is the immutable envelope admitted and hashed by the dispatcher.
// Grant and Scope are resolved by the trusted broker, not accepted from models.
type BrowserCall struct {
	Grant     Reference       `json:"grant"`
	Scope     BrowserScope    `json:"scope"`
	Arguments json.RawMessage `json:"arguments"`
}
