package protocol

import (
	"encoding/json"
	"reflect"

	"github.com/google/jsonschema-go/jsonschema"
)

// BrowserToken is a bounded opaque native identity, not a session ID or URL.
type BrowserToken string

type BrowserPreviewScope struct {
	HostID               BrowserToken `json:"host_id"`
	HostIdentity         BrowserToken `json:"host_identity"`
	ConnectionGeneration BrowserToken `json:"connection_generation"`
	EnvironmentID        BrowserToken `json:"environment_id"`
	Loopback             string       `json:"loopback" enum:"127.0.0.1,::1"`
	Ports                []int        `json:"ports"`
}
type BrowserScope struct {
	ProviderID           BrowserToken         `json:"provider_id"`
	ProviderEpoch        BrowserToken         `json:"provider_epoch"`
	TabID                BrowserToken         `json:"tab_id"`
	TabGeneration        BrowserToken         `json:"tab_generation"`
	ProfileID            BrowserToken         `json:"profile_id"`
	ControlLineage       BrowserToken         `json:"control_lineage"`
	AttachmentID         BrowserToken         `json:"attachment_id"`
	AttachmentGeneration BrowserToken         `json:"attachment_generation"`
	Preview              *BrowserPreviewScope `json:"preview,omitempty"`
}
type BrowserOfferedTab struct {
	TabID            BrowserToken         `json:"tab_id"`
	TabGeneration    BrowserToken         `json:"tab_generation"`
	ProfileID        BrowserToken         `json:"profile_id"`
	DocumentRevision string               `json:"document_revision"`
	URL              string               `json:"url"`
	Title            string               `json:"title"`
	Preview          *BrowserPreviewScope `json:"preview,omitempty"`
}
type BrowserProviderBindParams struct {
	RootID                ID                    `json:"root_id"`
	Version               int                   `json:"version" min:"1" max:"2"`
	DesktopID             BrowserToken          `json:"desktop_id"`
	WindowID              BrowserToken          `json:"window_id"`
	OfferRevision         BrowserToken          `json:"offer_revision"`
	CreateProfileID       BrowserToken          `json:"create_profile_id"`
	Availability          bool                  `json:"availability,omitempty"`
	ExpectedProviderEpoch *BrowserToken         `json:"expected_provider_epoch,omitempty"`
	OfferedTabs           []BrowserOfferedTab   `json:"offered_tabs"`
	OfferedPreviewHosts   []BrowserPreviewScope `json:"offered_preview_hosts"`
}
type BrowserProviderBindResult struct {
	Version       int          `json:"version" min:"1" max:"2"`
	ProviderID    BrowserToken `json:"provider_id"`
	ProviderEpoch BrowserToken `json:"provider_epoch"`
}
type BrowserProviderUnbindParams struct {
	RootID        ID           `json:"root_id"`
	ProviderEpoch BrowserToken `json:"provider_epoch"`
}
type BrowserAccepted struct {
	Accepted bool `json:"accepted"`
}
type BrowserFailure struct {
	Kind    string `json:"kind" enum:"permission_denied,desktop_unavailable,host_not_connected,browser_busy,stale_document,attachment_revoked,tab_closed,preview_disconnected,unsupported_operation,outcome_unknown"`
	Message string `json:"message"`
}
type BrowserTab struct {
	TabID            BrowserToken  `json:"tab_id"`
	TabGeneration    BrowserToken  `json:"tab_generation"`
	DocumentRevision string        `json:"document_revision"`
	URL              string        `json:"url"`
	Title            string        `json:"title"`
	State            string        `json:"state" enum:"available,busy,attached"`
	Requestable      bool          `json:"requestable"`
	AttachmentID     *BrowserToken `json:"attachment_id,omitempty"`
}
type BrowserTabsResult struct {
	Tabs []BrowserTab `json:"tabs"`
}
type BrowserAttachment struct {
	Scope            BrowserScope `json:"scope"`
	RootID           ID           `json:"root_id"`
	AgentID          ID           `json:"agent_id"`
	DocumentRevision string       `json:"document_revision"`
	URL              string       `json:"url"`
	Title            string       `json:"title"`
}
type BrowserAttachmentsResult struct {
	Attachments []BrowserAttachment `json:"attachments"`
}
type BrowserInventoryTarget struct {
	TabID         BrowserToken `json:"tab_id"`
	TabGeneration BrowserToken `json:"tab_generation"`
}
type BrowserInventoryRequest struct {
	RequestID     BrowserToken             `json:"request_id"`
	RootID        ID                       `json:"root_id"`
	AgentID       ID                       `json:"agent_id"`
	ProviderID    BrowserToken             `json:"provider_id"`
	ProviderEpoch BrowserToken             `json:"provider_epoch"`
	Tabs          []BrowserInventoryTarget `json:"tabs"`
}
type BrowserInventoryResultParams struct {
	RequestID     BrowserToken    `json:"request_id"`
	RootID        ID              `json:"root_id"`
	ProviderEpoch BrowserToken    `json:"provider_epoch"`
	Tabs          []BrowserTab    `json:"tabs"`
	Error         *BrowserFailure `json:"error,omitempty"`
}

