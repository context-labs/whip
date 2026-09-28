package protocol

import (
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func workspaceFixtures(fixtures []Fixture, created time.Time) ([]Fixture, error) {
	request := session.WorkspaceRequest{ID: "capture_workspace", SessionID: "session_child", SnapshotID: "snapshot_workspace"}
	snapshot := session.WorkspaceSnapshot{ID: request.SnapshotID, SessionID: request.SessionID, CaptureID: request.ID, CreatedAt: created, Semantics: session.WorkspaceSemantics}
	for _, state := range []session.WorkspaceActionState{session.WorkspaceClaimed, session.WorkspaceSucceeded, session.WorkspaceUncertain} {
		action := session.WorkspaceAction{WorkspaceRequest: request, Kind: session.WorkspaceCapture, State: state, CreatedAt: created}
		snapshot.State = state
		if state != session.WorkspaceClaimed {
			action.FinishedAt = &created
		}
		if state == session.WorkspaceUncertain {
			action.Failure = new("Workspace action outcome is uncertain; inspect the workspace before any new action.")
		}
		raw, err := json.Marshal(WorkspaceResultFromDomain(session.WorkspaceResult{Action: action, Snapshot: snapshot}))
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, Fixture{Type: "WorkspaceResult", Value: raw, Valid: true})
	}
	for _, item := range []struct {
		typeName, raw string
		valid         bool
	}{
		{"WorkspaceActionParams", `{"session_id":"session_child","action_id":"capture_workspace","snapshot_id":"snapshot_workspace"}`, true},
		{"ReadWorkspaceActionParams", `{"session_id":"session_child","action_id":"capture_workspace"}`, true},
		{"WorkspaceSnapshotParams", `{"session_id":"session_child","snapshot_id":"snapshot_workspace"}`, true},
		{"WorkspaceSnapshotsParams", `{"session_id":"session_child","limit":100}`, true},
		{"WorkspaceSnapshotsResult", `{"items":[]}`, true},
		{"WorkspaceActionParams", `{"session_id":"session_child","snapshot_id":"snapshot_workspace"}`, false},
		{"WorkspaceActionParams", `{"session_id":"session_child","action_id":"capture_workspace","snapshot_id":"snapshot_workspace","object_id":"host-private"}`, false},
		{"WorkspaceSnapshotsParams", `{"session_id":"session_child","limit":101}`, false},
		{"WorkspaceSnapshotsParams", `{"session_id":"session_child","limit":0}`, false},
		{"WorkspaceSnapshotsResult", `{"items":null}`, false},
	} {
		fixtures = append(fixtures, Fixture{Type: item.typeName, Value: json.RawMessage(item.raw), Valid: item.valid})
	}
	return fixtures, nil
}
