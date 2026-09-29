// Package browserhost owns connection-bound desktop offers and agent controls.
// Human pages and preview routes outlive revoked agent controls. Nothing in this
// package is restored from a checkpoint or retried on another connection.
package browserhost

import (
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/context-labs/whip/internal/session"
)

const (
	MaxPeers           = 32
	MaxBindings        = 256
	MaxPending         = 64
	MaxAttachments     = 8
	MaxRecords         = 256
	MaxScreenshotBytes = 4 << 20
	maxOutboundBytes   = 2 << 20
)

var (
	ErrClosed     = errors.New("browser connection is closed")
	ErrStale      = errors.New("browser authority is stale")
	ErrBusy       = errors.New("browser capacity reached")
	ErrEventStale = errors.New("browser event attachment is stale")
	ErrUnknown    = errors.New("browser effect outcome is uncertain; it will not be replayed")
)

// Failure is a bounded native rejection. Transport loss is ErrUnknown, never a
// fabricated native rejection. Messages are untrusted display text.
type Failure struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func (e *Failure) Error() string { return e.Kind + ": " + e.Message }
func validFailure(e *Failure) bool {
	if e == nil {
		return true
	}
	switch e.Kind {
	case "permission_denied", "desktop_unavailable", "host_not_connected", "browser_busy", "stale_document", "attachment_revoked", "tab_closed", "preview_disconnected", "unsupported_operation", "outcome_unknown":
		return textBound(e.Message, 4096)
	default:
		return false
	}
}

type Identity struct {
	RootID  string `json:"root_id"`
	AgentID string `json:"agent_id"`
}

func (v Identity) valid() bool { return token(v.RootID) && token(v.AgentID) }

type Preview = session.BrowserPreviewScope

type Scope = session.BrowserScope

func cloneScope(v Scope) Scope { v.Preview = clonePreview(v.Preview); return v }
func clonePreview(v *Preview) *Preview {
	if v == nil {
		return nil
	}
	n := *v
	n.Ports = slices.Clone(v.Ports)
	return &n
}

type OfferedTab struct {
	TabID            string   `json:"tab_id"`
	TabGeneration    string   `json:"tab_generation"`
	ProfileID        string   `json:"profile_id"`
	DocumentRevision string   `json:"document_revision"`
	URL              string   `json:"url"`
	Title            string   `json:"title"`
	Preview          *Preview `json:"preview,omitempty"`
}
type Offer struct {
	RootID                string       `json:"root_id"`
	Version               int          `json:"version"`
	DesktopID             string       `json:"desktop_id"`
	WindowID              string       `json:"window_id"`
	OfferRevision         string       `json:"offer_revision"`
	CreateProfileID       string       `json:"create_profile_id"`
	Availability          bool         `json:"availability"`
	ExpectedProviderEpoch string       `json:"expected_provider_epoch"`
	Tabs                  []OfferedTab `json:"tabs"`
	PreviewHosts          []Preview    `json:"preview_hosts"`
}
type Binding struct {
	Version       int    `json:"version"`
	ProviderID    string `json:"provider_id"`
	ProviderEpoch string `json:"provider_epoch"`
}
type Arguments struct {
	URL              string `json:"url,omitempty"`
	PreviewHostID    string `json:"preview_host_id,omitempty"`
	TabID            string `json:"tab_id,omitempty"`
	AttachmentID     string `json:"attachment_id,omitempty"`
	ExpectedDocument string `json:"expected_document,omitempty"`
	Port             int    `json:"port,omitempty"`
}
type Attachment struct {
	Scope            Scope    `json:"scope"`
	Owner            Identity `json:"owner"`
	DocumentRevision string   `json:"document_revision"`
	URL              string   `json:"url"`
	Title            string   `json:"title"`
	Delegated        bool     `json:"delegated"`
}

func cloneAttachment(a Attachment) Attachment { a.Scope = cloneScope(a.Scope); return a }

