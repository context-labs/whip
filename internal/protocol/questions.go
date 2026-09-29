package protocol

import (
	"reflect"
	"time"

	"github.com/context-labs/whip/internal/session"
	"github.com/google/jsonschema-go/jsonschema"
)

type QuestionOption struct {
	Label       string `json:"label" pattern:"^[^\\x00]{1,256}$"`
	Description string `json:"description"`
	Recommended bool   `json:"recommended"`
}

type QuestionSet struct {
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options"`
	Multiple bool             `json:"multiple"`
}

type QuestionRequest struct {
	Questions []QuestionSet `json:"questions"`
	Batch     bool          `json:"batch"`
}

type QuestionAnswer struct {
	Answer    []string `json:"answer"`
	Dismissed bool     `json:"dismissed"`
}

type Question struct {
	OperationID ID               `json:"operation_id"`
	SessionID   ID               `json:"session_id"`
	TurnID      ID               `json:"turn_id"`
	CellID      ID               `json:"cell_id"`
	Request     QuestionRequest  `json:"request"`
	State       string           `json:"state" enum:"pending,answered,dismissed,closed"`
	Answers     []QuestionAnswer `json:"answers"`
	CloseReason *string          `json:"close_reason" enum:"cancelled,expired,interrupted"`
	CreatedAt   string           `json:"created_at"`
	Deadline    string           `json:"deadline"`
	ClosedAt    *string          `json:"closed_at"`
}

type QuestionParams struct {
	SessionID   ID `json:"session_id"`
	OperationID ID `json:"operation_id"`
}

type QuestionsParams struct {
	SessionID   ID   `json:"session_id"`
	PendingOnly bool `json:"pending_only,omitempty"`
	After       *ID  `json:"after,omitempty"`
	Limit       int  `json:"limit" min:"1" max:"100"`
}

type QuestionsResult struct {
	Items []Question `json:"items"`
}

type AnswerQuestionParams struct {
	SessionID   ID               `json:"session_id"`
	OperationID ID               `json:"operation_id"`
	Answers     []QuestionAnswer `json:"answers"`
}

func QuestionFromDomain(value session.Question) Question {
	result := Question{
		OperationID: ID(value.OperationID), SessionID: ID(value.SessionID), TurnID: ID(value.TurnID), CellID: ID(value.CellID),
		State: string(value.State), Request: QuestionRequest{Questions: []QuestionSet{}, Batch: value.Request.Batch}, Answers: []QuestionAnswer{},
		CreatedAt: value.CreatedAt.Format(time.RFC3339Nano), Deadline: value.Deadline.Format(time.RFC3339Nano), ClosedAt: timeString(value.ClosedAt),
	}
	if value.CloseReason != nil {
		result.CloseReason = new(string(*value.CloseReason))
	}
	for _, question := range value.Request.Questions {
		set := QuestionSet{Question: question.Question, Options: []QuestionOption{}, Multiple: question.Multiple}
		for _, option := range question.Options {
			set.Options = append(set.Options, QuestionOption{Label: option.Label, Description: option.Description, Recommended: option.Recommended})
		}
		result.Request.Questions = append(result.Request.Questions, set)
	}
	for _, answer := range value.Answers {
		result.Answers = append(result.Answers, QuestionAnswer{Answer: append([]string{}, answer.Answer...), Dismissed: answer.Dismissed})
	}
	return result
}

func (p AnswerQuestionParams) DomainAnswers() []session.QuestionAnswer {
	result := make([]session.QuestionAnswer, len(p.Answers))
	for i, answer := range p.Answers {
		result[i] = session.QuestionAnswer{Answer: append([]string{}, answer.Answer...), Dismissed: answer.Dismissed}
	}
	return result
}

// Native patterns keep standalone validators self-contained. Go applies the
// stricter UTF-8 byte/aggregate limits and request-dependent answer rules.
func questionSchema(schema *jsonschema.Schema, t reflect.Type) {
	boundedArray := func(field string, minimum, maximum int) {
		value := schema.Properties[field]
		value.Type, value.Types = "array", nil
		value.MinItems, value.MaxItems = &minimum, &maximum
	}
	const chunk = `[^\x00]{1000}`
	const tail = `[^\x00]{0,1000}`
	const text = `(?:` + tail + `|` + chunk + tail + `|` + chunk + chunk + tail + `|` + chunk + chunk + chunk + tail + `|` + chunk + chunk + chunk + chunk + `[^\x00]{0,96})`
	switch t {
	case reflect.TypeFor[QuestionOption]():
		schema.Properties["description"].Pattern = "^" + text + "$"
	case reflect.TypeFor[QuestionSet]():
		boundedArray("options", 2, 6)
		schema.Properties["question"].Pattern = "^" + text + "$"
		schema.Properties["question"].Not = &jsonschema.Schema{Pattern: `^[\s\p{Z}]*$`}
	case reflect.TypeFor[QuestionRequest]():
		boundedArray("questions", 1, 8)
	case reflect.TypeFor[QuestionAnswer]():
		boundedArray("answer", 0, 7)
		schema.Properties["answer"].Items.Pattern = "^" + text + "$"
	case reflect.TypeFor[AnswerQuestionParams]():
		boundedArray("answers", 1, 8)
	case reflect.TypeFor[QuestionsResult]():
		boundedArray("items", 0, 100)
	case reflect.TypeFor[Question]():
		boundedArray("answers", 0, 8)
		variants := make([]*jsonschema.Schema, 0, 4)
		for _, state := range []string{"pending", "answered", "dismissed", "closed"} {
			variant := schema.CloneSchemas()
			variant.Properties["state"] = &jsonschema.Schema{Type: "string", Enum: []any{state}}
			variant.Properties["close_reason"] = &jsonschema.Schema{Type: "null"}
			variant.Properties["closed_at"] = &jsonschema.Schema{Type: "string"}
			if state == "pending" || state == "closed" {
				variant.Properties["answers"].MaxItems = new(0)
			} else {
				variant.Properties["answers"].MinItems = new(1)
			}
			switch state {
			case "pending":
				variant.Properties["closed_at"] = &jsonschema.Schema{Type: "null"}
			case "closed":
				variant.Properties["close_reason"] = &jsonschema.Schema{Type: "string", Enum: []any{"cancelled", "expired", "interrupted"}}
			}
			variants = append(variants, variant)
		}
		*schema = jsonschema.Schema{OneOf: variants}
	}
}
