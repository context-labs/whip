package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/llm"
	"github.com/context-labs/whip/internal/session"
)

const (
	ProtocolMajor        = 1
	MaxFrameSize         = 1 << 20
	MaxSnapshotChunk     = 256 << 10
	MaxConnections       = 64
	MaxInFlight          = 32
	MaxOutboundEnvelopes = 1024
	MaxOutboundBytes     = 8 << 20
	MaxUploadSize        = 64 << 20
)

var ErrFrameTooLarge = errors.New("protocol frame exceeds 1 MiB")

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return e.Message }

type InitializeParams struct {
	ProtocolMajor int              `json:"protocol_major"`
	BuildID       string           `json:"build_id"`
	ClientKind    string           `json:"client_kind"`
	ClientID      string           `json:"client_id"`
	Capabilities  []string         `json:"capabilities,omitempty"`
	Cursors       map[string]int64 `json:"cursors,omitempty"`
}

type InitializeResult struct {
	ProtocolMajor int      `json:"protocol_major"`
	BuildID       string   `json:"build_id"`
	Generation    int64    `json:"generation"`
	PID           int      `json:"pid,omitempty"`
	StartedAt     string   `json:"started_at,omitempty"`
	Capabilities  []string `json:"capabilities"`
	Nonce         []byte   `json:"nonce"`
}

