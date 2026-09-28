// Package client is the Go v4 client. It knows wire values and transport only;
// no runtime, persistence or execution implementation is linked into callers.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

type Error struct{ protocol.RPCError }

func (e *Error) Error() string { return e.Kind + ": " + e.Message }

type Client struct {
	socket  string
	initial protocol.InitializeResult
}

func (c *Client) Identity() protocol.ID { return c.initial.RuntimeID }
func (c *Client) Builtins() []protocol.DefinitionRef {
	return append([]protocol.DefinitionRef{}, c.initial.Builtins...)
}

func Connect(ctx context.Context, socket string, expected *protocol.ID) (*Client, error) {
	if socket == "" {
		return nil, errors.New("runtime socket required")
	}
	var initial protocol.InitializeResult
	err := exchange(ctx, socket, protocol.InitializeParams{Major: protocol.Major, ExpectedRuntimeID: expected}, "initialize", nil, &initial)
	if err != nil {
		return nil, err
	}
	return &Client{socket: socket, initial: initial}, nil
}

// Call reconnects with the pinned runtime identity. Transport errors have an
// unknown delivery outcome; callers recover mutations with their request ID.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	if method == "initialize" {
		return errors.New("use Connect to initialize")
	}
	return exchange(ctx, c.socket, protocol.InitializeParams{Major: protocol.Major, ExpectedRuntimeID: &c.initial.RuntimeID}, method, params, result)
}

func exchange(parent context.Context, socket string, initial protocol.InitializeParams, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socket)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 4096), protocol.MaxFrameBytes+1)
	var initialized protocol.InitializeResult
	if err := invoke(conn, scanner, "init", "initialize", initial, &initialized); err != nil {
		return err
	}
	if method == "initialize" {
		raw, err := json.Marshal(initialized)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, result)
	}
	return invoke(conn, scanner, "call", method, params, result)
}

func invoke(conn net.Conn, scanner *bufio.Scanner, id protocol.ID, method string, params, result any) error {
	var operation *protocol.Operation
	for _, op := range protocol.Operations() {
		if op.Name == method {
			operation = &op
			break
		}
	}
	if operation == nil {
		return fmt.Errorf("unknown method %q", method)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	if err := protocol.Validate(operation.Params.Name(), raw); err != nil {
		return err
	}
	frame, err := json.Marshal(protocol.Request{JSONRPC: "2.0", ID: id, Method: method, Params: raw})
	if err != nil {
		return err
	}
	if len(frame) > protocol.MaxFrameBytes {
		return errors.New("request exceeds frame limit")
	}
	if _, err := conn.Write(append(frame, '\n')); err != nil {
		return err
	}
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return err
		}
		return errors.New("connection closed before acknowledgement; delivery is unknown")
	}
	if err := protocol.Validate("Response", scanner.Bytes()); err != nil {
		return err
	}
	var response protocol.Response
	if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
		return err
	}
	if response.ID != id {
		return errors.New("response identity mismatch")
	}
	if response.Error != nil {
		return &Error{*response.Error}
	}
	if err := protocol.Validate(operation.Result.Name(), response.Result); err != nil {
		return err
	}
	return json.Unmarshal(response.Result, result)
}

func (c *Client) Wait(ctx context.Context, identity protocol.RequestIdentity) (protocol.Admission, error) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		var result protocol.Admission
		if err := c.Call(ctx, "receipts.get", identity, &result); err != nil {
			return result, err
		}
		if result.Receipt.DeletedAt != nil || result.Input != nil && result.Input.State == "cancelled" || result.Turn != nil && result.Turn.FinishedAt != nil {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-ticker.C:
		}
	}
}
