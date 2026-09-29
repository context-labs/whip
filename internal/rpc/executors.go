package rpc

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/context-labs/whip/internal/executor"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// executorConnection is entered only after ordinary initialization and an
// explicit first bind. It owns one reader and one notification pump; a write
// mutex serializes those two bounded producers without another retained queue.
// No accepted session execution borrows this connection's lifetime.
func (s *Server) executorConnection(parent context.Context, conn net.Conn, scanner *bufio.Scanner, first protocol.Request) {
	ctx, cancel := context.WithCancel(parent)
	peer, err := s.runtime.ExecutorPeer()
	if err != nil {
		cancel()
		return
	}
	var pump sync.WaitGroup
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		cancel()
		peer.Close()
		_ = conn.Close()
		pump.Wait()
		stop()
	}()
	var writer sync.Mutex
	write := func(value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if len(encoded) > protocol.MaxFrameBytes {
			return errors.New("executor frame exceeds bounds")
		}
		writer.Lock()
		defer writer.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		_, err = conn.Write(append(encoded, '\n'))
		return err
	}
	succeeded := false
	respond := func(request protocol.Request) error {
		requestCtx, stop := context.WithTimeout(ctx, 10*time.Second)
		result, err := s.dispatchExecutor(requestCtx, peer, request.Method, request.Params)
		stop()
		response := protocol.Response{JSONRPC: "2.0", ID: request.ID}
		if err == nil {
			response.Result, err = json.Marshal(result)
		}
		if err != nil {
			response.Result = nil
			response.Error = executorWireError(err)
		}
		succeeded = err == nil
		return write(response)
	}
	// The first lease acknowledgement precedes every invocation event on this
	// connection, so a single-definition SDK server can install its generation.
	if err := respond(first); err != nil || !succeeded {
		return
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return
	}
	pump.Go(func() {
		defer cancel()
		for {
			event, err := peer.Next(ctx)
			if err != nil {
				return
			}
			wire := protocol.ExecutorEvent{JSONRPC: "2.0", Method: "executor." + event.Type, Epoch: protocol.ID(event.Epoch), Generation: protocol.Counter(event.Generation), InvocationID: protocol.ID(event.ID)}
			if event.Invocation != nil {
				wire.Invocation = new(executorInvocation(*event.Invocation))
			}
			if err := write(wire); err != nil {
				return
			}
		}
	})
	for scanner.Scan() {
		raw := scanner.Bytes()
		if protocol.Validate("Request", raw) != nil {
			return
		}
		var request protocol.Request
		if json.Unmarshal(raw, &request) != nil || respond(request) != nil {
			return
		}
	}
}

func (s *Server) dispatchExecutor(ctx context.Context, peer *executor.Peer, method string, raw json.RawMessage) (any, error) {
	var name string
	switch method {
	case "executor.bind":
		name = "ExecutorBindParams"
	case "executor.pending":
		name = "ExecutorPendingParams"
	case "tool.result":
		name = "ExecutorToolResultParams"
	case "hook.result":
		name = "ExecutorHookResultParams"
	case "tool.progress":
		name = "ExecutorProgressParams"
	default:
		return nil, ErrMethod
	}
	if err := protocol.Validate(name, raw); err != nil {
		return nil, fmt.Errorf("%w: invalid executor request", session.ErrInvalid)
	}
	switch method {
	case "executor.bind":
		return decode(raw, func(p protocol.ExecutorBindParams) (any, error) {
			definition, err := s.runtime.Definition(ctx, session.DefinitionRef{ID: string(p.Definition.ID), Revision: p.Definition.Revision})
			if err != nil {
				return nil, err
			}
			coverage := executor.Coverage{}
			for _, name := range p.Tools {
				coverage.Tools = append(coverage.Tools, string(name))
			}
			for _, name := range p.Hooks {
				coverage.Hooks = append(coverage.Hooks, string(name))
			}
			lease, err := peer.Bind(ctx, definition.Document, coverage)
			return executorLease(lease), err
		})
	case "executor.pending":
		return decode(raw, func(p protocol.ExecutorPendingParams) (any, error) {
			after := ""
			if p.After != nil {
				after = string(*p.After)
			}
			page, err := peer.Pending(executor.Lease{Epoch: string(p.Epoch), Definition: session.DefinitionRef{ID: string(p.Definition.ID), Revision: p.Definition.Revision}, Generation: int64(p.Generation)}, after)
			result := protocol.ExecutorPendingResult{Items: []protocol.ExecutorInvocation{}}
			for _, invocation := range page.Items {
				result.Items = append(result.Items, executorInvocation(invocation))
			}
			if page.Next != "" {
				result.NextAfter = new(protocol.ID(page.Next))
			}
			return result, err
		})
	case "tool.result":
		return decode(raw, func(p protocol.ExecutorToolResultParams) (any, error) {
			value, err := executorBytes(p.OutputBase64)
			if err != nil {
				return nil, err
			}
			err = peer.Settle(string(p.Epoch), int64(p.Generation), string(p.InvocationID), executor.Tool, executor.Result{Value: value, Failure: p.Failure})
			return protocol.ExecutorAccepted{Accepted: err == nil}, err
		})
	case "hook.result":
		return decode(raw, func(p protocol.ExecutorHookResultParams) (any, error) {
			arguments, err := executorBytes(p.ArgumentsBase64)
			if err != nil {
				return nil, err
			}
			spawn, err := executorBytes(p.SpawnBase64)
			if err != nil {
				return nil, err
			}
			err = peer.Settle(string(p.Epoch), int64(p.Generation), string(p.InvocationID), executor.Hook, executor.Result{Arguments: arguments, Spawn: spawn, Decision: p.Decision, Reason: p.Reason, Context: p.Context, Failure: p.Failure})
			return protocol.ExecutorAccepted{Accepted: err == nil}, err
		})
	case "tool.progress":
		return decode(raw, func(p protocol.ExecutorProgressParams) (any, error) {
			err := peer.Progress(string(p.Epoch), int64(p.Generation), string(p.InvocationID), p.Text)
			return protocol.ExecutorAccepted{Accepted: err == nil}, err
		})
	}
	return nil, ErrMethod
}

