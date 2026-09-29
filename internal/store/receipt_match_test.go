package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func TestReceiptMatchNeverAdmitsAndKeepsPayloadProofAcrossDeletionAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	identity := session.RequestIdentity{ClientID: "caller", RequestID: "collision"}
	request := Submission{SessionID: owner.ID, Source: session.UserInput, Parts: []session.Part{{Type: "text", Text: "original"}}}
	if _, err := s.MatchSubmission(t.Context(), identity, request); !errors.Is(err, ErrNotFound) || count(t, s, "inputs") != 0 || count(t, s, "receipts") != 0 {
		t.Fatal("read admitted absent command", err)
	}
	accepted, err := s.Admit(t.Context(), identity, request)
	if err != nil {
		t.Fatal(err)
	}
	changed := request
	changed.Parts = []session.Part{{Type: "text", Text: "different"}}
	if _, err := s.MatchSubmission(t.Context(), identity, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("identity collision claimed payload match", err)
	}
	if _, err := s.SetLifecycle(t.Context(), owner.ID, session.Stopped); err != nil {
		t.Fatal(err)
	}
	value, err := s.MatchSubmission(t.Context(), identity, request)
	if err != nil || value.Input.ID != accepted.Input.ID || value.Receipt.Digest != accepted.Receipt.Digest {
		t.Fatal(value, err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	value, err = s.MatchSubmission(t.Context(), identity, request)
	if err != nil || value.Receipt.DeletedAt == nil || value.Input != nil || count(t, s, "sessions") != 0 {
		t.Fatal("tombstone lost proof", value, err)
	}
	if _, err := s.MatchSubmission(t.Context(), identity, changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}

func TestReceiptMatchUsesCapturedChildAndMaintenanceRequests(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	compactID := session.RequestIdentity{ClientID: "client", RequestID: "compact"}
	compact := Submission{SessionID: owner.ID, Source: session.UserInput, Kind: session.CompactInput}
	compacted, err := s.Admit(t.Context(), compactID, compact)
	if err != nil {
		t.Fatal(err)
	}
	compact.Parts = []session.Part{}
	if value, err := s.MatchSubmission(t.Context(), compactID, compact); err != nil || !reflect.DeepEqual(value, compacted) {
		t.Fatal(value, err)
	}
	if _, err := s.CancelInput(t.Context(), compacted.Input.ID); err != nil {
		t.Fatal(err)
	}
	childID := session.RequestIdentity{ClientID: "client", RequestID: "child"}
	childRequest := childRequest(owner.ID)
	child, err := s.SpawnChild(t.Context(), childID, childRequest)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := s.MatchChild(t.Context(), childID, childRequest); err != nil || value.Input.ID != child.Admission.Input.ID {
		t.Fatal(value, err)
	}
	childRequest.Parts = []session.Part{{Type: "text", Text: "different child request"}}
	if _, err := s.MatchChild(t.Context(), childID, childRequest); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	compactionHistoryTest(t, s, owner.ID, "context")
	formulationID := session.RequestIdentity{ClientID: "client", RequestID: "formulate"}
	formulation := session.GoalFormulationRequest{GoalID: "formulated"}
	formulated, err := s.AdmitGoalFormulation(t.Context(), formulationID, owner.ID, formulation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{GoalsEnabled: new(false)}); err != nil {
		t.Fatal(err)
	}
	if value, err := s.MatchGoalFormulation(t.Context(), formulationID, owner.ID, formulation); err != nil || value.Input.ID != formulated.Input.ID {
		t.Fatal(value, err)
	}
	formulation.TailMessages = 8
	if _, err := s.MatchGoalFormulation(t.Context(), formulationID, owner.ID, formulation); !errors.Is(err, ErrConflict) {
		t.Fatal("resolved defaults replaced exact authored request", err)
	}
}

func TestReceiptMatchGoalResumeAndNormalizedHostOperation(t *testing.T) {
	s := fresh(t)
	_, owner := create(t, s, nil)
	goal := createGoalTest(t, s, owner.ID, "goal", nil, false)
	identity := session.RequestIdentity{ClientID: "client", RequestID: "resume"}
	resumed, err := s.ResumeGoal(t.Context(), identity, owner.ID, goal.Goal.GoalRef)
	if err != nil {
		t.Fatal(err)
	}
	if value, err := s.MatchGoalResume(t.Context(), identity, owner.ID, goal.Goal.GoalRef); err != nil || value.Input.ID != resumed.Input.ID {
		t.Fatal(value, err)
	}
	changed := goal.Goal.GoalRef
	changed.Revision++
	if _, err := s.MatchGoalResume(t.Context(), identity, owner.ID, changed); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	_, direct := create(t, s, nil)
	identity.RequestID = "host"
	operation := session.HostOperation{Module: "shell", Name: "run", Arguments: json.RawMessage(`{"command":"echo 9007199254740993", "timeout":20}`)}
	accepted, err := s.AdmitHostOperation(t.Context(), identity, direct.ID, operation)
	if err != nil {
		t.Fatal(err)
	}
	operation.Arguments = json.RawMessage(`{ "timeout":20,"command":"echo 9007199254740993"}`)
	if value, err := s.MatchHostOperation(t.Context(), identity, direct.ID, operation); err != nil || value.Input.ID != accepted.Input.ID {
		t.Fatal(value, err)
	}
	operation.Arguments = json.RawMessage(`{"timeout":21,"command":"echo 9007199254740993"}`)
	if _, err := s.MatchHostOperation(t.Context(), identity, direct.ID, operation); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
}
