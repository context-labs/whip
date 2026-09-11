package acp

import (
	"encoding/json"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
)

func TestQuestionAnswerPayloadConformsToProtocol(t *testing.T) {
	for _, tt := range []struct {
		name   string
		answer questionAnswer
	}{
		{"single", questionAnswer{ID: "q1", Answer: []string{"Postgres"}}},
		{"dismissed", questionAnswer{ID: "q1", Dismissed: true}},
		{"batch", questionAnswer{ID: "q1", Answers: []protocol.QuestionAnswerEntry{
			{Answer: []string{"Postgres"}}, {Dismissed: true},
		}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			payload, err := json.Marshal(tt.answer)
			if err != nil {
				t.Fatal(err)
			}
			if err := protocol.ValidateRuntime("question.answer", payload); err != nil {
				t.Fatalf("question.answer payload %s: %v", payload, err)
			}
		})
	}
}
