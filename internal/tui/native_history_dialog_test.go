package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeHistoryInputFixture(t *testing.T, m *nativeModel) (protocol.SubmitParams, []byte) {
	t.Helper()
	data := nativeImageFixture(t)
	ref, err := m.handle.PutContent(t.Context(), "redraft-image", "image/png", data)
	if err != nil {
		t.Fatal(err)
	}
	contextRef, err := m.handle.PutContent(t.Context(), "redraft-context", "text/plain", []byte("original selected page evidence"))
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "fixture", RequestID: "redraft-input"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "original\trequest\r\n"}, {Type: "content", ReferenceID: contextRef.ID}, {Type: "text", Text: "selected image:"}, {Type: "content", ReferenceID: ref.ID}}, DesignContext: &protocol.DesignContext{ContextAttachmentID: contextRef.ID, ScreenshotAttachmentID: &ref.ID, Elements: []protocol.DesignContextElement{{Label: "Save"}}, ElementCount: 1, PageTitle: "Original page"}}
	command, err := m.handle.Submission(params)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	value, err := command.Send(ctx)
	if err != nil || value.Input == nil {
		t.Fatal(value, err)
	}
	value, err = command.Wait(ctx)
	if err != nil || value.Turn == nil || value.Turn.State != "succeeded" {
		t.Fatal(value, err)
	}
	nativeUIRead(t, m)
	return params, data
}

func TestNativeHistoryPickerRequiresExplicitStopAndStagesOriginalOnExactRewind(t *testing.T) {
	m, _ := nativeUIFixture(t)
	params, _ := nativeHistoryInputFixture(t, m)
	m.command("/rewind")
	if m.historyDialog == nil || len(m.historyDialog.entries) != 1 {
		t.Fatal("opening input picker missing", m.status)
	}
	if _, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); command != nil || !strings.Contains(m.historyDialog.status, "explicitly") {
		t.Fatal("picker silently stopped active owner", m.historyDialog)
	}
	owner, err := m.handle.Get(t.Context())
	if err != nil || owner.Lifecycle != "active" {
		t.Fatal(owner, err)
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	nativeUIControl(t, m, "/stop")
	m.input.SetValue("unrelated composer remains")
	m.commandKeepingDraft("/rewind")
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal(m.status)
	}
	result := command().(nativeControlResult)
	if result.err != nil || result.redraft == nil || m.redraft != nil || m.input.Value() != "unrelated composer remains" {
		t.Fatal("unacknowledged rewind changed draft", result.err, m.status)
	}
	// Losing the first observation cannot rebase the original request.
	retry := result.retry().(nativeControlResult)
	m.Update(retry)
	if retry.err != nil || m.redraft == nil || m.input.Value() != "unrelated composer remains" {
		t.Fatal("matching receipt erased destination draft", retry.err, m.status)
	}
	m.redraftCommand("restore")
	if m.redraft == nil || m.input.Value() != "unrelated composer remains" {
		t.Fatal("restore silently replaced nonempty composer")
	}
	m.redraftCommand("replace")
	parts, err := m.promptParts(m.input.Value())
	if err != nil || !reflect.DeepEqual(parts, params.Parts) || !reflect.DeepEqual(m.draftDesign, params.DesignContext) || m.redraft != nil {
		t.Fatal("original parts/metadata lost", parts, m.draftDesign, err)
	}
	nativeUIRead(t, m)
	activity, err := m.handle.Activity(t.Context())
	if err != nil || activity.Lifecycle != "stopped" || activity.ActiveTurn != nil || activity.QueuedInputCount != 0 || len(m.history.messages) != 0 {
		t.Fatal("redraft restarted or submitted work", activity, err)
	}
	if !strings.Contains(m.input.Value(), "Attachment") {
		t.Fatal("restored nonimage content was labelled as an image", m.input.Value())
	}
}

