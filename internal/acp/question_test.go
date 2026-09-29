package acp

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func TestQuestionAnswerPayloadConformsToProtocol(t *testing.T) {
	for _, answers := range [][]protocol.QuestionAnswer{{{Answer: []string{"Postgres"}}}, {{Answer: []string{}, Dismissed: true}}, {{Answer: []string{"Postgres"}}, {Answer: []string{}, Dismissed: true}}} {
		raw, err := json.Marshal(protocol.AnswerQuestionParams{SessionID: "owner", OperationID: "question", Answers: answers})
		if err != nil {
			t.Fatal(err)
		}
		if err := protocol.Validate("AnswerQuestionParams", raw); err != nil {
			t.Fatalf("answer=%s: %v", raw, err)
		}
	}
}

func TestBridgeQuestionOptionCardinality(t *testing.T) {
	for _, count := range []int{0, 1, 2, 6, 7} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			options := make([]protocol.QuestionOption, count)
			for i := range options {
				options[i] = protocol.QuestionOption{Label: "option " + strconv.Itoa(i)}
			}
			raw, err := json.Marshal(protocol.Question{OperationID: "operation", SessionID: "owner", TurnID: "turn", CellID: "cell", State: "pending", Request: protocol.QuestionRequest{Questions: []protocol.QuestionSet{{Question: "Choose", Options: options}}}, Answers: []protocol.QuestionAnswer{}, CreatedAt: time.Now().Format(time.RFC3339Nano), Deadline: time.Now().Add(time.Minute).Format(time.RFC3339Nano)})
			if err != nil {
				t.Fatal(err)
			}
			err = protocol.Validate("Question", raw)
			if (err == nil) != (count >= 2 && count <= 6) {
				t.Fatalf("canonical question cardinality%d: %v", count, err)
			}
		})
	}
}
