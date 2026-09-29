package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/gobwas/ws"
)

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	if !s.acquire(s.connections) {
		http.Error(w, "connection limit reached", http.StatusServiceUnavailable)
		return
	}
	defer s.release(s.connections)
	conn, buffered, _, err := (ws.HTTPUpgrader{Timeout: handshakeTimeout}).Upgrade(r, w)
	if conn != nil {
		defer func() { _ = conn.Close() }()
	}
	if err != nil {
		return
	}
	browser := NewWebSocket(conn, buffered.Reader)
	stopBrowser := context.AfterFunc(r.Context(), func() { _ = browser.Close() })
	defer stopBrowser()
	deadline := time.Now().Add(handshakeTimeout)
	_ = browser.SetReadDeadline(deadline)
	frame, err := browser.ReadMessage()
	if err != nil {
		return
	}
	initialize, id, err := s.restrictInitialize(frame)
	if err != nil {
		rejectHandshake(browser, id, err)
		return
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	upstreamConn, err := (&net.Dialer{}).DialContext(ctx, "unix", s.options.SocketPath)
	if err != nil {
		rejectHandshake(browser, id, errors.New("runtime is unavailable"))
		return
	}
	upstream := NewUnix(upstreamConn)
	defer func() { _ = upstream.Close() }()
	stopUpstream := context.AfterFunc(r.Context(), func() { _ = upstream.Close() })
	defer stopUpstream()
	_ = upstreamConn.SetDeadline(deadline)
	if upstream.WriteMessage(initialize) != nil {
		return
	}
	reply, err := upstream.ReadMessage()
	if err != nil {
		rejectHandshake(browser, id, errors.New("runtime did not acknowledge network initialization"))
		return
	}
	var response protocol.Response
	if protocol.Validate("Response", reply) != nil || json.Unmarshal(reply, &response) != nil || response.ID != id {
		rejectHandshake(browser, id, errors.New("invalid runtime initialization response"))
		return
	}
	if response.Error != nil {
		_ = browser.WriteMessage(reply)
		return
	}
	var initial protocol.InitializeResult
	if protocol.Validate("InitializeResult", response.Result) != nil || json.Unmarshal(response.Result, &initial) != nil {
		rejectHandshake(browser, id, errors.New("invalid runtime initialization metadata"))
		return
	}
	if err := s.checkBackend(initial); err != nil {
		rejectHandshake(browser, id, err)
		return
	}
	// Pipelined browser traffic cannot reach the backend before this exact ack.
	if browser.WriteMessage(reply) != nil {
		return
	}
	_ = browser.SetReadDeadline(time.Time{})
	_ = upstream.SetReadDeadline(time.Time{})
	closeBoth := func() { _ = browser.Close(); _ = upstream.Close() }
	var workers sync.WaitGroup
	workers.Go(func() { defer closeBoth(); relay(upstream, browser, true) })
	relay(browser, upstream, false)
	closeBoth()
	workers.Wait()
}

func (s *Server) restrictInitialize(frame []byte) ([]byte, protocol.ID, error) {
	compact, _, err := compactEnvelope(frame)
	if err != nil {
		return nil, "invalid", err
	}
	var request protocol.Request
	if protocol.Validate("Request", compact) != nil || json.Unmarshal(compact, &request) != nil || request.Method != "initialize" {
		return nil, "invalid", errors.New("initialize must be the first browser request")
	}
	var params protocol.InitializeParams
	if protocol.Validate("InitializeParams", request.Params) != nil || json.Unmarshal(request.Params, &params) != nil {
		return nil, request.ID, errors.New("invalid browser initialization")
	}
	if params.ExpectedRuntimeID != nil && *params.ExpectedRuntimeID != s.options.RuntimeID || params.ExpectedProcessEpoch != nil && *params.ExpectedProcessEpoch != s.options.ProcessEpoch {
		return nil, request.ID, errors.New("runtime identity or process generation mismatch")
	}
	params.NetworkClient = true
	params.ExpectedRuntimeID = &s.options.RuntimeID
	params.ExpectedProcessEpoch = &s.options.ProcessEpoch
	request.Params, err = json.Marshal(params)
	if err != nil {
		return nil, request.ID, err
	}
	encoded, err := json.Marshal(request)
	return encoded, request.ID, err
}

func rejectHandshake(browser Transport, id protocol.ID, err error) {
	if id == "" {
		id = "invalid"
	}
	frame, _ := json.Marshal(protocol.Response{JSONRPC: "2.0", ID: id, Error: &protocol.RPCError{Code: -32014, Kind: "IDENTITY", Message: err.Error()}})
	_ = browser.WriteMessage(frame)
}

// One bounded message per direction; socket backpressure and deadlines replace
// relay queues. No reconnect or application request replay is performed here.
func relay(to, from Transport, browserSource bool) {
	for {
		frame, err := from.ReadMessage()
		if err != nil {
			return
		}
		compact, fields, err := compactEnvelope(frame)
		if err != nil {
			return
		}
		var method string
		_ = json.Unmarshal(fields["method"], &method)
		if browserSource && protocol.Validate("Request", compact) != nil {
			return
		}
		if browserSource && method == "initialize" {
			var id protocol.ID
			_ = json.Unmarshal(fields["id"], &id)
			rejectHandshake(from, id, errors.New("a browser connection cannot initialize twice"))
			return
		}
		if to.SetWriteDeadline(time.Now().Add(writeTimeout)) != nil || to.WriteMessage(compact) != nil {
			return
		}
	}
}