func TestNativeHistoryForkTitleRetainsCapturedAttachmentHandlesWithoutUploadOrPrompt(t *testing.T) {
	m, _ := nativeUIFixture(t)
	params, original := nativeHistoryInputFixture(t, m)
	source := m.owner
	m.command("/rewind")
	m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	if m.historyDialog == nil || !m.historyDialog.fork || m.historyDialog.keep != 0 {
		t.Fatal("fork title did not capture selected prefix")
	}
	m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	m.Update(tea.PasteMsg{Content: "My chosen fork"})
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal(m.status)
	}
	result := command().(nativeControlResult)
	if result.err != nil || result.attach == nil || result.redraft == nil || m.owner.ID != source.ID {
		t.Fatal(result, m.status)
	}
	// A known destination can already have an unsent terminal-owned draft.
	m.drafts = map[protocol.ID]nativeDraft{result.attach.ID: {text: "existing destination draft"}}
	_, read := m.Update(result)
	m.Update(read())
	if m.owner.ID == source.ID || m.redraft == nil || m.input.Value() != "existing destination draft" || len(m.history.messages) != 0 {
		t.Fatal("fork attached or replaced the wrong draft", m.status, m.input.Value())
	}
	ref, data, err := m.handle.ReadContent(t.Context(), params.Parts[3].ReferenceID)
	if err != nil || ref.SessionID != m.owner.ID || !bytes.Equal(data, original) || m.attachment != nil || m.attachmentBusy {
		t.Fatal("fork did not preserve canonical owned handle", ref, err)
	}
	m.redraftCommand("replace")
	parts, err := m.promptParts(m.input.Value())
	if err != nil || !reflect.DeepEqual(parts, params.Parts) || !reflect.DeepEqual(m.draftDesign, params.DesignContext) {
		t.Fatal(parts, m.draftDesign, err)
	}
	activity, err := m.handle.Activity(t.Context())
	if err != nil || activity.ActiveTurn != nil || activity.QueuedInputCount != 0 {
		t.Fatal("fork redraft executed without explicit send", activity, err)
	}
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	command = m.submit()
	if command == nil {
		t.Fatal(m.status)
	}
	retained, err := m.recovery.restore(m.connection, m.owner.ID)
	if err != nil || retained == nil {
		t.Fatal(err)
	}
	var pending protocol.SubmitParams
	if err := json.Unmarshal(retained.Record().Params, &pending); err != nil || !reflect.DeepEqual(pending.Parts, params.Parts) || !reflect.DeepEqual(pending.DesignContext, params.DesignContext) || pending.SessionID != m.owner.ID {
		t.Fatal("explicit send changed original metadata or owner", pending, err)
	}
	// Prepared but unsent input proves redraft itself never admitted a turn.
	activity, err = m.handle.Activity(t.Context())
	if err != nil || activity.ActiveTurn != nil || activity.QueuedInputCount != 0 {
		t.Fatal(activity, err)
	}
}

func TestNativeHistoryDialogRejectsStaleConfigAndFrozenHistory(t *testing.T) {
	m, _ := nativeUIFixture(t)
	nativeHistoryInputFixture(t, m)
	m.command("/fork")
	captured := m.historyDialog
	selection := m.owner.Configuration.Model
	selection.Name = "changed-elsewhere"
	nativeMenuRPC[protocol.Session](t, m.connection, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: m.owner.ID, ExpectedRevision: m.owner.ConfigRevision, Patch: protocol.ConfigPatch{Model: &selection}})
	_, command := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	result := command().(nativeControlResult)
	m.Update(result)
	if result.err == nil || result.attach != nil || m.retryControl != nil || m.redraft != nil || m.owner.ConfigRevision != captured.owner.ConfigRevision {
		t.Fatal("stale fork dialog changed capture or retried", result, m.status)
	}
	nativeUIRead(t, m)
	nativeUIControl(t, m, "/stop")
	m.command("/rewind")
	frozen := m.historyDialog.snapshot
	nativeMenuRPC[protocol.HistoryEdit](t, m.connection, "sessions.rewind", protocol.RewindParams{EditID: "concurrent", SessionID: m.owner.ID, ExpectedRevision: frozen.Revision, ObservedThrough: frozen.ThroughSequence, KeepThrough: frozen.ThroughSequence})
	_, command = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	result = command().(nativeControlResult)
	m.Update(result)
	if result.err == nil || result.reset || m.redraft != nil || m.retryControl != nil {
		t.Fatal("stale rewind dialog applied a draft or rebased", result)
	}
}

