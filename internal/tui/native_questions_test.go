package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeQuestionBatchPreservesMultiFreeformAndDismissal(t *testing.T) {
	m, provider := nativeUIFixture(t)
	provider.codes = map[string]string{"ask": `answer = user.ask(questions=[{"question":"Pick routes", "options":[{"label":"A", "recommended":True},{"label":"B"}], "multiple":True}, {"question":"Second", "options":[{"label":"C"},{"label":"D"}]}])` + "\nprint(answer)"}
	input := nativeUISubmit(t, m, "ask")
	decision := awaitNativeDecisions(t, m)
	if decision.question == nil || m.decision.form == nil || !strings.Contains(m.View().Content, "Recommended") {
		t.Fatal("question form missing", m.View().Content)
	}
	if command := nativeDecisionKey(t, m, "enter"); command != nil || m.decision.form.index != 0 {
		t.Fatal("empty multi-selection submitted")
	}
	nativeDecisionKey(t, m, "space")
	nativeDecisionKey(t, m, "f")
	m.Update(tea.PasteMsg{Content: "custom route"})
	nativeDecisionKey(t, m, "enter")
	if m.input.Value() != "" || m.decision.form.custom != "custom route" {
		t.Fatal("answer pasted into conversation prompt", m.input.Value())
	}
	nativeDecisionKey(t, m, "esc")
	if m.decision != nil {
		t.Fatal("question dialog did not hide")
	}
	nativeDecisionKey(t, m, "tab")
	if m.decision == nil || m.decision.form.custom != "custom route" || !m.decision.form.selected[0] {
		t.Fatal("hiding discarded an unsent answer")
	}
	if command := nativeDecisionKey(t, m, "enter"); command != nil || m.decision.form.index != 1 {
		t.Fatal("batch did not retain first answer")
	}
	command := nativeDecisionKey(t, m, "d")
	if result := finishNativeDecision(t, m, command); result.err != nil {
		t.Fatal(result.err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := input.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	m.Update(nativeControlResult{mutation: true, err: errors.New("lost acknowledgement"), retry: command})
	if result := finishNativeDecision(t, m, m.command("/retry")); result.err != nil {
		t.Fatal("exact answer retry after turn settlement failed", result.err)
	}
	var result protocol.Question
	if err := m.connection.Call(ctx, "questions.get", protocol.QuestionParams{SessionID: decision.owner, OperationID: decision.id}, &result); err != nil {
		t.Fatal(err)
	}
	want := []protocol.QuestionAnswer{{Answer: []string{"A", "custom route"}}, {Answer: []string{}, Dismissed: true}}
	if result.State != "answered" || !equalNativeAnswers(result.Answers, want) {
		t.Fatal("batch provenance/answers changed", result)
	}
}

func TestNativeSingleQuestionSelectionAndCanonicalClosure(t *testing.T) {
	m, provider := nativeUIFixture(t)
	provider.codes = map[string]string{"ask": `user.ask(question="Choose",options=[{"label":"A"},{"label":"B"}])`}
	nativeUISubmit(t, m, "ask")
	decision := awaitNativeDecisions(t, m)
	nativeDecisionKey(t, m, "down")
	if result := finishNativeDecision(t, m, nativeDecisionKey(t, m, "enter")); result.err != nil {
		t.Fatal(result.err)
	}
	var result protocol.Question
	if err := m.connection.Call(t.Context(), "questions.get", protocol.QuestionParams{SessionID: decision.owner, OperationID: decision.id}, &result); err != nil || len(result.Answers) != 1 || len(result.Answers[0].Answer) != 1 || result.Answers[0].Answer[0] != "B" {
		t.Fatal(result, err)
	}
	if m.decision != nil {
		t.Fatal("recorded question retained active dialog")
	}
}
