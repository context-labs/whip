// Package protocol defines the transport-independent WHIP client contract.
package protocol

import (
	"encoding/json"
	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
	"time"
)

const Major = 3
const Minor = 0

type ErrorData struct {
	Kind string `json:"kind"`
}

type RPCError struct {
	Data    *ErrorData `json:"data,omitempty"`
	Code    int        `json:"code"`
	Message string     `json:"message"`
}

type InitializeParams struct {
	ProtocolMajor int              `json:"protocol_major"`
	BuildID       string           `json:"build_id"`
	ClientKind    string           `json:"client_kind"`
	ClientID      string           `json:"client_id"`
	Capabilities  []string         `json:"capabilities,omitempty"`
	Cursors       map[string]int64 `json:"-"`
}

type ProtocolLimits struct {
	FrameBytes        int   `json:"frame_bytes"`
	Connections       int   `json:"connections"`
	InFlightRequests  int   `json:"in_flight_requests"`
	OutboundMessages  int   `json:"outbound_messages"`
	OutboundBytes     int64 `json:"outbound_bytes,string"`
	RootSubscriptions int   `json:"root_subscriptions"`
	ContentChunkBytes int   `json:"content_chunk_bytes"`
	UploadBytes       int64 `json:"upload_bytes,string"`
}

type InitializeResult struct {
	Operations             []Operation    `json:"operations"`
	Limits                 ProtocolLimits `json:"limits"`
	NegotiatedCapabilities []string       `json:"negotiated_capabilities"`
	ProtocolMinor          int            `json:"protocol_minor"`
	RuntimeID              string         `json:"runtime_id"`
	ConnectionID           string         `json:"connection_id"`
	HostPlatform           string         `json:"host_platform"`
	HostArchitecture       string         `json:"host_architecture"`
	NetworkEndpoint        string         `json:"network_endpoint,omitempty"`
	ProtocolMajor          int            `json:"protocol_major"`
	BuildID                string         `json:"build_id"`
	Generation             int64          `json:"generation,string"`
	PID                    int            `json:"pid,omitempty"`
	StartedAt              string         `json:"started_at,omitempty"`
	Capabilities           []string       `json:"capabilities"`
}

type CommandParams struct {
	CommandID string          `json:"command_id"`
	Scope     string          `json:"scope"`
	RootID    string          `json:"root_id,omitempty"`
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type CommandStatusParams struct {
	CommandID string `json:"command_id"`
}

type CommandResult struct {
	Content    *ContentHandle  `json:"content,omitempty"`
	Operation  string          `json:"operation"`
	Result     json.RawMessage `json:"result,omitempty"`
	Failure    *RPCError       `json:"failure,omitempty"`
	CommandID  string          `json:"command_id"`
	IngressSeq int64           `json:"ingress_seq,string"`
	Status     string          `json:"status"`
	Output     string          `json:"-"`
	Error      string          `json:"-"`
}

type ReplayParams struct {
	RootID string `json:"root_id"`
	Cursor int64  `json:"cursor,string"`
	Limit  int    `json:"limit,omitempty"`
}

type ProtocolEvent struct {
	SubscriptionID string          `json:"subscription_id,omitempty"`
	RootID         string          `json:"root_id"`
	Seq            int64           `json:"seq,string"`
	Kind           string          `json:"kind"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

type StreamEvent struct {
	Usage   *UsageEvent `json:"usage,omitempty"`
	AgentID string      `json:"agent_id,omitempty"`
	ID      string      `json:"id,omitempty"`
	Name    string      `json:"name,omitempty"`
	Text    string      `json:"text,omitempty"`
	Args    string      `json:"args,omitempty"`
	Result  string      `json:"result,omitempty"`
}

type SubmitPayload struct {
	Text        string            `json:"text"`
	Parts       []llm.ContentPart `json:"parts,omitempty"`
	Attachments []InputAttachment `json:"attachments,omitempty"`
}

// InputAttachment keeps uploaded bodies out of request frames and command
// payloads. The daemon resolves this exact scoped content identity on its worker.
type InputAttachment struct {
	Kind    string        `json:"kind"`
	Content ContentHandle `json:"content"`
	Name    string        `json:"name,omitempty"`
}

type UsageEvent struct {
	Used  int       `json:"used"`
	Size  int       `json:"size"`
	Usage llm.Usage `json:"usage"`
}

type PlanItem struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

type PlanEvent struct {
	Items []PlanItem `json:"items"`
}

type AgentTranscriptResult struct {
	Cursor       int64                         `json:"cursor,string"`
	Agent        session.RuntimeAgent          `json:"agent"`
	Page         session.BoundedTranscriptPage `json:"page"`
	Presentation []session.SnapshotEvent       `json:"presentation,omitempty"`
	Inbox        []session.InboxItem           `json:"inbox,omitempty"`
}

type AgentSubmitResult struct {
	AgentID  string `json:"agent_id"`
	InboxSeq int64  `json:"inbox_seq,string"`
	Kind     string `json:"kind,omitempty"` // submit or steer
	Status   string `json:"status"`
}

type SessionPreviewResult struct {
	RootID    string `json:"root_id"`
	User      string `json:"user"`
	Assistant string `json:"assistant"`
}

type SessionUpdateEvent struct {
	Title         string `json:"title,omitempty"`
	Model         string `json:"model,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Effort        string `json:"effort,omitempty"`
	EffortChanged bool   `json:"effort_changed,omitempty"`
	WorkingDir    string `json:"working_directory,omitempty"`
}

type ProviderValidateParams struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Key     string `json:"key"`
}

