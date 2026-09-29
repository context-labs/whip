package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	QuestionCapability  = "user.ask"
	MaxQuestionsPerTurn = 32
	MaxQuestionBytes    = 256 << 10
	QuestionWait        = 5 * time.Minute
)

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Recommended bool   `json:"recommended,omitempty"`
}

type QuestionSet struct {
	Question string           `json:"question"`
	Options  []QuestionOption `json:"options"`
	Multiple bool             `json:"multiple,omitempty"`
}

// QuestionRequest is the canonical operation intent. Batch retains the caller's
// explicit batch shape even when it contains just one question.
type QuestionRequest struct {
	Questions []QuestionSet `json:"questions"`
	Batch     bool          `json:"batch"`
}

type QuestionAnswer struct {
	Answer    []string `json:"answer"`
	Dismissed bool     `json:"dismissed"`
}

type (
	QuestionState       string
	QuestionCloseReason string
)

const (
	QuestionPending     QuestionState       = "pending"
	QuestionAnswered    QuestionState       = "answered"
	QuestionDismissed   QuestionState       = "dismissed"
	QuestionClosed      QuestionState       = "closed"
	QuestionCancelled   QuestionCloseReason = "cancelled"
	QuestionExpired     QuestionCloseReason = "expired"
	QuestionInterrupted QuestionCloseReason = "interrupted"
)

// Question projects immutable intent and settlement from its ordinary operation.
// A historical pending row never represents a resumable in-memory waiter.
type Question struct {
	OperationID OperationID
	SessionID   SessionID
	TurnID      TurnID
	CellID      CellID
	Request     QuestionRequest
	State       QuestionState
	Answers     []QuestionAnswer
	CloseReason *QuestionCloseReason
	CreatedAt   time.Time
	Deadline    time.Time
	ClosedAt    *time.Time
}

func decodeQuestionJSON(raw []byte, target any) error {
	if len(raw) > MaxQuestionBytes || !utf8.Valid(raw) || !json.Valid(raw) {
		return fmt.Errorf("%w: question JSON exceeds its size limit or is invalid", ErrInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: invalid question fields", ErrInvalid)
	}
	return nil
}

// ParseQuestionRequest accepts user.ask's single-question or explicit batch form.
func ParseQuestionRequest(raw []byte) (QuestionRequest, error) {
	var fields map[string]json.RawMessage
	if err := decodeQuestionJSON(raw, &fields); err != nil || fields == nil {
		return QuestionRequest{}, fmt.Errorf("%w: question arguments must be an object", ErrInvalid)
	}
	request := QuestionRequest{}
	if batch, ok := fields["questions"]; ok {
		if len(fields) != 1 {
			return request, fmt.Errorf("%w: batch and single question fields cannot be mixed", ErrInvalid)
		}
		request.Batch = true
		if err := decodeQuestionJSON(batch, &request.Questions); err != nil {
			return request, err
		}
	} else {
		var single QuestionSet
		if err := decodeQuestionJSON(raw, &single); err != nil {
			return request, err
		}
		request.Questions = []QuestionSet{single}
	}
	for i := range request.Questions {
		question := &request.Questions[i]
		question.Question = strings.TrimSpace(question.Question)
		for j := range question.Options {
			question.Options[j].Label = strings.TrimSpace(question.Options[j].Label)
		}
	}
	return request, request.Validate()
}

func DecodeQuestionRequest(raw []byte) (QuestionRequest, error) {
	var request QuestionRequest
	if err := decodeQuestionJSON(raw, &request); err != nil {
		return request, err
	}
	return request, request.Validate()
}

func (r QuestionRequest) Validate() error {
	if len(r.Questions) < 1 || len(r.Questions) > 8 || (!r.Batch && len(r.Questions) != 1) {
		return fmt.Errorf("%w: questions must contain 1–8 entries", ErrInvalid)
	}
	for _, question := range r.Questions {
		if err := questionText(question.Question, 4096); err != nil {
			return err
		}
		if question.Question != strings.TrimSpace(question.Question) || len(question.Options) < 2 || len(question.Options) > 6 {
			return fmt.Errorf("%w: question requires trimmed text and 2–6 options", ErrInvalid)
		}
		labels := make(map[string]bool, len(question.Options))
		recommended := 0
		for _, option := range question.Options {
			if err := questionText(option.Label, 256); err != nil {
				return err
			}
			if option.Label != strings.TrimSpace(option.Label) || labels[option.Label] {
				return fmt.Errorf("%w: option labels must be trimmed and unique", ErrInvalid)
			}
			labels[option.Label] = true
			if len(option.Description) > 4096 || !utf8.ValidString(option.Description) || strings.ContainsRune(option.Description, 0) {
				return fmt.Errorf("%w: invalid option description", ErrInvalid)
			}
			if option.Recommended {
				recommended++
			}
		}
		if recommended > 1 {
			return fmt.Errorf("%w: only one option may be recommended", ErrInvalid)
		}
	}
	return questionSize(r)
}

func questionText(value string, maxBytes int) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%w: question text must be UTF-8", ErrInvalid)
	}
	return ValidateText(value, maxBytes)
}

