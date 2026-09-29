package tui

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func nativeRecallUp(m *nativeModel) {
	m.recallUpAt = time.Time{}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
}

func TestNativeRecallEdgesRestoreOriginalDraftAndPreserveMultilineCursor(t *testing.T) {
	m := nativeSelectionFixture(t)
	m.rememberDraft(nativeDraft{text: "older"})
	m.rememberDraft(nativeDraft{text: "newer"})
	m.input.SetValue("first\nsecond")
	m.input.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.input.Line() != 0 || m.recall != nil || m.input.Value() != "first\nsecond" {
		t.Fatal("up did not stay inside multiline input")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.recall != nil {
		t.Fatal("held up key fell through visual edge into history")
	}
	nativeRecallUp(m)
	if m.input.Value() != "newer" {
		t.Fatal("newest local input absent", m.input.Value())
	}
	nativeRecallUp(m)
	if m.input.Value() != "older" {
		t.Fatal("older input absent", m.input.Value())
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.input.Value() != "first\nsecond" || m.recall != nil {
		t.Fatal("down lost original multiline composer", m.input.Value())
	}
	m.input.SetValue(strings.Repeat("word ", 50))
	m.sizeInput()
	m.input.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if m.recall != nil || m.input.Value() == "newer" {
		t.Fatal("soft-wrap cursor movement recalled history")
	}
}

func TestNativeRecallBoundsExcludeCredentialsAndHistoryChangesKeepDraft(t *testing.T) {
	m := nativeSelectionFixture(t)
	for i := range 40 {
		m.rememberDraft(nativeDraft{text: fmt.Sprintf("%02d", i) + strings.Repeat("x", 64<<10)})
	}
	bytes := 0
	for _, draft := range m.recallLocal {
		bytes += draft.bytes()
	}
	if len(m.recallLocal) > 32 || bytes > 1<<20 || len(m.recallLocal) == 0 {
		t.Fatal("recall cache is not bounded", len(m.recallLocal), bytes)
	}
	for _, command := range []string{"/auth inference secret", "  /connect route secret", "/setup route secret"} {
		if m.rememberDraft(nativeDraft{text: command}) {
			t.Fatal("credential command entered recall")
		}
	}
	m.input.SetValue("new unsent")
	nativeRecallUp(m)
	current := m.input.Value()
	m.history.snapshot.Revision++
	nativeRecallUp(m)
	if m.input.Value() != current || m.recall != nil || !strings.Contains(m.status, "History changed") {
		t.Fatal("history revision change rebased recalled draft")
	}
}

func TestNativeEscapeClearRecallsExactOriginalPartsWithoutCancellingOrSending(t *testing.T) {
	m, _ := nativeUIFixture(t)
	params, _ := nativeHistoryInputFixture(t, m)
	if !m.restoreParts(params.Parts) {
		t.Fatal("fixture original not representable")
	}
	m.draftDesign = params.DesignContext
	original := m.captureDraft()
	for range 2 {
		if _, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc}); command != nil {
			t.Fatal("draft escape sent a host command")
		}
	}
	if m.input.Value() != "" || len(m.recallLocal) != 1 {
		t.Fatal("double escape did not retain then clear draft")
	}
	nativeRecallUp(m)
	parts, err := m.promptParts(m.input.Value())
	if err != nil || !reflect.DeepEqual(params.Parts, parts) || !reflect.DeepEqual(m.draftDesign, params.DesignContext) || !reflect.DeepEqual(m.captureDraft(), original) {
		t.Fatal("recall changed original parts or metadata", parts, err)
	}
	activity, err := m.handle.Activity(t.Context())
	if err != nil || activity.ActiveTurn != nil || activity.QueuedInputCount != 0 {
		t.Fatal("draft clear/recall created host work", activity, err)
	}
	m.applyDraft(nativeDraft{})
	m.escapeAt = time.Time{}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.historyDialog == nil {
		t.Fatal("idle double escape did not open canonical rewind picker")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m.Update(tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl})
	if !strings.Contains(m.status, "stopped") || m.owner.Lifecycle != "active" {
		t.Fatal("Ctrl+K bypassed explicit stop prerequisite", m.status)
	}
}

func TestNativeCtrlCCancelsCapturedTurnButNeverRetargetsOrCancelsOnDetach(t *testing.T) {
	m, p := nativeUIFixture(t)
	input := nativeUISubmit(t, m, "hold")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal("fixture prompt did not start")
	}
	nativeUIRead(t, m)
	target := *m.activity.ActiveTurn
	key := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	m.Update(key)
	m.activity.ActiveTurn.ID = "different-new-turn"
	if _, command := m.Update(key); command != nil || m.interruptTarget != "different-new-turn" {
		t.Fatal("second Ctrl+C retargeted a newly observed turn")
	}
	m.activity.ActiveTurn = &target
	m.Update(key)
	_, command := m.Update(key)
	if command == nil {
		t.Fatal("second Ctrl+C did not cancel exact captured turn")
	}
	result := command().(nativeCancelled)
	m.Update(result)
	if result.err != nil || result.turn != target.ID {
		t.Fatal(result)
	}
	value, err := input.command.Wait(ctx)
	if err != nil || value.Turn == nil || value.Turn.State != "cancelled" {
		t.Fatal(value, err)
	}
	m.activity.ActiveTurn = nil
	m.Update(key)
	_, command = m.Update(key)
	if command == nil {
		t.Fatal("idle double Ctrl+C did not detach")
	}
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatal("idle key requested something other than detach")
	}
}

func TestNativeEscapeLeavesRunningChildWithoutCancellingIt(t *testing.T) {
	m, p := nativeUIFixture(t)
	root := m.owner
	child := nativeAgentChild(t, m, root.ID, "escape-child")
	if err := m.attachSession(child); err != nil {
		t.Fatal(err)
	}
	nativeUIRead(t, m)
	m.input.SetValue("hold")
	prepared := m.prompt("hold", "queue")
	if prepared == nil {
		t.Fatal(m.status)
	}
	input := prepared().(nativeSubmission)
	m.Update(input)
	if input.err != nil {
		t.Fatal(input.err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal("child prompt did not start")
	}
	nativeUIRead(t, m)
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if command == nil {
		t.Fatal(m.status)
	}
	_, read := m.Update(command())
	m.Update(read())
	if m.owner.ID != root.ID || len(m.recallLocal) != 0 {
		t.Fatal("Escape did not restore exact root or retained child recall")
	}
	value, found, err := input.command.Check(ctx)
	if err != nil || !found || value.Turn == nil || value.Turn.State != "running" {
		t.Fatal("leaving child cancelled its host work", value, err)
	}
	close(p.release)
	if _, err := input.command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}