type Tab struct {
	TabID            string `json:"tab_id"`
	TabGeneration    string `json:"tab_generation"`
	DocumentRevision string `json:"document_revision"`
	URL              string `json:"url"`
	Title            string `json:"title"`
	State            string `json:"state"`
	Requestable      bool   `json:"requestable"`
	AttachmentID     string `json:"attachment_id,omitempty"`
}
type InventoryTarget struct {
	TabID         string `json:"tab_id"`
	TabGeneration string `json:"tab_generation"`
}
type InventoryRequest struct {
	RequestID string            `json:"request_id"`
	Identity  Identity          `json:"identity"`
	Binding   Binding           `json:"binding"`
	Tabs      []InventoryTarget `json:"tabs"`
}
type InventoryResult struct {
	RequestID     string   `json:"request_id"`
	RootID        string   `json:"root_id"`
	ProviderEpoch string   `json:"provider_epoch"`
	Tabs          []Tab    `json:"tabs"`
	Error         *Failure `json:"error,omitempty"`
}
type Command struct {
	CommandID        string          `json:"command_id"`
	OperationID      string          `json:"operation_id"`
	Identity         Identity        `json:"identity"`
	Scope            Scope           `json:"scope"`
	ExpectedDocument string          `json:"expected_document"`
	DeadlineMillis   int64           `json:"deadline_millis,string"`
	Kind             string          `json:"kind"`
	Arguments        json.RawMessage `json:"arguments"`
}
type CommandResult struct {
	CommandID            string          `json:"command_id"`
	RootID               string          `json:"root_id"`
	ProviderEpoch        string          `json:"provider_epoch"`
	AttachmentGeneration string          `json:"attachment_generation"`
	DocumentRevision     string          `json:"document_revision"`
	URL                  string          `json:"url"`
	Title                string          `json:"title"`
	Result               json.RawMessage `json:"result,omitempty"`
	Error                *Failure        `json:"error,omitempty"`
	// Screenshot must match the bytes uploaded to this exact pending command.
	Screenshot *Screenshot `json:"screenshot,omitempty"`
}
type Screenshot struct {
	Size      int    `json:"size"`
	Digest    string `json:"digest"`
	MediaType string `json:"media_type"`
}
type Reply struct {
	CommandResult
	Image []byte
}
type Cancel struct {
	CommandID            string `json:"command_id"`
	RootID               string `json:"root_id"`
	ProviderEpoch        string `json:"provider_epoch"`
	AttachmentGeneration string `json:"attachment_generation"`
}
type Revoked struct {
	RootID  string  `json:"root_id"`
	Binding Binding `json:"binding"`
	Reason  string  `json:"reason"`
}

// Notification has exactly one body. The RPC owner serializes it and owns the
// sole writer; this package has no sockets or reconnect loop.
type Retired struct {
	RootID  string  `json:"root_id"`
	Binding Binding `json:"binding"`
	Scopes  []Scope `json:"scopes"`
}

type Notification struct {
	Command   *Command          `json:"command,omitempty"`
	Inventory *InventoryRequest `json:"inventory,omitempty"`
	Cancel    *Cancel           `json:"cancel,omitempty"`
	Revoked   *Revoked          `json:"revoked,omitempty"`
	Retired   *Retired          `json:"retired,omitempty"`
}
type Event struct {
	RootID               string          `json:"root_id"`
	ProviderEpoch        string          `json:"provider_epoch"`
	TabID                string          `json:"tab_id"`
	TabGeneration        string          `json:"tab_generation"`
	AttachmentID         string          `json:"attachment_id"`
	AttachmentGeneration string          `json:"attachment_generation"`
	Sequence             uint64          `json:"sequence,string"`
	OperationID          string          `json:"operation_id"`
	DocumentRevision     string          `json:"document_revision"`
	Kind                 string          `json:"kind"`
	Method               string          `json:"method"`
	Params               json.RawMessage `json:"params,omitempty"`
	URL                  string          `json:"url"`
	Title                string          `json:"title"`
}
type TransferPair struct {
	Parent Scope `json:"parent_scope"`
	Child  Scope `json:"child_scope"`
}
type TransferArguments struct {
	ChildAgentID string         `json:"child_agent_id"`
	Attachments  []TransferPair `json:"attachments"`
}

func token(s string) bool {
	return s != "" && len(s) <= 128 && strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) < 0 && utf8.ValidString(s)
}

func textBound(s string, n int) bool {
	return len(s) <= n && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func cleanTitle(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, s)
}

func cleanURL(s string) string {
	u, e := url.Parse(s)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	u.User = nil
	out := cleanTitle(u.String())
	if len(out) > 8192 {
		return ""
	}
	return out
}

