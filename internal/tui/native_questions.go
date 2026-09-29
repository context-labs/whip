package tui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

type nativeQuestionForm struct {
	index, cursor int
	answers       []protocol.QuestionAnswer
	selected      map[int]bool
	custom        string
	input         textinput.Model
	editing       bool
}

func newNativeDecision(value nativeDecision, width int) *nativeDecisionDialog {
	dialog := &nativeDecisionDialog{value: value}
	if value.question != nil {
		input := textinput.New()
		input.CharLimit = 4096
		input.SetWidth(max(width-6, 1))
		input.Placeholder = "Other answer"
		dialog.form = &nativeQuestionForm{answers: make([]protocol.QuestionAnswer, len(value.question.Request.Questions)), selected: map[int]bool{}, input: input}
	}
	return dialog
}

func (m *nativeModel) questionKey(key tea.KeyPressMsg) tea.Cmd {
	dialog, form := m.decision, m.decision.form
	question := dialog.value.question.Request.Questions[form.index]
	if form.editing {
		switch key.String() {
		case "esc":
			form.editing = false
			form.input.Blur()
		case "enter":
			value := strings.TrimSpace(form.input.Value())
			if value == "" || len(value) > 4096 {
				m.status = "Other answer must contain 1–4096 UTF-8 bytes."
				return nil
			}
			if !question.Multiple {
				form.selected = map[int]bool{}
			}
			form.custom, form.editing = value, false
			form.input.Blur()
		default:
			var command tea.Cmd
			form.input, command = form.input.Update(key)
			return command
		}
		return nil
	}
	switch strings.ToLower(key.String()) {
	case "up", "k":
		form.cursor = (form.cursor + len(question.Options) - 1) % len(question.Options)
	case "down", "j":
		form.cursor = (form.cursor + 1) % len(question.Options)
	case "space":
		if !question.Multiple {
			form.selected, form.custom = map[int]bool{}, ""
		}
		form.selected[form.cursor] = !form.selected[form.cursor]
	case "f":
		form.editing = true
		form.input.SetValue(form.custom)
		return form.input.Focus()
	case "left", "backspace":
		if form.index > 0 {
			form.index--
			form.restore(dialog.value.question.Request.Questions[form.index])
			dialog.offset = 0
		}
	case "d":
		form.answers[form.index] = protocol.QuestionAnswer{Answer: []string{}, Dismissed: true}
		return m.advanceQuestion()
	case "enter":
		if !question.Multiple && form.custom == "" {
			form.selected = map[int]bool{form.cursor: true}
		}
		answer := []string{}
		for i, option := range question.Options {
			if form.selected[i] {
				answer = append(answer, option.Label)
			}
		}
		if form.custom != "" && !slices.Contains(answer, form.custom) {
			answer = append(answer, form.custom)
		}
		if len(answer) == 0 {
			m.status = "Select at least one answer, provide Other with F, or explicitly dismiss with D."
			return nil
		}
		form.answers[form.index] = protocol.QuestionAnswer{Answer: answer}
		return m.advanceQuestion()
	}
	return nil
}

func (form *nativeQuestionForm) restore(question protocol.QuestionSet) {
	form.cursor, form.selected, form.custom = 0, map[int]bool{}, ""
	for _, value := range form.answers[form.index].Answer {
		found := false
		for i, option := range question.Options {
			if option.Label == value {
				form.selected[i], found = true, true
			}
		}
		if !found {
			form.custom = value
		}
	}
}

func (m *nativeModel) advanceQuestion() tea.Cmd {
	dialog, form := m.decision, m.decision.form
	if form.index+1 < len(form.answers) {
		form.index++
		form.restore(dialog.value.question.Request.Questions[form.index])
		dialog.offset = 0
		return nil
	}
	params := protocol.AnswerQuestionParams{SessionID: dialog.value.owner, OperationID: dialog.value.id, Answers: slices.Clone(form.answers)}
	for i := range params.Answers {
		params.Answers[i].Answer = slices.Clone(params.Answers[i].Answer)
	}
	text := m.input.Value()
	command := m.control("Answer question", true, func(ctx context.Context) nativeControlResult {
		var result protocol.Question
		err := m.connection.Call(ctx, "questions.answer", params, &result)
		if err == nil && (result.OperationID != params.OperationID || result.SessionID != params.SessionID || result.State != "answered" && result.State != "dismissed" || !equalNativeAnswers(result.Answers, params.Answers)) {
			err = errors.New("question settlement did not match original answer")
		}
		return nativeControlResult{label: "Question response recorded", decisionID: params.OperationID, err: err}
	})
	m.input.SetValue(text)
	return command
}

func equalNativeAnswers(first, second []protocol.QuestionAnswer) bool {
	return slices.EqualFunc(first, second, func(a, b protocol.QuestionAnswer) bool {
		return a.Dismissed == b.Dismissed && slices.Equal(a.Answer, b.Answer)
	})
}

func (form *nativeQuestionForm) view(request protocol.QuestionRequest) string {
	question := request.Questions[form.index]
	var text strings.Builder
	fmt.Fprintf(&text, "Question %d of %d\n\n%s\n\n", form.index+1, len(request.Questions), question.Question)
	if question.Multiple {
		text.WriteString("Select one or more answers.\n")
	}
	for i, option := range question.Options {
		cursor, picked := " ", " "
		if i == form.cursor {
			cursor = ">"
		}
		if form.selected[i] {
			picked = "x"
		}
		fmt.Fprintf(&text, "%s [%s] %s", cursor, picked, option.Label)
		if option.Recommended {
			text.WriteString(" (Recommended)")
		}
		if option.Description != "" {
			fmt.Fprintf(&text, "\n      %s", option.Description)
		}
		text.WriteByte('\n')
	}
	if form.editing {
		fmt.Fprintf(&text, "\nOther: %s\n", form.input.View())
	} else if form.custom != "" {
		fmt.Fprintf(&text, "\nOther: %s\n", form.custom)
	}
	return text.String()
}
