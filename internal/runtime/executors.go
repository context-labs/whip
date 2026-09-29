package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/context-labs/whip/internal/executor"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/tool"
)

// ExecutorPeer borrows a connection identity from this execution owner. The
// transport closes it when that exact connection ends; it cannot be resumed.
func (r *Runtime) ExecutorPeer() (*executor.Peer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, ErrClosed
	}
	return r.executors.Peer()
}

func (r *Runtime) prepareCustomTool(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	declaration, enabled := current.Config.Tools[call.Name]
	if !enabled {
		return tool.Prepared{}, fmt.Errorf("%w: custom tool is not enabled for this turn", session.ErrInvalid)
	}
	if current.Config.ToolsDefinition == nil {
		return tool.Prepared{}, errors.New("custom tool executor unavailable: declaration has no registered owner")
	}
	if err := declaration.ValidateInput(call.Arguments); err != nil {
		return tool.Prepared{}, err
	}
	turn, err := r.invocationTurn(ctx, current.ID, call)
	if err != nil {
		return tool.Prepared{}, err
	}
	arguments := call.Arguments
	if arguments == nil {
		arguments = map[string]any{}
	}
	raw, err := json.Marshal(arguments)
	if err != nil {
		return tool.Prepared{}, err
	}
	ref := *current.Config.ToolsDefinition
	timeout := time.Duration(declaration.TimeoutMillis) * time.Millisecond
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	var reserved *executor.Call
	return tool.Prepared{
		Capability: "tools." + call.Name, Resource: ref.ID + "@" + ref.Revision,
		Arguments: raw, Mutating: true, CustomTimeout: timeout,
		Acquire: func(ctx context.Context) (func(), error) {
			if reserved != nil {
				return nil, executor.ErrConflict
			}
			value, err := r.executors.Acquire(ctx, ref, executor.Tool, call.Name)
			if err != nil {
				return nil, err
			}
			reserved = value
			if err := reserved.Check(ctx); err != nil {
				reserved.Close()
				return nil, err
			}
			return reserved.Close, nil
		},
		Run: func(ctx context.Context, operation session.OperationID) (any, error) {
			if reserved == nil {
				return nil, errors.New("custom executor lifetime was not acquired")
			}
			defer r.executorProgress(current.ID, turn, nil)
			result, err := reserved.Invoke(ctx, executor.Request{
				SessionID: current.ID, TurnID: turn, CellID: call.CellID, HostOperation: call.DirectTurnID != "", OperationID: operation,
				Operation: "tools." + call.Name, Arguments: raw, Timeout: timeout,
			}, func(text string) {
				r.executorProgress(current.ID, turn, &ExecutorProgress{InvocationID: reserved.ID(), OperationID: operation, Text: text})
			})
			if err != nil {
				return nil, err
			}
			if result.Failure != "" {
				return nil, tool.SettledFailure(errors.New(result.Failure))
			}
			if err := declaration.ValidateResult(result.Value); err != nil {
				return result.Value, tool.SettledFailure(err)
			}
			return result.Value, nil
		},
	}, nil
}