// CDP arguments and results are native JSON data. Identity, ordering, deadlines
// and byte counters remain explicit decimal strings independently of that data.
type BrowserCommand struct {
	CommandID        BrowserToken    `json:"command_id"`
	OperationID      ID              `json:"operation_id"`
	RootID           ID              `json:"root_id"`
	AgentID          ID              `json:"agent_id"`
	ProviderEpoch    BrowserToken    `json:"provider_epoch"`
	Scope            BrowserScope    `json:"scope"`
	ExpectedDocument string          `json:"expected_document"`
	DeadlineMillis   Counter         `json:"deadline_millis"`
	Kind             string          `json:"kind" enum:"open,attach,allow_preview_port,detach,begin,cdp,end,transfer"`
	Arguments        json.RawMessage `json:"arguments"`
}
type BrowserScreenshot struct {
	Size      Counter `json:"size"`
	Digest    string  `json:"digest" pattern:"^[a-f0-9]{64}$"`
	MediaType string  `json:"media_type" enum:"image/jpeg"`
}
type BrowserCommandResultParams struct {
	CommandID            BrowserToken       `json:"command_id"`
	RootID               ID                 `json:"root_id"`
	ProviderEpoch        BrowserToken       `json:"provider_epoch"`
	AttachmentGeneration BrowserToken       `json:"attachment_generation"`
	DocumentRevision     string             `json:"document_revision"`
	URL                  string             `json:"url"`
	Title                string             `json:"title"`
	Result               json.RawMessage    `json:"result,omitempty"`
	Error                *BrowserFailure    `json:"error,omitempty"`
	Screenshot           *BrowserScreenshot `json:"screenshot,omitempty"`
}

// Uploads are confined to this peer's exact pending screenshot command. They
// cannot publish arbitrary content or authorize another operation's bytes.
type BrowserScreenshotChunkParams struct {
	CommandID            BrowserToken `json:"command_id"`
	RootID               ID           `json:"root_id"`
	ProviderEpoch        BrowserToken `json:"provider_epoch"`
	AttachmentGeneration BrowserToken `json:"attachment_generation"`
	Offset               Counter      `json:"offset"`
	DataBase64           string       `json:"data_base64"`
}
type BrowserCommandCancel struct {
	CommandID            BrowserToken `json:"command_id"`
	RootID               ID           `json:"root_id"`
	ProviderEpoch        BrowserToken `json:"provider_epoch"`
	AttachmentGeneration BrowserToken `json:"attachment_generation"`
}
type BrowserProviderRevoked struct {
	RootID        ID           `json:"root_id"`
	ProviderID    BrowserToken `json:"provider_id"`
	ProviderEpoch BrowserToken `json:"provider_epoch"`
	Reason        string       `json:"reason"`
}
type BrowserScopesRetired struct {
	RootID        ID             `json:"root_id"`
	ProviderID    BrowserToken   `json:"provider_id"`
	ProviderEpoch BrowserToken   `json:"provider_epoch"`
	Scopes        []BrowserScope `json:"scopes"`
}
type BrowserProviderEventParams struct {
	RootID               ID              `json:"root_id"`
	ProviderEpoch        BrowserToken    `json:"provider_epoch"`
	TabID                BrowserToken    `json:"tab_id"`
	TabGeneration        BrowserToken    `json:"tab_generation"`
	AttachmentID         BrowserToken    `json:"attachment_id"`
	AttachmentGeneration BrowserToken    `json:"attachment_generation"`
	Sequence             Counter         `json:"sequence"`
	OperationID          *ID             `json:"operation_id,omitempty"`
	DocumentRevision     string          `json:"document_revision"`
	Kind                 string          `json:"kind" enum:"cdp,state,document,closed,revoked,preview_disconnected"`
	Method               string          `json:"method,omitempty"`
	Params               json.RawMessage `json:"params,omitempty"`
	URL                  string          `json:"url,omitempty"`
	Title                string          `json:"title,omitempty"`
}
type BrowserEvent struct {
	JSONRPC   string                   `json:"jsonrpc" enum:"2.0"`
	Method    string                   `json:"method" enum:"browser.command,browser.inventory,browser.command.cancel,browser.provider.revoked,browser.scopes.retired"`
	Command   *BrowserCommand          `json:"command"`
	Inventory *BrowserInventoryRequest `json:"inventory"`
	Cancel    *BrowserCommandCancel    `json:"cancel"`
	Revoked   *BrowserProviderRevoked  `json:"revoked"`
	Retired   *BrowserScopesRetired    `json:"retired"`
}

