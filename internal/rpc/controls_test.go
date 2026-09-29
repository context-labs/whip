package rpc_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestSessionControlsRPCExactRetryAndConfigurationProjection(t *testing.T) {
	_, c := fixture(t)
	root := create(t, c).Root
	inspect := call[protocol.WorkspaceInspection](t, c, "workspace.inspect", protocol.SessionParams{SessionID: root.ID})
	if inspect.WorkingDirectory != root.WorkingDirectory || inspect.ConfigurationRevision != root.ConfigRevision {
		t.Fatal(inspect)
	}
	directory := filepath.Join(t.TempDir(), "target ")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.WorkspaceSetParams{ID: "cd", SessionID: root.ID, ExpectedRevision: root.ConfigRevision, Path: path}
	changed := call[protocol.ControlEdit](t, c, "workspace.set", request)
	if changed.Revision != root.ConfigRevision+1 || changed.Session.WorkingDirectory != path || changed.Session.Configuration.Run != nil {
		t.Fatal(changed)
	}
	run := call[protocol.ControlEdit](t, c, "run.configure", protocol.RunConfigureParams{ID: "run", SessionID: root.ID, ExpectedRevision: changed.Revision, Configuration: protocol.RunConfiguration{System: "configured", MaxTurns: 0, Headless: true, CacheKey: "cache"}})
	if run.Session.Configuration.Run == nil || !run.Session.Configuration.Run.Headless {
		t.Fatal(run)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	retry := call[protocol.ControlEdit](t, c, "workspace.set", request)
	if retry.Revision != changed.Revision || retry.Session.Configuration.Run != nil {
		t.Fatal("exact retry projected latest config", retry)
	}
	var wire *client.Error
	var response json.RawMessage
	request.ID = "stale"
	if err := c.Call(t.Context(), "workspace.set", request, &response); !errors.As(err, &wire) || wire.Kind != "CONFLICT" {
		t.Fatal(err)
	}
	if err := c.Call(t.Context(), "run.configure", protocol.RunConfigureParams{ID: "bad", SessionID: root.ID, ExpectedRevision: run.Revision, Configuration: protocol.RunConfiguration{System: "\x00"}}, &response); !errors.As(err, &wire) || wire.Kind != "INVALID" {
		t.Fatal(err)
	}
	call[protocol.DeleteResult](t, c, "sessions.delete", protocol.SessionParams{SessionID: root.ID})
	request.ID = "cd"
	deleted := call[protocol.ControlEdit](t, c, "workspace.set", request)
	if !deleted.Deleted || deleted.Session != nil {
		t.Fatal(deleted)
	}
}