func TestNativeHistoryDialogBoundsUnknownPrefixAndDraftMetadataIsolation(t *testing.T) {
	m := nativeSelectionFixture(t)
	m.ready = true
	m.history.snapshot = protocol.HistorySnapshot{Revision: 3, ThroughSequence: 6}
	message := nativeMessage(5, "user", "original")
	message.OpeningInput = true
	m.history.messages, m.history.earlier = []protocol.Message{message}, true
	m.owner.Lifecycle = "stopped"
	m.command("/rewind")
	if m.historyDialog == nil || m.historyDialog.entries[0].known {
		t.Fatal("missing preceding page fabricated a boundary")
	}
	if m.historyDialogKey(tea.KeyPressMsg{Code: tea.KeyEnter}) != nil || !strings.Contains(m.historyDialog.status, "preceding") {
		t.Fatal(m.historyDialog.status)
	}
	m.historyDialog = nil
	m.browse = &nativeBrowse{transcript: nativeTranscript{snapshot: m.history.snapshot, messages: []protocol.Message{message}}, earlier: true}
	m.command("/rewind")
	if m.historyDialog.entries[0].known {
		t.Fatal("forward page tail fabricated its unseen preceding boundary")
	}
	m.historyDialog.fork = true
	m.historyDialog.title = ""
	m.historyDialogPaste(strings.Repeat("馬", 256))
	m.historyDialogPaste("x")
	if m.historyDialog.title != strings.Repeat("馬", 256) || !strings.Contains(m.historyDialog.status, "refused") {
		t.Fatal("unbounded title accepted")
	}
	for _, size := range [][2]int{{40, 8}, {80, 24}} {
		for row := range strings.SplitSeq(m.historyDialog.view(size[0], size[1]), "\n") {
			if ansi.StringWidth(row) > size[0] {
				t.Fatal("history dialog exceeded frame width")
			}
		}
	}
	m.draftDesign = &protocol.DesignContext{ContextAttachmentID: "original"}
	m.input.SetValue("held")
	m.switchDraft("other")
	if m.drafts[m.owner.ID].design == nil || m.drafts[m.owner.ID].bytes() <= len("held") {
		t.Fatal("draft dropped metadata or omitted its bytes")
	}
}

func TestNativeHistoryDesignDraftEditsRequireExplicitMetadataRemoval(t *testing.T) {
	m := nativeSelectionFixture(t)
	m.ready = true
	m.owner.Configuration.Model = protocol.ModelSelection{Provider: "scripted", Name: "scripted"}
	m.draftDesign = &protocol.DesignContext{ContextAttachmentID: "missing"}
	m.input.SetValue("edited text without the required attachment")
	if m.prompt(m.input.Value(), "auto") != nil || !strings.Contains(m.status, "clear-context") {
		t.Fatal("removed design reference silently changed provenance", m.status)
	}
	before := m.input.Value()
	m.redraftCommand("clear-context")
	if m.draftDesign != nil || m.input.Value() != before {
		t.Fatal("explicit metadata removal changed the text")
	}
	m.draftDesign = &protocol.DesignContext{ContextAttachmentID: "old"}
	m.command("/help")
	if m.input.Value() != "" || m.draftDesign != nil {
		t.Fatal("discarded composer left hidden metadata on a future draft")
	}
}
