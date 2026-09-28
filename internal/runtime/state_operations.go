package runtime

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/content"
	"github.com/context-labs/whip/internal/session"
	"github.com/context-labs/whip/internal/store"
	"github.com/context-labs/whip/internal/tool"
)

func (r *Runtime) prepareState(ctx context.Context, current session.Session, call tool.Invocation) (tool.Prepared, error) {
	var request any
	switch call.Name {
	case "get":
		request = &session.StateKey{}
	case "read":
		request = &session.StateRead{Length: session.MaxStateReadBytes}
	case "list":
		request = &session.StateList{Limit: 20}
	case "history":
		request = &session.StateHistory{Limit: 20}
	case "write", "append":
		request = &session.StatePut{}
	default:
		return tool.Prepared{}, fmt.Errorf("%w: unsupported state operation %s", session.ErrInvalid, call.Name)
	}
	if err := decodeArguments(call.Arguments, request); err != nil {
		return tool.Prepared{}, err
	}
	if put, ok := request.(*session.StatePut); ok {
		staged, err := r.stageState(ctx, current.ID, call.Name, *put)
		if err != nil {
			return tool.Prepared{}, err
		}
		request = staged
	}
	arguments, err := json.Marshal(request)
	if err != nil {
		return tool.Prepared{}, err
	}
	return tool.Prepared{
		Capability: "state." + call.Name, Resource: string(current.TreeID), Arguments: arguments,
		Apply: func(ctx context.Context, id session.OperationID) (any, error) {
			result, err := r.store.ApplyStateOperation(ctx, id)
			if err != nil {
				return nil, err
			}
			if call.Name != "get" && call.Name != "read" {
				return result, nil
			}
			var version session.StateValue
			if err := json.Unmarshal(result, &version); err != nil {
				return nil, err
			}
			if call.Name == "read" {
				read := request.(*session.StateRead)
				data, err := r.content.ReadVerifiedRange(content.Body{Digest: version.Digest, Size: version.Size}, read.Offset, read.Length)
				return session.StateChunk{Version: version, Offset: read.Offset, Data: data}, err
			}
			resultEntry := session.StateEntry{Version: version}
			if version.Size <= session.MaxStateReadBytes {
				resultEntry.Value, err = r.content.ReadVerified(content.Body{Digest: version.Digest, Size: version.Size}, session.MaxStateReadBytes)
			}
			return resultEntry, err
		},
	}, nil
}

func (r *Runtime) stageState(ctx context.Context, actor session.SessionID, operation string, put session.StatePut) (session.StateWrite, error) {
	request := session.StateWrite{ID: "staged", SessionID: actor, Scope: put.Scope, Key: put.Key}
	if put.ExpectedRevision == nil {
		return request, fmt.Errorf("%w: expected_revision is required", session.ErrInvalid)
	}
	request.ExpectedRevision = *put.ExpectedRevision
	if err := request.ValidateIdentity(); err != nil {
		return request, err
	}
	if err := session.ValidateStateJSON(put.Value); err != nil {
		return request, err
	}
	data := []byte(put.Value)
	if operation == "append" {
		if request.ExpectedRevision == 0 {
			return request, store.ErrConflict
		}
		versions, err := r.store.StateHistory(ctx, actor, put.Scope, put.Key, request.ExpectedRevision-1, 1)
		if err != nil {
			return request, err
		}
		if len(versions) != 1 || versions[0].Revision != request.ExpectedRevision {
			return request, store.ErrConflict
		}
		_, previous, err := r.ReadState(ctx, actor, versions[0].ID)
		if err != nil {
			return request, err
		}
		data, err = session.AppendStateJSON(previous, data)
		if err != nil {
			return request, err
		}
	}
	if err := ctx.Err(); err != nil {
		return request, err
	}
	body, err := r.content.Put(data)
	request.Digest, request.Size = body.Digest, body.Size
	return request, err
}
