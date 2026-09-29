package tui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/client"
	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeDirectShellUsesJournalPermissionAndExactReceipt(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	if result := nativeUIControl(t, m, "/permissions"); result.err != nil {
		t.Fatal(result.err)
	}
	if result := nativeUIControl(t, m, "/permissions mode automatic"); result.err != nil {
		t.Fatal(result.err)
	}
	text := "  printf 'once\\n' >> direct.txt\n\nprintf '  original output\\n'  "
	// Human shell work does not require a model selection in the composer.
	m.owner.Configuration.Model = protocol.ModelSelection{}
	m.preferences.CollapsePaste = new(true)
	m.input.SetValue("!")
	m.pasteText(text)
	command := m.submit()
	if command == nil {
		t.Fatal(m.status)
	}
	raw, err := m.recovery.read(m.owner.ID)
	if err != nil {
		t.Fatal("shell dispatched without a durable original request", err)
	}
	var record client.InputRecord
	if err := json.Unmarshal(raw, &record); err != nil || record.Method != "tool.call" {
		t.Fatal(record, err)
	}
	var params protocol.CallHostToolParams
	if err := json.Unmarshal(record.Params, &params); err != nil || params.SessionID != m.owner.ID {
		t.Fatal(params, err)
	}
	arguments, err := base64.StdEncoding.DecodeString(params.Operation.ArgumentsBase64)
	var decoded map[string]string
	if err != nil || json.Unmarshal(arguments, &decoded) != nil || decoded["command"] != text {
		t.Fatal("shell command changed before dispatch", string(arguments), err)
	}
	admitted := command().(nativeSubmission)
	m.Update(admitted)
	if admitted.err != nil || admitted.admission.Input == nil || admitted.admission.Input.Kind != "host_operation" {
		t.Fatal(admitted.err, admitted.admission.Input)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	settled, err := admitted.command.Wait(ctx)
	if err != nil || settled.Turn == nil || settled.Turn.State != "succeeded" {
		t.Fatal(settled.Turn, err)
	}
	if _, err := admitted.command.Retry(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(m.owner.WorkingDirectory, "direct.txt"))
	if err != nil || string(data) != "once\n" {
		t.Fatal("exact retry repeated the shell effect", string(data), err)
	}
	if _, err := m.recovery.read(m.owner.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("acknowledged input remained in journal", err)
	}
}

func TestNativeEffortKeepsSelectionAndRejectsStaleOwnerConfiguration(t *testing.T) {
	m, _ := nativeUIFixture(t)
	before := m.owner
	result := nativeUIControl(t, m, "/effort off")
	if result.err != nil || m.owner.Configuration.Model.Provider != before.Configuration.Model.Provider || m.owner.Configuration.Model.Name != before.Configuration.Model.Name || m.owner.Configuration.Model.Effort != "off" {
		t.Fatal(result.err, m.owner.Configuration.Model)
	}
	command := m.command("/effort default")
	if command == nil {
		t.Fatal(m.status)
	}
	selection := m.owner.Configuration.Model
	selection.Name = "newer-model"
	changed := nativeMenuRPC[protocol.Session](t, m.connection, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: m.owner.ID, ExpectedRevision: m.owner.ConfigRevision, Patch: protocol.ConfigPatch{Model: &selection}})
	stale := command().(nativeControlResult)
	m.Update(stale)
	if stale.err == nil || m.retryControl != nil {
		t.Fatal("stale effort overwrote a newer selection or became replayable", stale.err)
	}
	actual, err := m.handle.Get(t.Context())
	if err != nil || actual.ConfigRevision != changed.ConfigRevision || actual.Configuration.Model.Name != "newer-model" {
		t.Fatal(actual, err)
	}
}

func TestNativeInspectionAndReportArePassiveAndDoNotExposeConversation(t *testing.T) {
	m, _ := nativeUIFixture(t)
	result := nativeUIControl(t, m, "/context-doctor")
	if result.err != nil || !strings.Contains(m.notice, "No captured attempt") {
		t.Fatal(result.err, m.notice)
	}
	inputs, err := m.handle.Inputs(t.Context(), "all", nil, 100)
	if err != nil || len(inputs.Items) != 0 {
		t.Fatal("inspection admitted work", inputs, err)
	}
	t.Setenv("WHIP_FAKE_API_SECRET", "do-not-include-secret")
	t.Setenv("TERM_PROGRAM", "fixture\x1b]52;c;payload\a\nterminal")
	m.input.SetValue("private conversation draft")
	report := m.nativeReport()
	if strings.Contains(report, "private conversation") || strings.Contains(report, "do-not-include-secret") || strings.ContainsRune(report, '\x1b') || strings.Contains(report, m.owner.WorkingDirectory) || strings.Contains(report, string(m.owner.ID)) {
		t.Fatal("report exposed private state or terminal control")
	}
	if m.input.Value() != "private conversation draft" {
		t.Fatal("report changed draft")
	}
}

func TestNativeExportPagesBeyondVisibleWindowAndPublishesPrivately(t *testing.T) {
	m, _ := nativeUIFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for index := range 40 {
		input := nativeUISubmit(t, m, fmt.Sprintf("export message %02d", index))
		if _, err := input.command.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	page, err := m.handle.History(ctx, protocol.HistoryPageParams{Direction: "backward", Limit: 64})
	if err != nil || m.history.replace(page) != nil || len(m.history.messages) != 64 {
		t.Fatal("could not select the bounded tail window", err, len(m.history.messages))
	}
	path := filepath.Join(t.TempDir(), "full transcript.md")
	if result := nativeUIControl(t, m, "/export "+path); result.err != nil {
		t.Fatal(result.err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Count(string(data), "## user ·") != 40 || strings.Count(string(data), "## assistant ·") != 40 || !strings.Contains(string(data), "export message 00") || !strings.Contains(string(data), "export message 39") {
		t.Fatal("export omitted captured history", err, len(data))
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(info, err)
	}
	stopped, stop := context.WithCancel(t.Context())
	stop()
	if err := exportNativeTranscript(stopped, m.connection, m.owner.ID, path); err == nil {
		t.Fatal("cancelled export published")
	}
	again, err := os.ReadFile(path)
	if err != nil || string(again) != string(data) {
		t.Fatal("failed export replaced previous file", err)
	}
	temporary, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".whip-export-*"))
	if err != nil || len(temporary) != 0 {
		t.Fatal("export retained temporary files", temporary, err)
	}
}
