package gateway

import (
	"context"
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

// watchPeer owns the already verified connection. The runtime's ordinary RPC
// idle limit is 30 seconds, so a bounded read-only heartbeat retains this exact
// observation. No execution, reconnection or replacement binding occurs here.
func (s *Server) watchPeer(ctx context.Context, stream *Unix, interval time.Duration) {
	ctx, cancel := context.WithCancel(ctx)
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(closed); _ = stream.Close() })
	type message struct {
		raw []byte
		err error
	}
	read := make(chan message, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			raw, err := stream.ReadMessage()
			select {
			case read <- message{raw, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		cancel()
		_ = stream.Close()
		<-readerDone
		if !stop() {
			<-closed
		}
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	pending := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if pending {
				return
			}
			if err := stream.SetWriteDeadline(time.Now().Add(handshakeTimeout)); err != nil {
				return
			}
			if err := stream.WriteMessage([]byte(`{"jsonrpc":"2.0","id":"gateway-monitor","method":"host.status","params":{}}`)); err != nil {
				return
			}
			pending = true
		case message := <-read:
			if message.err != nil || !pending || protocol.Validate("Response", message.raw) != nil {
				return
			}
			var response protocol.Response
			if json.Unmarshal(message.raw, &response) != nil || response.ID != "gateway-monitor" || response.Error != nil || protocol.Validate("HostStatus", response.Result) != nil {
				return
			}
			var status protocol.HostStatus
			if json.Unmarshal(response.Result, &status) != nil || status.RuntimeID != s.options.RuntimeID || status.ProcessEpoch != s.options.ProcessEpoch {
				return
			}
			pending = false
		}
	}
}
