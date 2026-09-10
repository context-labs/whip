package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/context-labs/whip/internal/agentdef"
	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
)

// handleExecutor serves the executor lease and tool settlement RPCs. Results
// and progress are accepted only from the connection holding the lease.
func (s *Server) handleExecutor(connection *serverConn, request rpcMessage) (any, *RPCError, bool) {
	registry := s.daemon.executors
	switch request.Method {
	case "executor.bind":
		var params protocol.ExecutorBindParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		tools, err := executorTools(connection.ctx, s.daemon.store, params)
		if err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		generation := registry.bind(connection, params.Definition, params.Revision, tools)
		return protocol.ExecutorBindResult{Generation: generation, Tools: tools}, nil, true
	case "executor.pending":
		var params protocol.ExecutorPendingParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		invocations, err := registry.pendingFor(connection, params)
		if err != nil {
			return nil, rpcFailure(-32003, err.Error()), true
		}
		return protocol.ExecutorPendingResult{Invocations: invocations}, nil, true
	case "tool.result":
		var params protocol.ToolResultParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if err := registry.settle(connection, params); err != nil {
			return nil, rpcFailure(-32009, err.Error()), true
		}
		return protocol.Accepted{Accepted: true}, nil, true
	case "tool.progress":
		var params protocol.ToolProgressParams
		if err := decodeProviderParams(request.Params, &params); err != nil {
			return nil, rpcFailure(-32602, err.Error()), true
		}
		if err := registry.report(connection, params); err != nil {
			return nil, rpcFailure(-32009, err.Error()), true
		}
		return protocol.Accepted{Accepted: true}, nil, true
	}
	return nil, nil, false
}

// executorTools resolves the definition revision an executor binds and checks
// that the offered handlers cover every declared tool, and nothing more.
func executorTools(ctx context.Context, store *session.Store, params protocol.ExecutorBindParams) ([]string, error) {
	if params.Definition == "" {
		return nil, errors.New("executor.bind requires a definition id")
	}
	var definition agentdef.Definition
	if builtIn, ok := agentdef.Lookup(params.Definition); ok {
		if params.Revision != "" {
			return nil, fmt.Errorf("built-in definition %q has no revisions", params.Definition)
		}
		definition = builtIn
	} else {
		if params.Revision == "" {
			return nil, errors.New("executor.bind requires the registered definition's revision")
		}
		record, err := store.LoadDefinition(ctx, params.Definition, params.Revision)
		if errors.Is(err, session.ErrNoDefinition) {
			return nil, fmt.Errorf("agent definition %q revision %s is not registered", params.Definition, params.Revision)
		}
		if err != nil {
			return nil, err
		}
		if definition, err = agentdef.Decode(record.Body); err != nil {
			return nil, err
		}
	}
	declared := definition.ToolNames()
	if len(declared) == 0 {
		return nil, fmt.Errorf("agent definition %q declares no tools", params.Definition)
	}
	for _, name := range declared {
		if !slices.Contains(params.Tools, name) {
			return nil, fmt.Errorf("executor offers no handler for tool %q", name)
		}
	}
	for _, name := range params.Tools {
		if !slices.Contains(declared, name) {
			return nil, fmt.Errorf("agent definition %q declares no tool %q", params.Definition, name)
		}
	}
	return declared, nil
}
