package tui

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeCompactionAdmitsHelperAndUndoesSelectionWithoutRewritingHistory(t *testing.T) {
	m, _ := nativeUIFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, text := range []string{"first original input ", "second original input ", "third original input ", "fourth original input ", "fifth original input ", "sixth original input "} {
		value := nativeUISubmit(t, m, strings.Repeat(text, 50))
		if _, err := value.command.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		nativeUIRead(t, m)
	}
	before, err := m.handle.History(ctx, protocol.HistoryPageParams{Direction: "forward", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	command := m.command("/compact")
	if command == nil {
		t.Fatal(m.status)
	}
	if _, err := m.recovery.read(m.owner.ID); err != nil {
		t.Fatal("compaction was not journalled before dispatch", err)
	}
	admitted := command().(nativeSubmission)
	m.Update(admitted)
	if admitted.err != nil || admitted.admission.Input == nil || admitted.admission.Input.Kind != "compact" {
		t.Fatal(admitted.err, admitted.admission)
	}
	completed, err := admitted.command.Wait(ctx)
	if err != nil || completed.Turn == nil || completed.Turn.State != "succeeded" {
		t.Fatal(completed, err)
	}
	head := nativeMenuRPC[protocol.ContextHead](t, m.connection, "context.head", protocol.SessionParams{SessionID: m.owner.ID})
	if head.CompactionID == nil {
		t.Fatal("real compaction did not select its summary", head)
	}
	if value := nativeUIControl(t, m, "/compact log"); value.err != nil || !strings.Contains(m.notice, string(*head.CompactionID)) || !strings.Contains(m.notice, "selected") {
		t.Fatal(value.err, m.notice)
	}
	undone := nativeUIControl(t, m, "/compact retry")
	if undone.err != nil {
		t.Fatal(undone.err)
	}
	current := nativeMenuRPC[protocol.ContextHead](t, m.connection, "context.head", protocol.SessionParams{SessionID: m.owner.ID})
	if current.CompactionID != nil || current.Revision != head.Revision+1 {
		t.Fatal(current)
	}
	if repeated := undone.retry().(nativeControlResult); repeated.err == nil {
		t.Fatal("old context CAS silently rebased")
	}
	retained := nativeMenuRPC[protocol.CompactionResult](t, m.connection, "context.compaction", protocol.CompactionParams{SessionID: m.owner.ID, CompactionID: *head.CompactionID})
	if retained.Text != "Compacted fixture history." {
		t.Fatal("undo deleted immutable summary", retained)
	}
	after, err := m.handle.History(ctx, protocol.HistoryPageParams{Direction: "forward", Limit: 100})
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("compaction undo rewrote raw conversation", err)
	}
	usage, err := m.handle.Usage(ctx)
	if err != nil || usage.Attempts.Settled != 7 || usage.Attempts.InFlight != 0 {
		t.Fatal("undo made another model request", usage, err)
	}
}

func TestNativeCompactionSettingsPreserveBothScopesAndReportPartialCAS(t *testing.T) {
	f := newNativeMenuFixture(t)
	m, err := newNativeModel(t.Context(), f.connection, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.work.close)
	m.Update(m.Init()())
	if !m.ready {
		t.Fatal(m.status)
	}
	initial := m.owner.Configuration
	changed := nativeUIControl(t, m, "/compact configured fixture")
	if changed.err != nil {
		t.Fatal(changed.err)
	}
	inventory := nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	if inventory.CompactionModel == nil || inventory.CompactionModel.Provider != "fixture" || inventory.CompactionModel.Name != "configured" || !reflect.DeepEqual(inventory.CompactionModel, m.owner.Configuration.Compaction.Model) || !reflect.DeepEqual(initial.Model, m.owner.Configuration.Model) {
		t.Fatal("helper setting changed the conversation model or lost saved scope", inventory, m.owner.Configuration)
	}
	if reset := nativeUIControl(t, m, "/compact off"); reset.err != nil {
		t.Fatal(reset.err)
	}
	inventory = nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	if inventory.CompactionModel != nil || m.owner.Configuration.Compaction.Model != nil || m.owner.Configuration.Compaction.ThresholdPercent != initial.Compaction.ThresholdPercent {
		t.Fatal("off disabled compaction instead of clearing helper selection", inventory, m.owner.Configuration)
	}
	before := m.owner
	newPolicy := m.owner.Configuration.Compaction
	newPolicy.ThresholdPercent = 70
	newer := nativeMenuRPC[protocol.Session](t, f.connection, "sessions.configure", protocol.UpdateConfigurationParams{SessionID: before.ID, ExpectedRevision: before.ConfigRevision, Patch: protocol.ConfigPatch{Compaction: &newPolicy}})
	partial := nativeUIControl(t, m, "/compact summary fixture")
	if partial.err == nil || !strings.Contains(m.status, "host helper default was saved") || m.retryControl != nil {
		t.Fatal("partial conflict was hidden or rebased", partial.err, m.status)
	}
	inventory = nativeMenuRPC[protocol.ProviderInventory](t, f.connection, "providers.list", protocol.EmptyParams{})
	current := nativeMenuRPC[protocol.Session](t, f.connection, "sessions.get", protocol.SessionParams{SessionID: before.ID})
	if inventory.CompactionModel == nil || inventory.CompactionModel.Name != "summary" || !reflect.DeepEqual(current.Configuration.Compaction, newer.Configuration.Compaction) {
		t.Fatal("partial settings result not truthful", inventory, current)
	}
	if repeated := partial.retry().(nativeControlResult); repeated.err == nil {
		t.Fatal("session CAS was automatically rebuilt")
	}
	if status := nativeUIControl(t, m, "/compact status"); status.err != nil || m.owner.ConfigRevision != newer.ConfigRevision || !strings.Contains(m.notice, "70%") {
		t.Fatal(status.err, m.notice)
	}
	if invalid := nativeUIControl(t, m, "/compact unknown missing-route"); invalid.err == nil {
		t.Fatal("unconfigured helper route accepted")
	}
	if f.requests.Load() != 0 {
		t.Fatal("settings silently probed provider", f.requests.Load())
	}
}
