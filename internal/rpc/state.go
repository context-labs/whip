package rpc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchState(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "state.get":
		return decode(raw, func(p protocol.GetStateParams) (any, error) {
			value, err := r.State(ctx, session.SessionID(p.SessionID), session.StateScope(p.Scope), p.Key)
			return protocol.StateVersionFromDomain(value), err
		})
	case "state.write", "state.append":
		return decode(raw, func(p protocol.WriteStateParams) (any, error) {
			if len(p.DataBase64) > base64.StdEncoding.EncodedLen(session.MaxContentBytes) {
				return nil, fmt.Errorf("%w: state write payload exceeds 4 MiB", session.ErrInvalid)
			}
			data, err := base64.StdEncoding.Strict().DecodeString(p.DataBase64)
			if err != nil {
				return nil, fmt.Errorf("%w: invalid state encoding", session.ErrInvalid)
			}
			write := r.WriteState
			if method == "state.append" {
				write = r.AppendState
			}
			value, err := write(ctx, session.SessionID(p.SessionID), session.StateScope(p.Scope), string(p.VersionID), p.Key, int64(p.ExpectedRevision), data)
			return protocol.StateVersionFromDomain(value), err
		})
	case "state.read":
		return decode(raw, func(p protocol.ReadStateParams) (any, error) {
			value, data, err := r.ReadStateRange(ctx, session.SessionID(p.SessionID), string(p.VersionID), int64(p.Offset), p.Length)
			return protocol.ReadStateResult{Version: protocol.StateVersionFromDomain(value), Offset: p.Offset, DataBase64: base64.StdEncoding.EncodeToString(data)}, err
		})
	case "state.list":
		return decode(raw, func(p protocol.ListStateParams) (any, error) {
			after := ""
			if p.After != nil {
				after = *p.After
			}
			values, err := r.ListState(ctx, session.SessionID(p.SessionID), session.StateScope(p.Scope), after, p.Limit)
			return stateVersions(values), err
		})
	case "state.history":
		return decode(raw, func(p protocol.StateHistoryParams) (any, error) {
			values, err := r.StateHistory(ctx, session.SessionID(p.SessionID), session.StateScope(p.Scope), p.Key, int64(p.After), p.Limit)
			return stateVersions(values), err
		})
	default:
		return nil, ErrMethod
	}
}

func stateVersions(values []session.StateValue) protocol.StateVersionsResult {
	result := protocol.StateVersionsResult{Items: []protocol.StateVersion{}}
	for _, value := range values {
		result.Items = append(result.Items, protocol.StateVersionFromDomain(value))
	}
	return result
}