func questionSize(value any) error {
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaxQuestionBytes {
		return fmt.Errorf("%w: question exceeds its encoded size limit", ErrInvalid)
	}
	return nil
}

// NormalizeAnswers validates free text as well as offered choices. Dismissal
// deliberately discards answer content and does not cancel the surrounding turn.
func (r QuestionRequest) NormalizeAnswers(answers []QuestionAnswer) ([]QuestionAnswer, error) {
	if len(answers) != len(r.Questions) {
		return nil, fmt.Errorf("%w: answer count must match question count", ErrInvalid)
	}
	if err := questionSize(answers); err != nil {
		return nil, err
	}
	result := make([]QuestionAnswer, len(answers))
	for i, answer := range answers {
		if answer.Dismissed {
			result[i] = QuestionAnswer{Answer: []string{}, Dismissed: true}
			continue
		}
		question := r.Questions[i]
		if len(answer.Answer) < 1 || len(answer.Answer) > 7 || (!question.Multiple && len(answer.Answer) != 1) {
			return nil, fmt.Errorf("%w: invalid number of answer entries", ErrInvalid)
		}
		seen := make(map[string]bool, len(answer.Answer))
		for _, entry := range answer.Answer {
			if err := questionText(entry, 4096); err != nil {
				return nil, err
			}
			for _, option := range question.Options {
				if entry == option.Label && seen[entry] {
					return nil, fmt.Errorf("%w: duplicate selected option", ErrInvalid)
				}
			}
			seen[entry] = true
		}
		result[i] = QuestionAnswer{Answer: append([]string(nil), answer.Answer...)}
	}
	return result, questionSize(result)
}

func (r QuestionRequest) AnswerValue(answers []QuestionAnswer) (json.RawMessage, error) {
	answers, err := r.NormalizeAnswers(answers)
	if err != nil {
		return nil, err
	}
	if !r.Batch {
		return questionValueJSON(answers[0])
	}
	dismissed := true
	for _, answer := range answers {
		dismissed = dismissed && answer.Dismissed
	}
	return questionValueJSON(struct {
		Answers   []QuestionAnswer `json:"answers"`
		Dismissed bool             `json:"dismissed"`
	}{answers, dismissed})
}

func questionValueJSON(value any) (json.RawMessage, error) {
	if err := questionSize(value); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func (r QuestionRequest) DecodeAnswers(raw []byte) ([]QuestionAnswer, error) {
	if !r.Batch {
		var answer QuestionAnswer
		if err := decodeQuestionJSON(raw, &answer); err != nil {
			return nil, err
		}
		return r.NormalizeAnswers([]QuestionAnswer{answer})
	}
	var value struct {
		Answers   []QuestionAnswer `json:"answers"`
		Dismissed bool             `json:"dismissed"`
	}
	if err := decodeQuestionJSON(raw, &value); err != nil {
		return nil, err
	}
	return r.NormalizeAnswers(value.Answers)
}
