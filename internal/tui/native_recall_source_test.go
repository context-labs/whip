package tui

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeGlobalRecallUsesRealHumanTextWithoutForeignParts(t *testing.T) {
	m, _ := nativeUIFixture(t)
	original, _ := nativeHistoryInputFixture(t, m)
	var root protocol.CreateTreeResult
	if err := m.connection.Call(t.Context(), "trees.create", protocol.CreateTreeParams{CreationID: "recall-other", Engine: "starlark", Definition: m.owner.Definition, WorkingDirectory: t.TempDir(), Overrides: protocol.ConfigPatch{AutomaticTitle: new(false), Model: &m.owner.Configuration.Model}}, &root); err != nil {
		t.Fatal(err)
	}
	if err := m.attachSession(*root.Root); err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("original composer")
	saved := m.captureDraft()
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if command == nil || m.input.Value() != saved.text {
		t.Fatal("history read did not retain draft")
	}
	m.Update(command())
	parts, err := m.promptParts(m.input.Value())
	if err != nil || len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != strings.TrimSpace(original.Parts[0].Text) || len(m.images) != 0 || m.draftDesign != nil {
		t.Fatalf("global recall transferred authority or changed text: %+v %v", parts, err)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if !reflect.DeepEqual(saved, m.captureDraft()) {
		t.Fatal("down did not restore original unsent composer")
	}
	activity, err := m.handle.Activity(t.Context())
	if err != nil || activity.QueuedInputCount != 0 || activity.ActiveTurn != nil {
		t.Fatal("recall admitted host work", activity, err)
	}
}

func TestNativeGlobalRecallDiscardsChangedDraftAndStaleOwner(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.input.SetValue("draft")
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.input.SetValue("edited while loading")
	m.Update(command())
	if m.input.Value() != "edited while loading" || m.recall != nil {
		t.Fatal("late history read overwrote edit")
	}
	m.recallUpAt = time.Time{}
	_, command = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m.generation++
	m.recall = nil
	m.Update(command())
	if m.input.Value() != "edited while loading" {
		t.Fatal("old owner response overwrote current composer")
	}
}

func TestNativeGlobalRecallScanAndByteBoundsNeverReturnPartialInput(t *testing.T) {
	calls := 0
	entries, partial, err := collectNativeRecall(func(before *protocol.Counter) (protocol.InputTextPage, error) {
		calls++
		next := protocol.Counter(9007199254740995 - calls*500)
		if calls > 1 && (before == nil || *before != next+500) {
			t.Fatal("exact cursor was rounded", before)
		}
		return protocol.InputTextPage{Items: []protocol.InputText{}, ScannedCount: 500, NextCursor: &next}, nil
	})
	if err != nil || !partial || calls != 8 || len(entries) != 0 {
		t.Fatal("empty pages stalled or exceeded bound", calls, partial, err)
	}
	calls = 0
	entries, partial, err = collectNativeRecall(func(*protocol.Counter) (protocol.InputTextPage, error) {
		calls++
		page := protocol.InputTextPage{ScannedCount: 2}
		for i := range 2 {
			page.Items = append(page.Items, protocol.InputText{SessionID: "foreign", InputID: protocol.ID(fmt.Sprintf("input-%d-%d", calls, i)), Ordinal: protocol.Counter(1000 - calls*2 - i), Text: fmt.Sprintf("%d%d", calls, i) + strings.Repeat("x", (120<<10)-2)})
		}
		page.NextCursor = new(page.Items[1].Ordinal)
		return page, nil
	})
	if err != nil || !partial || calls != 8 || len(entries) != 2 || len(entries[0].text) != 120<<10 || len(entries[1].text) != 120<<10 {
		t.Fatal("bounded text was truncated or overretained", len(entries), calls, err)
	}
	if _, _, err := collectNativeRecall(func(*protocol.Counter) (protocol.InputTextPage, error) {
		return protocol.InputTextPage{ScannedCount: 1, Items: []protocol.InputText{{Ordinal: 2}}, NextCursor: new(protocol.Counter(3))}, nil
	}); err == nil {
		t.Fatal("nonadvancing source accepted")
	}
}