type ProviderValidateResult struct {
	Models []llm.ModelInfo `json:"models"`
}

type ProviderDescriptor struct {
	BaseURL string `json:"base_url"`
}

type ModelDescriptor struct {
	Name      string   `json:"name,omitempty"`
	ID        string   `json:"id,omitempty"`
	Providers []string `json:"providers"`
	Context   int      `json:"context,omitempty"`
	Vision    bool     `json:"vision,omitempty"`
}

type ProviderCatalogsResult struct {
	Models    map[string]ModelDescriptor    `json:"models"`
	Providers map[string]ProviderDescriptor `json:"providers"`
	Catalogs  map[string]config.Catalog     `json:"catalogs"`
	Errors    map[string]string             `json:"errors,omitempty"`
}

type ContextAuditRow struct {
	Label string `json:"label"`
	Bytes int    `json:"bytes,omitempty"`
	Note  string `json:"note,omitempty"`
}

type ContextAuditResult struct {
	WorkingDirectory string            `json:"working_directory"`
	Rows             []ContextAuditRow `json:"rows"`
}

type MCPStatusResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
	Error  string `json:"error,omitempty"`
	Tools  int    `json:"tools,omitempty"`
	Source string `json:"source,omitempty"`
}

type LSPStatusResult struct {
	Name  string `json:"name"`
	Root  string `json:"root,omitempty"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

type ReplayResult struct {
	Events  []ProtocolEvent `json:"events"`
	Latest  int64           `json:"latest,string"`
	Expired bool            `json:"expired,omitempty"`
}

type SnapshotParams struct {
	RootID string `json:"root_id"`
}

type UploadBeginParams struct {
	UploadID       string `json:"upload_id"`
	RootID         string `json:"root_id"`
	AgentID        string `json:"agent_id,omitempty"`
	ExpectedDigest string `json:"expected_digest"`
	Size           int64  `json:"size,string"`
	MediaType      string `json:"media_type,omitempty"`
	Source         string `json:"source,omitempty"`
}

type UploadChunkParams struct {
	UploadID string `json:"upload_id"`
	Offset   int64  `json:"offset,string"`
	Data     []byte `json:"data"`
}

type UploadFinishParams struct {
	UploadID string `json:"upload_id"`
}

type ContentHandle struct {
	ReferenceID string `json:"reference_id"`
	Digest      string `json:"digest"`
	Size        int64  `json:"size,string"`
	MediaType   string `json:"media_type,omitempty"`
	Source      string `json:"source,omitempty"`
}

type PermissionDecision struct {
	CommandID    string `json:"command_id"`
	RootID       string `json:"root_id"`
	PermissionID string `json:"permission_id"`
	Allow        bool   `json:"allow"`
	Reason       string `json:"reason,omitempty"`
	Remember     string `json:"remember,omitempty"` // "", "tree", or "global"
}

type PermissionDecisionParams struct {
	Decision PermissionDecision `json:"decision"`
}

type PermissionDecisionResult struct {
	OperationID string `json:"operation_id"`
	LeaseID     string `json:"lease_id"`
}

type RestartNotice struct {
	Generation int64     `json:"generation,string"`
	Cursors    CursorMap `json:"cursors"`
}

type RestartParams struct {
	Generation int64 `json:"generation,string"`
}

type SubscribeParams struct {
	RootID         string `json:"root_id"`
	SubscriptionID string `json:"subscription_id"`
	Cursor         int64  `json:"cursor,string"`
}

type SubscribeResult struct {
	SubscriptionID string `json:"subscription_id"`
	Cursor         int64  `json:"cursor,string"`
}

type UnsubscribeParams struct {
	SubscriptionID string `json:"subscription_id"`
}

func (e *RPCError) Error() string { return e.Message }

type RuntimeConfiguration struct {
	ImportClaude    bool   `json:"import_claude"`
	ImportCodex     bool   `json:"import_codex"`
	Revision        string `json:"revision"`
	DefaultModel    string `json:"default_model"`
	DefaultProvider string `json:"default_provider"`
	DefaultEffort   string `json:"default_effort"`
	CompactModel    string `json:"compact_model"`
	CompactProvider string `json:"compact_provider"`
	CompactPercent  int    `json:"compact_percent"`
	GoalMaxRounds   int    `json:"goal_max_rounds"`
	MaxRetries      int    `json:"max_retries"`
}

type ConfigurationUpdate struct {
	ImportClaude    *bool   `json:"import_claude,omitempty"`
	ImportCodex     *bool   `json:"import_codex,omitempty"`
	Revision        string  `json:"revision"`
	DefaultModel    *string `json:"default_model,omitempty"`
	DefaultProvider *string `json:"default_provider,omitempty"`
	DefaultEffort   *string `json:"default_effort,omitempty"`
	CompactModel    *string `json:"compact_model,omitempty"`
	CompactProvider *string `json:"compact_provider,omitempty"`
	CompactPercent  *int    `json:"compact_percent,omitempty"`
	GoalMaxRounds   *int    `json:"goal_max_rounds,omitempty"`
	MaxRetries      *int    `json:"max_retries,omitempty"`
}

type ProviderKeySetup struct {
	Revision    string `json:"revision"`
	Provider    string `json:"provider"`
	Key         string `json:"key,omitempty"`
	Environment bool   `json:"environment"`
}

type ProviderChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ProviderLoginStatus struct {
	FlowID          string           `json:"flow_id"`
	State           string           `json:"state"`
	VerificationURL string           `json:"verification_url,omitempty"`
	UserCode        string           `json:"user_code,omitempty"`
	Email           string           `json:"email,omitempty"`
	Teams           []ProviderChoice `json:"teams"`
	Projects        []ProviderChoice `json:"projects"`
	TeamID          string           `json:"team_id,omitempty"`
	ProjectID       string           `json:"project_id,omitempty"`
	ExpiresAt       time.Time        `json:"expires_at"`
	Error           string           `json:"error,omitempty"`
}

type ProviderLoginParams struct {
	FlowID string `json:"flow_id"`
}

type ProviderLoginTeamParams struct {
	FlowID string `json:"flow_id"`
	TeamID string `json:"team_id"`
}

type ProviderLoginProjectParams struct {
	FlowID    string `json:"flow_id"`
	ProjectID string `json:"project_id"`
}

type ProviderLoginCreateParams struct {
	FlowID string `json:"flow_id"`
	Name   string `json:"name"`
}
