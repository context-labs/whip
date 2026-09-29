package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func setDirectory(t *testing.T, s *Store, request session.WorkspaceSetRequest, path string) (session.ControlEdit, error) {
	t.Helper()
	current, err := s.Session(t.Context(), request.SessionID)
	if err != nil {
		return session.ControlEdit{}, err
	}
	owners, err := s.ControlOwners(t.Context(), current.TreeID)
	if err != nil {
		return session.ControlEdit{}, err
	}
	return s.SetWorkingDirectory(t.Context(), request, path, owners)
}

func TestSessionControlsCaptureHistoryAndExactDeletedRetry(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	submit(t, s, owner.ID, "old")
	old := claim(t, s, owner.ID)
	if _, err := s.Finish(t.Context(), old.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "directory ")
	request := session.WorkspaceSetRequest{ID: "cd", SessionID: owner.ID, ExpectedRevision: 1, Path: "../directory "}
	edited, err := setDirectory(t, s, request, destination)
	if err != nil {
		t.Fatal(err)
	}
	if edited.Revision != 2 || edited.Session.WorkingDirectory != destination || edited.Session.HistoryRevision != owner.HistoryRevision {
		t.Fatal(edited)
	}
	captured, err := s.ConfigurationSession(t.Context(), owner.ID, old.Turn.ConfigRevision)
	if err != nil || captured.WorkingDirectory != owner.WorkingDirectory {
		t.Fatal(captured, err)
	}
	submit(t, s, owner.ID, "new")
	next := claim(t, s, owner.ID)
	current, err := s.ConfigurationSession(t.Context(), owner.ID, next.Turn.ConfigRevision)
	if err != nil || current.WorkingDirectory != destination {
		t.Fatal(current, err)
	}
	retry, err := s.SetWorkingDirectory(t.Context(), request, "not an absolute path", nil)
	if err != nil || retry.Revision != 2 {
		t.Fatal("receipt lost to mutable checks", retry, err)
	}
	changed := request
	changed.Path = "different"
	if _, err := s.WorkspaceSetRetry(t.Context(), changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.UpdateConfiguration(t.Context(), owner.ID, 2, session.ConfigPatch{AutomaticTitle: new(false)}); err != nil {
		t.Fatal(err)
	}
	retry, err = s.WorkspaceSetRetry(t.Context(), request)
	if err != nil || retry.Session.ConfigRevision != 2 || !retry.Session.Config.AutomaticTitle {
		t.Fatal("retry projected later config", retry, err)
	}
	if _, err := s.Finish(t.Context(), next.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	retry, err = s.WorkspaceSetRetry(t.Context(), request)
	if err != nil || !retry.Deleted || retry.Session != nil {
		t.Fatal(retry, err)
	}
	mustFail(t, s, "DELETE FROM session_control_edits")
}

func TestSessionControlsIdleOwnersPinsCASAndRollback(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	request := session.WorkspaceSetRequest{ID: "cd", SessionID: owner.ID, ExpectedRevision: 1, Path: "new"}
	target := t.TempDir()
	if _, err := s.SetWorkingDirectory(t.Context(), request, target, nil); !errors.Is(err, ErrConflict) {
		t.Fatal("unguarded owners", err)
	}
	submit(t, s, owner.ID, "pending")
	if _, err := setDirectory(t, s, request, target); !errors.Is(err, ErrBusy) {
		t.Fatal("queued input", err)
	}
	work := claim(t, s, owner.ID)
	if _, err := setDirectory(t, s, request, target); !errors.Is(err, ErrBusy) {
		t.Fatal("active work", err)
	}
	if _, err := s.ConfigureRun(t.Context(), session.RunConfigureRequest{ID: "run", SessionID: owner.ID, ExpectedRevision: 1}); !errors.Is(err, ErrBusy) {
		t.Fatal("run active", err)
	}
	if _, err := s.Finish(t.Context(), work.Turn.ID, session.Succeeded, nil, nil); err != nil {
		t.Fatal(err)
	}
	execTest(t, s, "CREATE TRIGGER fail_control BEFORE INSERT ON session_control_edits BEGIN SELECT RAISE(ABORT,'injected'); END")
	if _, err := setDirectory(t, s, request, target); err == nil {
		t.Fatal("injected write succeeded")
	}
	if count(t, s, "session_configurations") != 1 || count(t, s, "session_control_edits") != 0 {
		t.Fatal("partial config write")
	}
	execTest(t, s, "DROP TRIGGER fail_control")
	if _, err := setDirectory(t, s, request, target); err != nil {
		t.Fatal(err)
	}
	stale := request
	stale.ID = "stale"
	if _, err := setDirectory(t, s, stale, target); !errors.Is(err, ErrConflict) {
		t.Fatal("stale config", err)
	}
}

func TestRunControlRootPolicySurvivesUpdatesAndDoesNotInherit(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	request := session.RunConfigureRequest{ID: "run", SessionID: owner.ID, ExpectedRevision: 1, Configuration: session.RunConfiguration{System: "exact system", MaxTurns: 3, Headless: true, CacheKey: "cache"}}
	result, err := s.ConfigureRun(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Session.Config.Run == nil || *result.Session.Config.Run != request.Configuration {
		t.Fatal(result)
	}
	updated, err := s.UpdateConfiguration(t.Context(), owner.ID, 2, session.ConfigPatch{Model: &session.ModelSelection{Provider: "other", Name: "other"}})
	if err != nil || updated.Config.Run == nil || *updated.Config.Run != request.Configuration {
		t.Fatal(updated, err)
	}
	child, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "child"}, ChildRequest{ParentID: owner.ID, Parts: []session.Part{{Type: "text", Text: "child"}}})
	if err != nil {
		t.Fatal(err)
	}
	if child.Session.Config.Run != nil {
		t.Fatal("root override leaked to child")
	}
	request.ID = "busy-child"
	request.ExpectedRevision = 3
	if _, err := s.ConfigureRun(t.Context(), request); !errors.Is(err, ErrBusy) {
		t.Fatal("child queue ignored", err)
	}
	request.SessionID = child.Session.ID
	request.ExpectedRevision = 1
	if _, err := s.ConfigureRun(t.Context(), request); !errors.Is(err, session.ErrInvalid) {
		t.Fatal("child run control", err)
	}
}

func TestHeadlessRunDeniesHumanWaitsWithoutBroadeningAuthority(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		t.Run(map[bool]string{false: "ask", true: "automatic"}[automatic], func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			if automatic {
				if _, err := s.SetPermissionMode(t.Context(), session.PermissionModeRequest{ID: "mode", SessionID: owner.ID, ExpectedRevision: 1, Mode: session.PermissionAutomatic}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.ConfigureRun(t.Context(), session.RunConfigureRequest{ID: "run", SessionID: owner.ID, ExpectedRevision: 1, Configuration: session.RunConfiguration{Headless: true}}); err != nil {
				t.Fatal(err)
			}
			submit(t, s, owner.ID, "prompt")
			turn := claim(t, s, owner.ID).Turn
			message, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: "call", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "call", Name: "execute", Arguments: []byte(`{"code":"1"}`)}}}})
			if err != nil {
				t.Fatal(err)
			}
			cell, _, err := s.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: turn.ID, CallMessageID: message.ID, CallID: "call"})
			if err != nil {
				t.Fatal(err)
			}
			question := admitOperation(t, s, questionSpec(t, cell, "ask", false))
			if question.State != session.OperationDenied {
				t.Fatal(question)
			}
			op := admitOperation(t, s, operationSpec(cell, "read"))
			want := session.OperationDenied
			if automatic {
				want = session.OperationReady
			}
			if op.State != want {
				t.Fatal(op)
			}
			untrusted := operationSpec(cell, "mcp")
			untrusted.Capability = "mcp.call"
			if op := admitOperation(t, s, untrusted); op.State != session.OperationDenied {
				t.Fatal("untrusted MCP widened", op)
			}
			if count(t, s, "permissions") != 0 || count(t, s, "questions") != 0 {
				t.Fatal("headless created pending human wait")
			}
		})
	}
}