func validatePreview(p Preview) error {
	if !token(p.HostID) || !token(p.HostIdentity) || !token(p.ConnectionGeneration) || !token(p.EnvironmentID) || (p.Loopback != "127.0.0.1" && p.Loopback != "::1") || len(p.Ports) > 64 {
		return errors.New("invalid verified preview scope")
	}
	previous := 0
	for _, port := range p.Ports {
		if port <= previous || port > 65535 {
			return errors.New("preview ports must be sorted and unique")
		}
		previous = port
	}
	return nil
}

func previewPort(raw, loopback string) (int, error) {
	u, e := url.Parse(raw)
	if e != nil || !textBound(raw, 8192) || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() != loopback {
		return 0, errors.New("preview URL must use its offered literal loopback without credentials")
	}
	port := 80
	if u.Scheme == "https" {
		port = 443
	}
	if u.Port() != "" {
		port, e = strconv.Atoi(u.Port())
	}
	if e != nil || port < 1 || port > 65535 {
		return 0, errors.New("invalid preview port")
	}
	return port, nil
}

func validateOffer(o Offer) error {
	if (o.Version != 1 && o.Version != 2) || !token(o.RootID) || !token(o.DesktopID) || !token(o.WindowID) || !token(o.OfferRevision) || !token(o.CreateProfileID) || (o.ExpectedProviderEpoch != "" && !token(o.ExpectedProviderEpoch)) || len(o.Tabs) > 32 || len(o.PreviewHosts) > 16 {
		return errors.New("invalid or oversized browser offer")
	}
	if o.Availability && (o.Version != 2 || len(o.Tabs) != 0 || len(o.PreviewHosts) != 0) {
		return errors.New("availability is an inert v2 create-only offer")
	}
	seen := map[string]bool{}
	for _, t := range o.Tabs {
		if !token(t.TabID) || !token(t.TabGeneration) || !token(t.ProfileID) || seen[t.TabID] || !textBound(t.DocumentRevision, 128) || !textBound(t.URL, 8192) || !textBound(t.Title, 512) {
			return errors.New("invalid or duplicate offered tab")
		}
		seen[t.TabID] = true
		if t.Preview != nil {
			if e := validatePreview(*t.Preview); e != nil {
				return e
			}
		}
	}
	clear(seen)
	for _, p := range o.PreviewHosts {
		if seen[p.HostID] {
			return errors.New("duplicate preview host")
		}
		seen[p.HostID] = true
		if e := validatePreview(p); e != nil {
			return e
		}
	}
	return nil
}

func cloneOffer(o Offer) Offer {
	o.Tabs = slices.Clone(o.Tabs)
	for i := range o.Tabs {
		o.Tabs[i].Preview = clonePreview(o.Tabs[i].Preview)
		o.Tabs[i].URL = cleanURL(o.Tabs[i].URL)
		o.Tabs[i].Title = cleanTitle(o.Tabs[i].Title)
	}
	o.PreviewHosts = slices.Clone(o.PreviewHosts)
	for i := range o.PreviewHosts {
		o.PreviewHosts[i].Ports = slices.Clone(o.PreviewHosts[i].Ports)
	}
	return o
}

func validateArguments(operation string, a Arguments) error {
	invalid := errors.New("browser arguments do not match the selected operation")
	switch operation {
	case "open":
		u, e := url.Parse(a.URL)
		if e != nil || (a.URL != "about:blank" && (u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "")) || a.TabID != "" || a.AttachmentID != "" || a.Port != 0 || a.ExpectedDocument != "" {
			return invalid
		}
	case "attach":
		if a.TabID == "" || a.AttachmentID != "" || a.URL != "" || a.PreviewHostID != "" || a.Port != 0 || a.ExpectedDocument != "" {
			return invalid
		}
	case "run":
		if a.AttachmentID == "" || a.TabID != "" || a.URL != "" || a.PreviewHostID != "" || a.Port != 0 {
			return invalid
		}
	case "detach":
		if a.AttachmentID == "" || a.TabID != "" || a.URL != "" || a.PreviewHostID != "" || a.Port != 0 || a.ExpectedDocument != "" {
			return invalid
		}
	case "allow_preview_port":
		if a.AttachmentID == "" || a.TabID != "" || a.URL != "" || a.PreviewHostID != "" || a.Port < 1 || a.Port > 65535 || a.ExpectedDocument != "" {
			return invalid
		}
	default:
		return invalid
	}
	return nil
}