func browserSchema(schema *jsonschema.Schema, t reflect.Type) {
	if t.PkgPath() != reflect.TypeFor[BrowserEvent]().PkgPath() {
		return
	}
	switch t {
	case reflect.TypeFor[BrowserProviderBindParams]():
		browserArray(schema, "offered_tabs", 32)
		browserArray(schema, "offered_preview_hosts", 16)
	case reflect.TypeFor[BrowserPreviewScope]():
		browserArray(schema, "ports", 64)
		schema.Properties["ports"].UniqueItems = true
		schema.Properties["ports"].Items.Minimum = new(float64(1))
		schema.Properties["ports"].Items.Maximum = new(float64(65535))
	case reflect.TypeFor[BrowserInventoryRequest](), reflect.TypeFor[BrowserInventoryResultParams](), reflect.TypeFor[BrowserTabsResult]():
		browserArray(schema, "tabs", 72)
	case reflect.TypeFor[BrowserAttachmentsResult]():
		browserArray(schema, "attachments", 8)
	case reflect.TypeFor[BrowserScopesRetired]():
		browserArray(schema, "scopes", 256)
	case reflect.TypeFor[BrowserFailure]():
		schema.Properties["message"].MaxLength = new(4096)
	case reflect.TypeFor[BrowserProviderRevoked]():
		schema.Properties["reason"].MaxLength = new(128)
	case reflect.TypeFor[BrowserScreenshotChunkParams]():
		schema.Properties["data_base64"].MaxLength = new(87384)
		schema.Properties["data_base64"].MinLength = new(4)
	case reflect.TypeFor[BrowserCommand]():
		schema.Properties["expected_document"].MaxLength = new(128)
	case reflect.TypeFor[BrowserEvent]():
		names := []string{"command", "inventory", "cancel", "revoked", "retired"}
		methods := []string{"browser.command", "browser.inventory", "browser.command.cancel", "browser.provider.revoked", "browser.scopes.retired"}
		for i, name := range names {
			variant := &jsonschema.Schema{Properties: map[string]*jsonschema.Schema{"method": {Enum: []any{methods[i]}}}}
			for _, field := range names {
				variant.Properties[field] = &jsonschema.Schema{Type: "null"}
			}
			variant.Properties[name] = &jsonschema.Schema{Not: &jsonschema.Schema{Type: "null"}}
			schema.OneOf = append(schema.OneOf, variant)
		}
	}
	switch t {
	case reflect.TypeFor[BrowserOfferedTab](), reflect.TypeFor[BrowserTab](), reflect.TypeFor[BrowserAttachment](), reflect.TypeFor[BrowserCommandResultParams](), reflect.TypeFor[BrowserProviderEventParams]():
		for name, limit := range map[string]int{"url": 8192, "title": 512, "document_revision": 128, "method": 256} {
			if field := schema.Properties[name]; field != nil {
				field.MaxLength = new(limit)
			}
		}
	}
}

func browserArray(schema *jsonschema.Schema, name string, limit int) {
	f := schema.Properties[name]
	f.Type, f.Types, f.MaxItems = "array", nil, new(limit)
}
