package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestChildNamesPersistWithoutChangingRoutingOrExactRetries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, parent := create(t, s, nil)
	if parent.Name != "" {
		t.Fatal("root acquired a child display name")
	}
	request := childRequest(parent.ID)
	request.Name = "Repository reviewer"
	first := spawnChildTest(t, s, "named", request)
	duplicate := spawnChildTest(t, s, "duplicate-label", request)
	automatic := spawnChildTest(t, s, "automatic-label", childRequest(parent.ID))
	if first.Session.Name != request.Name || duplicate.Session.Name != request.Name || first.Session.ID == duplicate.Session.ID || automatic.Session.Name != session.DefaultChildName(automatic.Session.ID) {
		t.Fatal("names replaced routing identities or defaults were unstable")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	if retried := spawnChildTest(t, s, "named", request); !reflect.DeepEqual(first, retried) {
		t.Fatal("restart changed named child admission")
	}
	loaded, err := s.Session(t.Context(), automatic.Session.ID)
	if err != nil || loaded.Name != automatic.Session.Name {
		t.Fatalf("default name did not survive restart: %+v %v", loaded, err)
	}
	changed := request
	changed.Name = "Changed reviewer"
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "spawn-test", RequestID: "named"}, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("retry changed immutable name: %v", err)
	}
	if _, err := s.db.ExecContext(t.Context(), "UPDATE child_names SET name=? WHERE session_id=?", "changed", first.Session.ID); err == nil {
		t.Fatal("name was mutable")
	}
	if _, err := s.db.ExecContext(t.Context(), "INSERT INTO child_names(session_id,name) VALUES (?,?)", parent.ID, "root"); err == nil {
		t.Fatal("root acquired a child name")
	}
	if err := s.DeleteSubtree(t.Context(), first.Session.ID); err != nil {
		t.Fatal(err)
	}
	if count(t, s, "child_names") != 2 {
		t.Fatal("child deletion did not delete only its own name")
	}
}

func TestChildNameAndTemplateValidationCannotAdmitPartialWork(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, nil)
	for _, name := range []string{" leading", "trailing ", "line\nbreak", "control\x00", strings.Repeat("x", session.MaxChildNameBytes+1), strings.Repeat("é", session.MaxChildNameBytes/2+1), string([]byte{0xff})} {
		request := childRequest(parent.ID)
		request.Name = name
		if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "invalid"}, request); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid name admitted: %q %v", name, err)
		}
	}
	for _, template := range []string{"missing", "../outside"} {
		request := childRequest(parent.ID)
		request.Template = template
		if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "invalid"}, request); !errors.Is(err, session.ErrInvalid) {
			t.Fatalf("invalid template admitted: %q %v", template, err)
		}
	}
	if count(t, s, "sessions") != 1 || count(t, s, "child_names") != 0 || count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 {
		t.Fatal("invalid child identity admitted partial work")
	}
}

func TestChildTemplateUsesCapturedAliasAndCannotWidenBindings(t *testing.T) {
	s := fresh(t)
	_, parent := create(t, s, nil)
	first := bindingDefinition(t, s, "first", false, false)
	second := bindingDefinition(t, s, "second", false, false)
	var err error
	parent, err = s.UpdateConfiguration(t.Context(), parent.ID, parent.ConfigRevision, session.ConfigPatch{Children: map[string]session.DefinitionRef{"review": first.Ref}})
	if err != nil {
		t.Fatal(err)
	}
	submit(t, s, parent.ID, "turn")
	turn := claim(t, s, parent.ID).Turn
	message, err := s.AppendMessage(t.Context(), turn.ID, session.MessageDraft{ID: "call", Role: session.Assistant, Parts: []session.Part{{Type: "tool_call", Call: &session.ToolCall{ID: "execute", Name: "execute", Arguments: json.RawMessage(`{"code":"1"}`)}}}})
	if err != nil {
		t.Fatal(err)
	}
	cell, _, err := s.BeginCell(t.Context(), session.CellSpec{ID: "cell", TurnID: turn.ID, CallMessageID: message.ID, CallID: "execute"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err = s.UpdateConfiguration(t.Context(), parent.ID, parent.ConfigRevision, session.ConfigPatch{Children: map[string]session.DefinitionRef{"review": second.Ref}})
	if err != nil {
		t.Fatal(err)
	}
	controlGrant(t, s, parent, "spawn", "agents.spawn", string(parent.TreeID))
	request := childRequest(parent.ID)
	request.Name, request.Template = "Reviewer", "review"
	preview, err := s.PreviewChild(t.Context(), cell.ID, request)
	if err != nil || preview.Name != request.Name || preview.Template != "review" || preview.Definition != first.Ref {
		t.Fatalf("preview ignored captured template: %+v %v", preview, err)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	op := admitOperation(t, s, session.OperationSpec{ID: "spawn", CellID: cell.ID, RequestID: "spawn", Capability: "agents.spawn", Resource: string(parent.TreeID), Arguments: raw})
	child, err := s.SpawnChildOperation(t.Context(), op.ID)
	if err != nil || child.Session == nil || child.Session.Definition != first.Ref || child.Session.Name != "Reviewer" {
		t.Fatalf("spawn ignored captured template: %+v %v", child, err)
	}
	direct := spawnChildTest(t, s, "direct", request)
	if direct.Session.Definition != second.Ref {
		t.Fatal("public spawn ignored current parent alias")
	}
	request.Definition = &first.Ref
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "both"}, request); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("template and definition both accepted: %v", err)
	}
	request.Definition = nil
	_, err = s.UpdateConfiguration(t.Context(), parent.ID, parent.ConfigRevision, session.ConfigPatch{Modules: []string{"files"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SpawnChild(t.Context(), session.RequestIdentity{ClientID: "test", RequestID: "widen"}, request); !errors.Is(err, session.ErrInvalid) {
		t.Fatalf("named template widened parent modules: %v", err)
	}
}