func executorBytes(encoded *string) (json.RawMessage, error) {
	if encoded == nil {
		return nil, nil
	}
	if len(*encoded) > base64.StdEncoding.EncodedLen(executor.MaxResultBytes) {
		return nil, fmt.Errorf("%w: executor result exceeds bounds", session.ErrInvalid)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(*encoded)
	if err != nil || len(raw) > executor.MaxResultBytes {
		return nil, fmt.Errorf("%w: invalid executor result encoding", session.ErrInvalid)
	}
	return raw, nil
}

func executorLease(lease executor.Lease) protocol.ExecutorLease {
	result := protocol.ExecutorLease{Epoch: protocol.ID(lease.Epoch), Definition: protocol.DefinitionRef{ID: protocol.ID(lease.Definition.ID), Revision: lease.Definition.Revision}, Generation: protocol.Counter(lease.Generation), Tools: []protocol.ID{}, Hooks: []protocol.ID{}}
	for _, name := range lease.Tools {
		result.Tools = append(result.Tools, protocol.ID(name))
	}
	for _, name := range lease.Hooks {
		result.Hooks = append(result.Hooks, protocol.ID(name))
	}
	return result
}

func executorInvocation(value executor.Invocation) protocol.ExecutorInvocation {
	request := value.Request
	origin := "turn"
	if request.HostOperation {
		origin = "host_operation"
	} else if request.CellID != "" {
		origin = "cell"
	}
	result := protocol.ExecutorInvocation{Origin: origin, InvocationID: protocol.ID(value.ID), Lease: executorLease(value.Lease), Kind: string(value.Kind), Name: protocol.ID(value.Name), SessionID: protocol.ID(request.SessionID), TurnID: protocol.ID(request.TurnID), Operation: request.Operation, InputPreview: request.Input, PermissionMode: request.PermissionMode, DeadlineMillis: protocol.Counter(value.Deadline.UnixMilli())}
	if request.CellID != "" {
		result.CellID = new(protocol.ID(request.CellID))
	}
	if request.OperationID != "" {
		result.OperationID = new(protocol.ID(request.OperationID))
	}
	if request.Arguments != nil {
		result.ArgumentsBase64 = new(base64.StdEncoding.EncodeToString(request.Arguments))
	}
	if request.Spawn != nil {
		result.SpawnBase64 = new(base64.StdEncoding.EncodeToString(request.Spawn))
	}
	return result
}

func executorWireError(err error) *protocol.RPCError {
	switch {
	case errors.Is(err, executor.ErrConflict), errors.Is(err, executor.ErrReplaced):
		return &protocol.RPCError{Code: -32009, Kind: "CONFLICT", Message: err.Error()}
	case errors.Is(err, executor.ErrClosed), errors.Is(err, executor.ErrDisconnected):
		return &protocol.RPCError{Code: -32003, Kind: "CLOSED", Message: err.Error()}
	case errors.Is(err, executor.ErrCapacity):
		return &protocol.RPCError{Code: -32003, Kind: "LIMIT", Message: err.Error()}
	default:
		return wireError(err)
	}
}
