package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/protocoltransport"

	"github.com/context-labs/whip/internal/session"
)

const (
	ProtocolMajor        = protocol.Major
	ProtocolMinor        = protocol.Minor
	MaxSubscriptions     = 16
	MaxFrameSize         = protocoltransport.MaxFrameSize
	MaxContentChunk      = 256 << 10
	MaxConnections       = 64
	MaxInFlight          = 32
	MaxOutboundEnvelopes = 1024
	MaxOutboundBytes     = 8 << 20
	MaxUploadSize        = session.MaxInputPayloadBytes
)

var ErrFrameTooLarge = protocoltransport.ErrFrameTooLarge

type rpcMessage = protocoltransport.Message

type RPCError = protocol.RPCError

type InitializeParams = protocol.InitializeParams

type InitializeResult = protocol.InitializeResult

type CommandParams = protocol.CommandParams

type CommandStatusParams = protocol.CommandStatusParams

type CommandResult = protocol.CommandResult

type ReplayParams = protocol.ReplayParams

type ProtocolEvent = protocol.ProtocolEvent

type StreamEvent = protocol.StreamEvent

// SubmitPayload is the durable user input accepted by root submit commands.
// Parts are optional and carry ACP image/context content without giving the
// protocol adapter direct access to an agent.
type SubmitPayload = protocol.SubmitPayload

type UsageEvent = protocol.UsageEvent

type PlanItem = protocol.PlanItem

type PlanEvent = protocol.PlanEvent

type AgentTranscriptResult = protocol.AgentTranscriptResult

type AgentSubmitResult = protocol.AgentSubmitResult

type SessionPreviewResult = protocol.SessionPreviewResult

// SessionUpdateEvent carries an ordered metadata change that a presenter can
// reduce without replacing the complete root snapshot.
type SessionUpdateEvent = protocol.SessionUpdateEvent

// ProviderValidateParams is deliberately handled outside the durable command
// journal: Key is an ephemeral credential used only for this request.
type ProviderValidateParams = protocol.ProviderValidateParams

type ProviderValidateResult = protocol.ProviderValidateResult

type ProviderCatalogsResult = protocol.ProviderCatalogsResult

type ContextAuditRow = protocol.ContextAuditRow

type ContextAuditResult = protocol.ContextAuditResult

type MCPStatusResult = protocol.MCPStatusResult

type LSPStatusResult = protocol.LSPStatusResult

type ReplayResult = protocol.ReplayResult

type SnapshotParams = protocol.SnapshotParams

type UploadBeginParams = protocol.UploadBeginParams

type UploadChunkParams = protocol.UploadChunkParams

type UploadFinishParams = protocol.UploadFinishParams

type ContentHandle = protocol.ContentHandle

type PermissionDecision = protocol.PermissionDecision

type PermissionDecisionParams = protocol.PermissionDecisionParams

type PermissionDecisionResult = protocol.PermissionDecisionResult

type RestartNotice = protocol.RestartNotice

type RestartParams = protocol.RestartParams

type eventNotification struct {
	Event ProtocolEvent `json:"event"`
}

func requestDigest(scope, rootID, operation string, payload json.RawMessage) (string, error) {
	var value any
	if len(payload) == 0 {
		value = nil
	} else {
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return "", err
		}
		if decoder.Decode(new(any)) != io.EOF {
			return "", errors.New("invalid trailing command payload")
		}
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

func marshalFrame(message rpcMessage) ([]byte, error) { return protocoltransport.MarshalFrame(message) }
func decodeFrame(data []byte) (rpcMessage, error)     { return protocoltransport.DecodeFrame(data) }
