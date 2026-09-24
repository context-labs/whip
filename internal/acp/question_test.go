package acp

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/context-labs/whip/internal/protocol"
	"github.com/context-labs/whip/internal/session"
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

func TestBridgeQuestionOptionCardinality(t *testing.T) {
	for _, count := range []int{0, 1, 2, 6, 7} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			backend := newFakeBackend(t)
			client := &fakeACPClient{answer: "0"}
			fixture := newACPFixture(t, backend, client)
			fixture.initialize(t)
			id := fixture.newSession(t)
			options := make([]session.QuestionOption, count)
			for i := range options {
				options[i] = session.QuestionOption{Label: "option " + strconv.Itoa(i)}
			}
			payload, err := json.Marshal(pendingQuestion{QuestionID: "cardinality", Question: "Choose", Options: options})
			if err != nil {
				t.Fatal(err)
			}
			fixture.bridge.mu.Lock()
			s := fixture.bridge.sessions[id]
			fixture.bridge.mu.Unlock()
			fixture.bridge.handleQuestion(s, payload)
			backend.mu.Lock()
			root := backend.roots[string(id)]
			backend.mu.Unlock()
			root.mu.Lock()
			answer := root.lastAnswer
			root.mu.Unlock()
			client.mu.Lock()
			defer client.mu.Unlock()
			if answer.ID != "cardinality" {
				t.Fatalf("missing answer: %+v", answer)
			}
			if count < 2 || count > 6 {
				if len(client.perms) != 0 || !answer.Dismissed || len(answer.Answer) != 0 {
					t.Fatalf("invalid cardinality prompted or selected: prompts=%+v answer=%+v", client.perms, answer)
				}
				return
			}
			if len(client.perms) != 1 || len(client.perms[0].Options) != count+1 {
				t.Fatalf("permission options = %+v", client.perms)
			}
			for i, option := range client.perms[0].Options[:count] {
				if string(option.OptionId) != strconv.Itoa(i) || option.Name != options[i].Label {
					t.Fatalf("option %d = %+v", i, option)
				}
			}
			if string(client.perms[0].Options[count].OptionId) != optDismiss || answer.Dismissed ||
				len(answer.Answer) != 1 || answer.Answer[0] != options[0].Label {
				t.Fatalf("dismiss option or selected answer incorrect: prompts=%+v answer=%+v", client.perms, answer)
			}
		})
	}
}
