package store

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/context-labs/whip/internal/session"
)

func outputDraft(id session.MessageID, text string) session.MessageDraft {
	return session.MessageDraft{ID: id, Role: session.Assistant, Parts: []session.Part{{Type: "text", Text: text}}}
}

func TestTurnOutputProjectsLastAssistantWithCapturedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output.db")
	s := openTest(t, path)
	_, owner := create(t, s, nil)
	owner, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Output: &session.OutputPolicy{Schema: json.RawMessage(`{"type":"integer"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	submit(t, s, owner.ID, "exact")
	turn := claim(t, s, owner.ID).Turn
	if value, err := s.TurnOutput(t.Context(), turn.ID); !errors.Is(err, ErrBusy) || value != nil {
		t.Fatalf("running output=%+v err=%v", value, err)
	}
	if _, err := s.AppendMessage(t.Context(), turn.ID, outputDraft("invalid-candidate", "not JSON")); err != nil {
		t.Fatal(err)
	}
	owner, err = s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Output: &session.OutputPolicy{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{outputDraft("final-exact", "```json\n9007199254740993\n```")}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openTest(t, path)
	value, err := s.TurnOutput(t.Context(), turn.ID)
	if err != nil || value == nil || value.TurnID != turn.ID || value.MessageID != "final-exact" || string(value.Value) != "9007199254740993" {
		t.Fatalf("captured output=%+v err=%v", value, err)
	}
	value.Value[0] = '0'
	if again, err := s.TurnOutput(t.Context(), turn.ID); err != nil || again == nil || string(again.Value) != "9007199254740993" {
		t.Fatalf("read aliased output=%+v err=%v", again, err)
	}
	submit(t, s, owner.ID, "cleared")
	cleared := claim(t, s, owner.ID).Turn
	if _, err := s.Finish(t.Context(), cleared.ID, session.Succeeded, nil, []session.MessageDraft{outputDraft("free-text", "ordinary prose")}); err != nil {
		t.Fatal(err)
	}
	if value, err := s.TurnOutput(t.Context(), cleared.ID); err != nil || value != nil {
		t.Fatalf("cleared contract output=%+v err=%v", value, err)
	}
	if err := s.DeleteSubtree(t.Context(), owner.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []session.TurnID{turn.ID, cleared.ID, "nonexistent"} {
		if value, err := s.TurnOutput(t.Context(), id); !errors.Is(err, ErrNotFound) || value != nil {
			t.Fatalf("missing output=%+v err=%v", value, err)
		}
	}
}

func TestTurnOutputNullAndAbsentContracts(t *testing.T) {
	for _, test := range []struct {
		name, schema, text, want string
	}{
		{"null value", `{"type":"null"}`, "null", "null"},
		{"multipart object", `{"type":"object"}`, ` { "answer": 42 } `, `{"answer":42}`},
		{"JSON null contract", `null`, "free text", ""},
		{"absent contract", "", "free text", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			if test.schema != "" {
				if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Output: &session.OutputPolicy{Schema: json.RawMessage(test.schema)}}); err != nil {
					t.Fatal(err)
				}
			}
			submit(t, s, owner.ID, "output")
			turn := claim(t, s, owner.ID).Turn
			draft := outputDraft("candidate", test.text)
			if test.name == "multipart object" {
				draft.Parts = []session.Part{{Type: "text", Text: ` { "answer": `}, {Type: "text", Text: `42 } `}}
			}
			if _, err := s.Finish(t.Context(), turn.ID, session.Succeeded, nil, []session.MessageDraft{draft}); err != nil {
				t.Fatal(err)
			}
			value, err := s.TurnOutput(t.Context(), turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if value != nil {
					t.Fatalf("unexpected output=%+v", value)
				}
			} else if value == nil || string(value.Value) != test.want {
				t.Fatalf("output=%+v want=%s", value, test.want)
			}
		})
	}
}

func TestTurnOutputUnsuccessfulAndInvalidStoredResult(t *testing.T) {
	for _, state := range []session.TurnState{session.Failed, session.Cancelled, session.Interrupted, session.Succeeded} {
		t.Run(string(state), func(t *testing.T) {
			s := fresh(t)
			_, owner := create(t, s, nil)
			if _, err := s.UpdateConfiguration(t.Context(), owner.ID, owner.ConfigRevision, session.ConfigPatch{Output: &session.OutputPolicy{Schema: json.RawMessage(`{"type":"number"}`)}}); err != nil {
				t.Fatal(err)
			}
			submit(t, s, owner.ID, "output")
			turn := claim(t, s, owner.ID).Turn
			if state == session.Cancelled {
				if _, err := s.CancelTurn(t.Context(), turn.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.TurnOutput(t.Context(), turn.ID); !errors.Is(err, ErrBusy) {
					t.Fatalf("cancelling output err=%v", err)
				}
			}
			if _, err := s.Finish(t.Context(), turn.ID, state, nil, []session.MessageDraft{outputDraft("invalid", "not JSON")}); err != nil {
				t.Fatal(err)
			}
			value, err := s.TurnOutput(t.Context(), turn.ID)
			if state == session.Succeeded {
				if err == nil || value != nil {
					t.Fatalf("invalid successful source was hidden: %+v %v", value, err)
				}
			} else if err != nil || value != nil {
				t.Fatalf("unsuccessful output=%+v err=%v", value, err)
			}
		})
	}
}
