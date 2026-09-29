package session

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestQuestionShapesAndAnswers(t *testing.T) {
	single := `{"question":" Choose a route ","options":[{"label":" A ","description":"first","recommended":true},{"label":"B"}]}`
	for _, batch := range []bool{false, true} {
		raw := single
		if batch {
			raw = `{"questions":[` + single + `]}`
		}
		request, err := ParseQuestionRequest([]byte(raw))
		if err != nil || request.Batch != batch || len(request.Questions) != 1 || request.Questions[0].Question != "Choose a route" || request.Questions[0].Options[0].Label != "A" || !request.Questions[0].Options[0].Recommended {
			t.Fatalf("shape: %+v %v", request, err)
		}
		encoded, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeQuestionRequest(encoded)
		if err != nil || !reflect.DeepEqual(request, decoded) {
			t.Fatalf("canonical intent: %+v %v", decoded, err)
		}
		for _, answer := range []QuestionAnswer{{Answer: []string{"A"}}, {Answer: []string{"custom route"}}, {Answer: []string{"ignored"}, Dismissed: true}} {
			value, err := request.AnswerValue([]QuestionAnswer{answer})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(value), `"answers"`) != batch || strings.Contains(string(value), "ignored") {
				t.Fatalf("result shape: %s", value)
			}
			answers, err := request.DecodeAnswers(value)
			if err != nil || len(answers) != 1 || answers[0].Dismissed != answer.Dismissed {
				t.Fatalf("result decode: %+v %v", answers, err)
			}
		}
	}
}

func TestQuestionValidationAndBounds(t *testing.T) {
	base := QuestionRequest{Questions: []QuestionSet{{Question: "Choose", Options: []QuestionOption{{Label: "A"}, {Label: "B"}}}}}
	for _, raw := range []string{
		`null`, `{}`, `{"questions":[]}`, `{"questions":null}`, `{"questions":[],"question":"x"}`,
		`{"question":"x","options":[{"label":"A"},{"label":"B"}],"extra":1}`,
		`{"question":"x","options":[{"label":"A","extra":1},{"label":"B"}]}`,
		`{"question":"x","options":[{"label":"A","recommended":true},{"label":"B","recommended":true}]}`,
		`{"question":"x","options":[{"label":" A "},{"label":"A"}]}`,
		`{"question":"x","options":[{"label":"A"}]}`,
		`{"question":"x","options":[{"label":"A"},{"label":"B"}],"multiple":"yes"}`,
	} {
		if _, err := ParseQuestionRequest([]byte(raw)); !errors.Is(err, ErrInvalid) {
			t.Errorf("accepted invalid input %s: %v", raw, err)
		}
	}
	for _, mutate := range []func(*QuestionRequest){
		func(r *QuestionRequest) { r.Questions[0].Question = strings.Repeat("x", 4097) },
		func(r *QuestionRequest) { r.Questions[0].Question = "\xff" },
		func(r *QuestionRequest) { r.Questions[0].Options[0].Label = strings.Repeat("x", 257) },
		func(r *QuestionRequest) { r.Questions[0].Options[0].Description = strings.Repeat("x", 4097) },
		func(r *QuestionRequest) {
			r.Questions[0].Options = append(r.Questions[0].Options, make([]QuestionOption, 5)...)
		},
		func(r *QuestionRequest) { r.Batch = true; r.Questions = append(r.Questions, make([]QuestionSet, 8)...) },
	} {
		raw, _ := json.Marshal(base)
		changed, _ := DecodeQuestionRequest(raw)
		mutate(&changed)
		if err := changed.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid canonical question: %+v %v", changed, err)
		}
	}
	for _, entries := range [][]string{nil, {" "}, {"A", "B"}, {strings.Repeat("x", 4097)}, {"\xff"}, {"x\x00y"}} {
		if _, err := base.NormalizeAnswers([]QuestionAnswer{{Answer: entries}}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted invalid answer: %+v %v", entries, err)
		}
	}
	base.Questions[0].Multiple = true
	if _, err := base.NormalizeAnswers([]QuestionAnswer{{Answer: []string{"A", "custom"}}}); err != nil {
		t.Fatal(err)
	}
	for _, entries := range [][]string{{"A", "A"}, {"a", "b", "c", "d", "e", "f", "g", "h"}} {
		if _, err := base.NormalizeAnswers([]QuestionAnswer{{Answer: entries}}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted duplicate or unbounded selections: %v", err)
		}
	}
	if _, err := base.NormalizeAnswers([]QuestionAnswer{{Dismissed: true, Answer: []string{strings.Repeat("x", MaxQuestionBytes)}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbounded dismissed payload: %v", err)
	}
	base.Batch = true
	base.Questions[0].Question = strings.Repeat("\n", 4095) + "x"
	for i := range base.Questions[0].Options {
		base.Questions[0].Options[i].Description = strings.Repeat("\n", 4096)
	}
	for range 4 {
		base.Questions[0].Options = append(base.Questions[0].Options, QuestionOption{Label: string(rune('C' + len(base.Questions[0].Options))), Description: strings.Repeat("\n", 4096)})
	}
	base.Questions[0].Question = "q"
	for range 7 {
		base.Questions = append(base.Questions, base.Questions[0])
	}
	if err := base.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("escaped aggregate request size was unbounded: %v", err)
	}
}
