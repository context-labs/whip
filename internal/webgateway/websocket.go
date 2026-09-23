package webgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/protocoltransport"
	"github.com/gobwas/ws"
)

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	if !s.acquire(s.connections) {
		http.Error(w, "connection limit reached", http.StatusServiceUnavailable)
		return
	}
	defer s.release(s.connections)
	conn, buffered, _, err := (ws.HTTPUpgrader{Timeout: handshakeTimeout}).Upgrade(r, w)
	// Upgrade can hijack before rejecting the request. HTTP no longer owns it.
	if conn != nil {
		defer func() { _ = conn.Close() }()
	}
	if err != nil {
		return
	}
	browser := protocoltransport.NewWebSocket(conn, buffered.Reader)
	stopBrowser := context.AfterFunc(r.Context(), func() { _ = browser.Close() })
	defer stopBrowser()
	deadline := time.Now().Add(handshakeTimeout)
	_ = browser.SetReadDeadline(deadline)
	frame, err := browser.ReadMessage()
	if err != nil {
		return
	}
	initialize, id, err := restrictInitialize(frame)
	if err != nil {
		rejectHandshake(browser, id, err)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	upstreamConn, err := (&net.Dialer{}).DialContext(ctx, "unix", s.options.SocketPath)
	if err != nil {
		rejectHandshake(browser, id, errors.New("daemon is unavailable"))
		return
	}
	upstream := protocoltransport.NewUnix(upstreamConn)
	defer func() { _ = upstream.Close() }()
	stopUpstream := context.AfterFunc(r.Context(), func() { _ = upstream.Close() })
	defer stopUpstream()
	_ = upstream.SetReadDeadline(deadline)
	_ = upstream.SetWriteDeadline(deadline)
	if err := upstream.WriteMessage(initialize); err != nil {
		return
	}
	reply, err := upstream.ReadMessage()
	if err != nil {
		rejectHandshake(browser, id, errors.New("daemon did not acknowledge restricted initialization"))
		return
	}
	response, err := protocoltransport.DecodeFrame(reply)
	if err != nil || !bytes.Equal(bytes.TrimSpace(response.ID), bytes.TrimSpace(id)) || response.Method != "" {
		rejectHandshake(browser, id, errors.New("daemon returned an invalid initialization response"))
		return
	}
	if response.Error != nil {
		_ = browser.WriteMessage(reply)
		return
	}
	raw, err := json.Marshal(response.Result)
	var result protocol.InitializeResult
	if err != nil || json.Unmarshal(raw, &result) != nil {
		rejectHandshake(browser, id, errors.New("daemon returned invalid initialization metadata"))
		return
	}
	if err := s.checkBackend(result); err != nil {
		rejectHandshake(browser, id, err)
		return
	}
	// No second browser message is read, let alone forwarded, before this ack.
	if err := browser.WriteMessage(reply); err != nil {
		return
	}
	_ = browser.SetReadDeadline(time.Time{})
	_ = upstream.SetReadDeadline(time.Time{})
	closeBoth := func() { _ = browser.Close(); _ = upstream.Close() }
	var workers sync.WaitGroup
	workers.Go(func() {
		defer closeBoth()
		relay(upstream, browser, true)
	})
	relay(browser, upstream, false)
	closeBoth()
	workers.Wait()
}

func rejectHandshake(browser protocoltransport.Transport, id json.RawMessage, err error) {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	frame, marshalErr := protocoltransport.MarshalFrame(protocoltransport.Message{
		ID: id, Error: &protocol.RPCError{Code: -32001, Message: err.Error()},
	})
	if marshalErr == nil {
		_ = browser.WriteMessage(frame)
	}
}

// Validate a single JSON envelope, then compact it for newline-delimited Unix
// transport. Forwarding browser bytes verbatim would permit newline smuggling.
func compactEnvelope(frame []byte) ([]byte, protocoltransport.Message, error) {
	message, err := protocoltransport.DecodeFrame(frame)
	if err != nil {
		return nil, message, err
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, frame); err != nil {
		return nil, message, err
	}
	if compact.Len()+1 > protocoltransport.MaxFrameSize {
		return nil, message, protocoltransport.ErrFrameTooLarge
	}
	return compact.Bytes(), message, nil
}

func restrictInitialize(frame []byte) ([]byte, json.RawMessage, error) {
	_, message, err := compactEnvelope(frame)
	if err != nil {
		return nil, nil, err
	}
	if message.Method != "initialize" || len(message.ID) == 0 || bytes.Equal(message.ID, []byte("null")) {
		return nil, message.ID, errors.New("initialize must be the first browser request")
	}
	var fields, params map[string]json.RawMessage
	if json.Unmarshal(frame, &fields) != nil || json.Unmarshal(message.Params, &params) != nil || params == nil {
		return nil, message.ID, errors.New("initialize requires object parameters")
	}
	// Decode with the daemon's case-insensitive field rules, but preserve unknown
	// fields and provider advertisements when forcing the one restrict-only marker.
	var typed protocol.InitializeParams
	if err := json.Unmarshal(message.Params, &typed); err != nil {
		return nil, message.ID, err
	}
	capabilities := typed.Capabilities
	if !slices.Contains(capabilities, protocol.NetworkClientCapability) {
		capabilities = append(capabilities, protocol.NetworkClientCapability)
	}
	for key := range params {
		if strings.EqualFold(key, "capabilities") {
			delete(params, key)
		}
	}
	params["capabilities"], err = json.Marshal(capabilities)
	if err != nil {
		return nil, message.ID, err
	}
	// Canonicalize security-sensitive envelope keys too: sorting a map must not
	// change which duplicate/case-variant method the daemon would decode last.
	for key := range fields {
		if strings.EqualFold(key, "params") || strings.EqualFold(key, "method") ||
			strings.EqualFold(key, "id") || strings.EqualFold(key, "jsonrpc") {
			delete(fields, key)
		}
	}
	fields["method"] = json.RawMessage(`"initialize"`)
	fields["jsonrpc"] = json.RawMessage(`"2.0"`)
	fields["id"] = message.ID
	fields["params"], err = json.Marshal(params)
	if err != nil {
		return nil, message.ID, err
	}
	forced, err := json.Marshal(fields)
	if err != nil {
		return nil, message.ID, err
	}
	compact, _, err := compactEnvelope(forced)
	return compact, message.ID, err
}

// Each direction has at most one bounded message in memory, with no relay
// queue. Socket backpressure and a write deadline disconnect slow consumers.
func relay(to, from protocoltransport.Transport, browserSource bool) {
	for {
		frame, err := from.ReadMessage()
		if err != nil {
			return
		}
		compact, message, err := compactEnvelope(frame)
		if err != nil {
			return
		}
		if browserSource && message.Method == "initialize" {
			rejectHandshake(from, message.ID, errors.New("a browser connection cannot initialize twice"))
			return
		}
		if err := to.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			return
		}
		if err := to.WriteMessage(compact); err != nil {
			return
		}
	}
}
