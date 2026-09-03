package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/context-labs/whip/internal/tools"
)

func newAskModel(multiple bool) (*model, chan askAnswer) {
	m := compactCmdModel()
	reply := make(chan askAnswer, 1)
	m.askDialog = &askDialog{
		req: tools.AskRequest{
			Question: "Which planner?",
			Options:  []tools.AskOption{{Label: "fable (Recommended)", Description: "default"}, {Label: "opus"}, {Label: "gpt-5.6-sol"}},
			Multiple: multiple,
		},
		reply: reply, picked: map[int]bool{},
	}
	return m, reply
}

func TestQuestionNumberPicks(t *testing.T) {
	m, reply := newAskModel(false)
	m.askKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3")})
	got := <-reply
	if !got.ok || len(got.answers) != 1 || got.answers[0] != "gpt-5.6-sol" {
		t.Fatalf("got %+v", got)
	}
	if m.askDialog != nil {
		t.Fatal("dialog should close after a pick")
	}
}

func TestQuestionCustomAndMulti(t *testing.T) {
	// custom row: arrow to the last row, enter, type, enter
	m, reply := newAskModel(false)
	m.askKey(tea.KeyMsg{Type: tea.KeyUp}) // wraps to the custom row
	m.askKey(tea.KeyMsg{Type: tea.KeyEnter})
	m.askKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("grok")})
	m.askKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-reply; !got.ok || got.answers[0] != "grok" {
		t.Fatalf("custom: got %+v", got)
	}

	// multi: toggle 1 and 2, enter submits both
	m, reply = newAskModel(true)
	m.askKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m.askKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m.askKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := <-reply; !got.ok || len(got.answers) != 2 {
		t.Fatalf("multi: got %+v", got)
	}

	// esc dismisses
	m, reply = newAskModel(false)
	m.askKey(tea.KeyMsg{Type: tea.KeyEsc})
	if got := <-reply; got.ok {
		t.Fatalf("esc: got %+v", got)
	}
}
