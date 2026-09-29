package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func (s *Server) connect(parent context.Context) (*Unix, error) {
	ctx, cancel := context.WithTimeout(parent, handshakeTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", s.options.SocketPath)
	if err != nil {
		return nil, err
	}
	stream := NewUnix(conn)
	stop := context.AfterFunc(ctx, func() { _ = stream.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return nil, err
	}
	params := protocol.InitializeParams{Major: protocol.Major, NetworkClient: true, ExpectedRuntimeID: &s.options.RuntimeID, ExpectedProcessEpoch: &s.options.ProcessEpoch}
	var result protocol.InitializeResult
	if err := invoke(stream, "initialize", params, &result); err != nil {
		_ = stream.Close()
		return nil, err
	}
	if err := s.checkBackend(result); err != nil {
		_ = stream.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return stream, nil
}

func (s *Server) checkBackend(result protocol.InitializeResult) error {
	if result.Major != protocol.Major || !result.NetworkClient {
		return errors.New("runtime did not acknowledge network restrictions")
	}
	if result.RuntimeID != s.options.RuntimeID || result.ProcessEpoch != s.options.ProcessEpoch {
		return errors.New("runtime identity or process generation changed")
	}
	return nil
}

func invoke(stream *Unix, method string, params, result any) error {
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	request := protocol.Request{JSONRPC: "2.0", ID: "gateway", Method: method, Params: encoded}
	frame, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if err := stream.WriteMessage(frame); err != nil {
		return err
	}
	raw, err := stream.ReadMessage()
	if err != nil {
		return err
	}
	if err := protocol.Validate("Response", raw); err != nil {
		return err
	}
	var response protocol.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		return err
	}
	if response.ID != request.ID {
		return errors.New("gateway response identity mismatch")
	}
	if response.Error != nil {
		return errors.New(response.Error.Kind)
	}
	for _, operation := range protocol.Operations() {
		if operation.Name == method {
			if err := protocol.Validate(operation.Result.Name(), response.Result); err != nil {
				return err
			}
			return json.Unmarshal(response.Result, result)
		}
	}
	return errors.New("unknown gateway method")
}
