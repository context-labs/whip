package protocol

import (
	"time"

	"github.com/context-labs/whip/internal/session"
)

type WorkspaceActionParams struct {
	ActionID   ID `json:"action_id"`
	SnapshotID ID `json:"snapshot_id"`
	SessionID  ID `json:"session_id"`
}

type ReadWorkspaceActionParams struct {
	SessionID ID `json:"session_id"`
	ActionID  ID `json:"action_id"`
}

type WorkspaceSnapshotParams struct {
	SessionID  ID `json:"session_id"`
	SnapshotID ID `json:"snapshot_id"`
}

type WorkspaceSnapshotsParams struct {
	SessionID ID  `json:"session_id"`
	After     ID  `json:"after,omitempty"`
	Limit     int `json:"limit" min:"1" max:"100"`
}

type WorkspaceAction struct {
	ID         ID      `json:"id"`
	SessionID  ID      `json:"session_id"`
	SnapshotID ID      `json:"snapshot_id"`
	Kind       string  `json:"kind" enum:"capture,restore,release"`
	State      string  `json:"state" enum:"claimed,succeeded,uncertain"`
	Failure    *string `json:"failure"`
	CreatedAt  string  `json:"created_at" format:"date-time"`
	FinishedAt *string `json:"finished_at"`
}

// WorkspaceSnapshot deliberately projects neither private paths nor Git object identities.
type WorkspaceSnapshot struct {
	ID         ID      `json:"id"`
	SessionID  ID      `json:"session_id"`
	CaptureID  ID      `json:"capture_id"`
	State      string  `json:"state" enum:"claimed,succeeded,uncertain"`
	Scope      string  `json:"scope" enum:"session_working_directory"`
	Semantics  string  `json:"semantics"`
	CreatedAt  string  `json:"created_at" format:"date-time"`
	ReleasedAt *string `json:"released_at"`
}

type WorkspaceResult struct {
	Action   WorkspaceAction   `json:"action"`
	Snapshot WorkspaceSnapshot `json:"snapshot"`
}

type WorkspaceSnapshotsResult struct {
	Items []WorkspaceSnapshot `json:"items"`
}

func WorkspaceActionFromDomain(value session.WorkspaceAction) WorkspaceAction {
	return WorkspaceAction{ID: ID(value.ID), SessionID: ID(value.SessionID), SnapshotID: ID(value.SnapshotID), Kind: string(value.Kind), State: string(value.State), Failure: value.Failure, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), FinishedAt: timeString(value.FinishedAt)}
}

func WorkspaceSnapshotFromDomain(value session.WorkspaceSnapshot) WorkspaceSnapshot {
	return WorkspaceSnapshot{ID: ID(value.ID), SessionID: ID(value.SessionID), CaptureID: ID(value.CaptureID), State: string(value.State), Scope: "session_working_directory", Semantics: value.Semantics, CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), ReleasedAt: timeString(value.ReleasedAt)}
}

func WorkspaceResultFromDomain(value session.WorkspaceResult) WorkspaceResult {
	return WorkspaceResult{Action: WorkspaceActionFromDomain(value.Action), Snapshot: WorkspaceSnapshotFromDomain(value.Snapshot)}
}
