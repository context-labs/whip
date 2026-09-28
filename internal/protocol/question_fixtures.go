package protocol

import (
	"encoding/json"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func questionFixtures(created time.Time) ([]Fixture, error) {
	question := session.Question{
		OperationID: "operation_question", SessionID: "session_root", TurnID: "turn_fixture", CellID: "cell_fixture", State: session.QuestionPending,
		Request:   session.QuestionRequest{Batch: true, Questions: []session.QuestionSet{{Question: "Choose a route", Options: []session.QuestionOption{{Label: "A", Description: "First route", Recommended: true}, {Label: "B"}}}}},
		CreatedAt: created, Deadline: created.Add(session.QuestionWait),
	}
	result := []Fixture{}
	for _, state := range []session.QuestionState{session.QuestionPending, session.QuestionAnswered, session.QuestionDismissed, session.QuestionClosed} {
		question.State = state
		question.Answers = nil
		if state != session.QuestionPending {
			question.ClosedAt = &created
		}
		switch state {
		case session.QuestionAnswered:
			question.Answers = []session.QuestionAnswer{{Answer: []string{"custom route"}}}
		case session.QuestionDismissed:
			question.Answers = []session.QuestionAnswer{{Dismissed: true}}
		case session.QuestionClosed:
			question.CloseReason = new(session.QuestionInterrupted)
		}
		raw, err := json.Marshal(QuestionFromDomain(question))
		if err != nil {
			return nil, err
		}
		result = append(result, Fixture{Type: "Question", Value: raw, Valid: true})
	}
	for _, entry := range []struct {
		typeName string
		raw      string
		valid    bool
	}{
		{"QuestionParams", `{"session_id":"session_root","operation_id":"operation_question"}`, true},
		{"QuestionsParams", `{"session_id":"session_root","pending_only":true,"limit":100}`, true},
		{"QuestionsResult", `{"items":[]}`, true},
		{"AnswerQuestionParams", `{"session_id":"session_root","operation_id":"operation_question","answers":[{"answer":["custom"],"dismissed":false}]}`, true},
		{"AnswerQuestionParams", `{"session_id":"session_root","operation_id":"operation_question","answers":[{"answer":[],"dismissed":true}]}`, true},
		{"AnswerQuestionParams", `{"session_id":"session_root","operation_id":"operation_question","answers":[]}`, false},
		{"AnswerQuestionParams", `{"session_id":"session_root","operation_id":"operation_question","answers":[{"answer":null,"dismissed":true}]}`, false},
		{"AnswerQuestionParams", `{"session_id":"session_root","operation_id":"operation_question","answers":[{"answer":["a","b","c","d","e","f","g","h"],"dismissed":false}]}`, false},
		{"QuestionsParams", `{"session_id":"session_root","pending_only":true,"limit":101}`, false},
		{"QuestionParams", `{"operation_id":"operation_question"}`, false},
	} {
		result = append(result, Fixture{Type: entry.typeName, Value: json.RawMessage(entry.raw), Valid: entry.valid})
	}
	return result, nil
}
