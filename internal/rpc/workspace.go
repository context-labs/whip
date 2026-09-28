package rpc

import (
	"context"
	"encoding/json"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/runtime"
	"github.com/context-labs/whip/internal/session"
)

func dispatchWorkspace(ctx context.Context, r *runtime.Runtime, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "workspace.capture", "workspace.restore", "workspace.release":
		return decode(raw, func(p protocol.WorkspaceActionParams) (any, error) {
			request := session.WorkspaceRequest{ID: session.WorkspaceActionID(p.ActionID), SnapshotID: session.WorkspaceSnapshotID(p.SnapshotID), SessionID: session.SessionID(p.SessionID)}
			action := r.CaptureWorkspace
			switch method {
			case "workspace.restore":
				action = r.RestoreWorkspace
			case "workspace.release":
				action = r.ReleaseWorkspace
			}
			value, err := action(ctx, request)
			return protocol.WorkspaceResultFromDomain(value), err
		})
	case "workspace.action":
		return decode(raw, func(p protocol.ReadWorkspaceActionParams) (any, error) {
			value, err := r.WorkspaceAction(ctx, session.SessionID(p.SessionID), session.WorkspaceActionID(p.ActionID))
			return protocol.WorkspaceActionFromDomain(value), err
		})
	case "workspace.snapshot":
		return decode(raw, func(p protocol.WorkspaceSnapshotParams) (any, error) {
			value, err := r.WorkspaceSnapshot(ctx, session.SessionID(p.SessionID), session.WorkspaceSnapshotID(p.SnapshotID))
			return protocol.WorkspaceSnapshotFromDomain(value), err
		})
	case "workspace.snapshots":
		return decode(raw, func(p protocol.WorkspaceSnapshotsParams) (any, error) {
			values, err := r.WorkspaceSnapshots(ctx, session.SessionID(p.SessionID), session.WorkspaceSnapshotID(p.After), p.Limit)
			result := protocol.WorkspaceSnapshotsResult{Items: make([]protocol.WorkspaceSnapshot, len(values))}
			for i, value := range values {
				result.Items[i] = protocol.WorkspaceSnapshotFromDomain(value)
			}
			return result, err
		})
	default:
		return nil, ErrMethod
	}
}