type CommandParams struct {
	CommandID string          `json:"command_id"`
	Scope     string          `json:"scope"`
	RootID    string          `json:"root_id,omitempty"`
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type CommandResult struct {
	CommandID  string `json:"command_id"`
	IngressSeq int64  `json:"ingress_seq"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
}

type ReplayParams struct {
	RootID string `json:"root_id"`
	Cursor int64  `json:"cursor"`
	Limit  int    `json:"limit,omitempty"`
}

type ProtocolEvent struct {
	RootID  string `json:"root_id"`
	Seq     int64  `json:"seq"`
	Kind    string `json:"kind"`
	Payload []byte `json:"payload,omitempty"`
}

type StreamEvent struct {
	AgentID string `json:"agent_id,omitempty"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Text    string `json:"text,omitempty"`
	Args    string `json:"args,omitempty"`
	Result  string `json:"result,omitempty"`
}

// SubmitPayload is the durable user input accepted by root submit commands.
// Parts are optional and carry ACP image/context content without giving the
// protocol adapter direct access to an agent.
type SubmitPayload struct {
	Text  string            `json:"text"`
	Parts []llm.ContentPart `json:"parts,omitempty"`
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
	Cursor       int64                   `json:"cursor"`
	Agent        session.RuntimeAgent    `json:"agent"`
	Messages     []llm.Message           `json:"messages"`
	Presentation []session.SnapshotEvent `json:"presentation,omitempty"`
	Inbox        []session.InboxItem     `json:"inbox,omitempty"`
}

type AgentSubmitResult struct {
	AgentID  string `json:"agent_id"`
	InboxSeq int64  `json:"inbox_seq"`
	Kind     string `json:"kind,omitempty"` // submit or steer
	Status   string `json:"status"`
}

type SessionPreviewResult struct {
	RootID    string `json:"root_id"`
	User      string `json:"user"`
	Assistant string `json:"assistant"`
}

// SessionUpdateEvent carries an ordered metadata change that a presenter can
// reduce without replacing the complete root snapshot.
type SessionUpdateEvent struct {
	Title         string `json:"title,omitempty"`
	Model         string `json:"model,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Effort        string `json:"effort,omitempty"`
	EffortChanged bool   `json:"effort_changed,omitempty"`
	WorkingDir    string `json:"working_directory,omitempty"`
}

// ProviderValidateParams is deliberately handled outside the durable command
// journal: Key is an ephemeral credential used only for this request.
type ProviderValidateParams struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	Key     string `json:"key"`
}

type ProviderValidateResult struct {
	Models []llm.ModelInfo `json:"models"`
}

type ProviderCatalogsResult struct {
	Catalogs map[string]config.Catalog `json:"catalogs"`
	Errors   map[string]string         `json:"errors,omitempty"`
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
	Latest  int64           `json:"latest"`
	Expired bool            `json:"expired,omitempty"`
}

type SnapshotParams struct {
	RootID string `json:"root_id"`
}

type SnapshotChunk struct {
	Index  int    `json:"index"`
	Count  int    `json:"count"`
	Cursor int64  `json:"cursor"`
	Data   []byte `json:"data"`
}

type SnapshotResult struct {
	SnapshotID string `json:"snapshot_id"`
	Count      int    `json:"count"`
	Cursor     int64  `json:"cursor"`
}

type SnapshotChunkParams struct {
	SnapshotID string `json:"snapshot_id"`
	Index      int    `json:"index"`
}

type UploadBeginParams struct {
	UploadID       string `json:"upload_id"`
	RootID         string `json:"root_id"`
	ExpectedDigest string `json:"expected_digest"`
	Size           int64  `json:"size"`
	MediaType      string `json:"media_type,omitempty"`
	Source         string `json:"source,omitempty"`
}

type UploadChunkParams struct {
	UploadID string `json:"upload_id"`
	Offset   int64  `json:"offset"`
	Data     []byte `json:"data"`
}

type UploadFinishParams struct {
	UploadID string `json:"upload_id"`
}

type ContentHandle struct {
	ReferenceID string `json:"reference_id"`
	Digest      string `json:"digest"`
	Size        int64  `json:"size"`
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

type IdentityStatusResult struct {
	ClientID       string `json:"client_id"`
	Kind           string `json:"kind"`
	Paired         bool   `json:"paired"`
	EnrollmentOpen bool   `json:"enrollment_open"`
}

type PermissionDecisionParams struct {
	Decision  PermissionDecision `json:"decision"`
	Signature []byte             `json:"signature"`
}

type PermissionDecisionResult struct {
	OperationID string `json:"operation_id"`
	LeaseID     string `json:"lease_id"`
	Nonce       []byte `json:"nonce"`
}

type PermissionModeParams struct {
	Command   CommandParams `json:"command"`
	Signature []byte        `json:"signature"`
}

type PermissionModeResult struct {
	Command CommandResult `json:"command"`
	Nonce   []byte        `json:"nonce"`
}

type RestartNotice struct {
	Generation int64            `json:"generation"`
	Cursors    map[string]int64 `json:"cursors"`
}

type RestartParams struct {
	Generation int64 `json:"generation"`
}

type eventNotification struct {
	Event ProtocolEvent `json:"event"`
}

type snapshotRequired struct {
	RootID string `json:"root_id"`
	Cursor int64  `json:"cursor"`
}

func requestDigest(scope, rootID, operation string, payload json.RawMessage) (string, error) {
	var value any
	if len(payload) == 0 {
		value = nil
	} else if err := json.Unmarshal(payload, &value); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(struct {
		Scope     string `json:"scope"`
		RootID    string `json:"root_id,omitempty"`
		Operation string `json:"operation"`
		Payload   any    `json:"payload,omitempty"`
	}{scope, rootID, operation, value})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

func marshalFrame(message rpcMessage) ([]byte, error) {
	message.JSONRPC = "2.0"
	data, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	if len(data)+1 > MaxFrameSize {
		return nil, ErrFrameTooLarge
	}
	return append(data, '\n'), nil
}

func decodeFrame(data []byte) (rpcMessage, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return rpcMessage{}, errors.New("empty protocol frame")
	}
	var message rpcMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return rpcMessage{}, err
	}
	if message.JSONRPC != "2.0" {
		return rpcMessage{}, fmt.Errorf("unsupported jsonrpc version %q", message.JSONRPC)
	}
	return message, nil
}
