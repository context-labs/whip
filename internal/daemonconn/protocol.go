package daemonconn

import (
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

const InitializationTimeout = 5 * time.Second

const MaxOutboundEnvelopes = 1024

const MaxContentChunk = 256 << 10

type EventNotification struct {
	Event protocol.ProtocolEvent `json:"event"`
}
