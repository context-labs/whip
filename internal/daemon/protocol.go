package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
	RootID       string `json:"root_id"`
	PermissionID string `json:"permission_id"`
	Allow        bool   `json:"allow"`
	Reason       string `json:"reason,omitempty"`
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
