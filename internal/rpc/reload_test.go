package rpc_test

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/config"
	"github.com/context-labs/whip/internal/protocol"
)

func TestReloadRPCScopedPendingCancellationAndCapturedHostRevision(t *testing.T) {
	r, c := fixture(t)
	created := create(t, c)
	other := create(t, c)
	snapshot, err := r.HostConfiguration().Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	next, err := r.HostConfiguration().Update(t.Context(), snapshot.Revision, func(h *config.Host) error { h.Defaults.Compaction.ThresholdPercent = 75; return nil })
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.ReloadSessionParams{EditID: "Reload.Mixed", SessionID: created.Root.ID, ExpectedRevision: 1}
	pending := call[protocol.ReloadEdit](t, c, "sessions.reload", request)
	if pending.State != "pending" || pending.HostRevision != next.Revision || pending.Configuration.Compaction.ThresholdPercent != 75 || pending.Revision != nil {
		t.Fatal(pending)
	}
	if _, err := r.HostConfiguration().Update(t.Context(), next.Revision, func(h *config.Host) error { h.Defaults.Compaction.ThresholdPercent = 90; return nil }); err != nil {
		t.Fatal(err)
	}
	if retry := call[protocol.ReloadEdit](t, c, "sessions.reload", request); !reflect.DeepEqual(retry, pending) {
		t.Fatal(retry)
	}
	read := protocol.ReloadEditParams{SessionID: created.Root.ID, EditID: request.EditID}
	if retry := call[protocol.ReloadEdit](t, c, "sessions.reload_edit", read); !reflect.DeepEqual(retry, pending) {
		t.Fatal(retry)
	}
	requireHistoryError(t, c, "sessions.reload_edit", protocol.ReloadEditParams{SessionID: other.Root.ID, EditID: request.EditID}, "NOT_FOUND")
	requireHistoryError(t, c, "sessions.cancel_reload", protocol.ReloadEditParams{SessionID: other.Root.ID, EditID: request.EditID}, "NOT_FOUND")
	interrupted := call[protocol.ReloadEdit](t, c, "sessions.cancel_reload", read)
	if interrupted.State != "interrupted" || interrupted.Revision != nil || interrupted.SettledAt == nil {
		t.Fatal(interrupted)
	}
	if retry := call[protocol.ReloadEdit](t, c, "sessions.reload", request); !reflect.DeepEqual(retry, interrupted) {
		t.Fatal(retry)
	}
	request.ExpectedRevision = 2
	requireHistoryError(t, c, "sessions.reload", request, "CONFLICT")
	call[protocol.Session](t, c, "sessions.lifecycle", protocol.LifecycleParams{SessionID: created.Root.ID, Lifecycle: "stopped"})
	request.EditID, request.ExpectedRevision = "stopped", 1
	call[protocol.ReloadEdit](t, c, "sessions.reload", request)
	if err := r.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		value := call[protocol.ReloadEdit](t, c, "sessions.reload_edit", protocol.ReloadEditParams{SessionID: created.Root.ID, EditID: request.EditID})
		if value.State == "applied" {
			if *value.Revision != 2 || value.Configuration.Compaction.ThresholdPercent != 90 {
				t.Fatal(value)
			}
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("reload pending")
		case <-tick.C:
		}
	}
}

func TestReloadRPCExactCounterBeyondJavascriptPrecision(t *testing.T) {
	r, c := fixture(t)
	created := create(t, c)
	db, err := sql.Open("sqlite", filepath.Join(filepath.Dir(r.SocketPath()), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(t.Context(), `INSERT INTO session_configurations(session_id,revision,configuration,working_directory,created_at,override_fields) SELECT session_id,9007199254740993,configuration,working_directory,created_at,override_fields FROM session_configurations WHERE session_id=? AND revision=1`, created.Root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), "UPDATE sessions SET config_revision=9007199254740993 WHERE id=?", created.Root.ID); err != nil {
		t.Fatal(err)
	}
	edit := call[protocol.ReloadEdit](t, c, "sessions.reload", protocol.ReloadSessionParams{EditID: "exact", SessionID: created.Root.ID, ExpectedRevision: 9007199254740993})
	if edit.ExpectedRevision != 9007199254740993 {
		t.Fatal(edit)
	}
}
