package protocol

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/session"
)

func TestQuestionContractStateAndBounds(t *testing.T) {
	created := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	base := QuestionFromDomain(session.Question{
		OperationID: "question", SessionID: "root", TurnID: "turn", CellID: "cell", State: session.QuestionPending,
		Request:   session.QuestionRequest{Questions: []session.QuestionSet{{Question: "Choose", Options: []session.QuestionOption{{Label: "A"}, {Label: "B"}}}}},
		CreatedAt: created, Deadline: created.Add(session.QuestionWait),
	})
	for _, test := range []struct {
		name string
		edit func(*Question)
	}{
		{"pending answer", func(q *Question) { q.Answers = []QuestionAnswer{{Answer: []string{"A"}}} }},
		{"pending close", func(q *Question) { q.ClosedAt = new(created.Format(time.RFC3339Nano)) }},
		{"closed without reason", func(q *Question) { q.State = "closed"; q.ClosedAt = new(created.Format(time.RFC3339Nano)) }},
		{"answered without answers", func(q *Question) { q.State = "answered"; q.ClosedAt = new(created.Format(time.RFC3339Nano)) }},
		{"empty questions", func(q *Question) { q.Request.Questions = []QuestionSet{} }},
		{"too many questions", func(q *Question) {
			for range 8 {
				q.Request.Questions = append(q.Request.Questions, q.Request.Questions[0])
			}
		}},
		{"oversized label", func(q *Question) { q.Request.Questions[0].Options[0].Label = strings.Repeat("x", 257) }},
		{"oversized question", func(q *Question) { q.Request.Questions[0].Question = strings.Repeat("x", 4097) }},
		{"empty options", func(q *Question) { q.Request.Questions[0].Options = nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, _ := json.Marshal(base)
			var changed Question
			if err := json.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			test.edit(&changed)
			raw, _ = json.Marshal(changed)
			if err := Validate("Question", raw); err == nil {
				t.Fatalf("invalid state or count accepted: %s", raw)
			}
		})
	}
	for _, length := range []int{1, 1000, 1001, 2000, 3001, 4001, 4096} {
		base.Request.Questions[0].Question = strings.Repeat("x", length)
		raw, _ := json.Marshal(base)
		if err := Validate("Question", raw); err != nil {
			t.Fatalf("native bounded pattern rejected valid length %d: %v", length, err)
		}
	}
}
