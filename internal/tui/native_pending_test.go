package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeForgetCommand(item nativePendingEntry) string {
	return fmt.Sprintf("/pending forget %s %s %s %s", item.owner, item.identity.ClientID, item.identity.RequestID, item.digest)
}

func TestNativePendingInspectsDeletedOwnersAndChecksTombstonesWithoutReplay(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	original := nativeRecoveryCommand(t, m, "lost-ack")
	if err := m.recovery.save(original); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if _, err := original.Send(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := original.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	owner := m.owner.ID
	nativeMenuRPC[protocol.DeleteResult](t, m.connection, "sessions.delete", protocol.SessionParams{SessionID: owner})
	m.ready = false
	m.uncertain = original
	m.input.SetValue("/pending list")
	command := m.submit()
	if command == nil {
		t.Fatal("unavailable owner trapped local recovery", m.status)
	}
	result := command().(nativeControlResult)
	m.Update(result)
	if result.err != nil || !strings.Contains(m.notice, string(owner)) || !strings.Contains(m.notice, "lost-ack") || strings.Contains(m.notice, "original recovery text") {
		t.Fatal("metadata inventory failed or exposed full input", result.err, m.notice)
	}
	if result := nativeUIControl(t, m, "/pending inspect "+string(owner)); result.err != nil || !strings.Contains(m.notice, "original recovery text") {
		t.Fatal("explicit input inspection failed", result.err, m.notice)
	}
	if result := nativeUIControl(t, m, "/pending check "+string(owner)); result.err != nil || !strings.Contains(m.notice, "deleted (retained receipt)") || m.uncertain != nil {
		t.Fatal("tombstone did not clear exact recovery", result.err, m.notice, m.uncertain)
	}
	if _, err := m.recovery.read(owner); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("checked tombstone left recovery", err)
	}
	var value protocol.Session
	if err := m.connection.Call(ctx, "sessions.get", protocol.SessionParams{SessionID: owner}, &value); err == nil {
		t.Fatal("receipt inspection recreated deleted owner")
	}
}

func TestNativePendingUnknownCheckKeepsOriginalAndExplicitForgetIsOnlyLocal(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	original := nativeRecoveryCommand(t, m, "never-sent")
	if err := m.recovery.save(original); err != nil {
		t.Fatal(err)
	}
	m.uncertain = original
	m.ready = false
	if result := nativeUIControl(t, m, "/pending check "+string(m.owner.ID)); result.err != nil || !strings.Contains(m.notice, "original local intent kept") || m.uncertain == nil {
		t.Fatal(result.err, m.notice)
	}
	item, err := nativePendingIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	m.input.SetValue("still unsubmitted")
	result := m.command(nativeForgetCommand(item))().(nativeControlResult)
	m.Update(result)
	if result.err != nil || m.uncertain != nil || m.input.Value() != "still unsubmitted" {
		t.Fatal("explicit forget changed draft or failed", result.err, m.uncertain, m.input.Value())
	}
	inputs, err := m.handle.Inputs(t.Context(), "all", nil, 100)
	if err != nil || len(inputs.Items) != 0 {
		t.Fatal("check/forget resent an unaccepted input", inputs, err)
	}
	m.input.SetValue("do not send while owner unavailable")
	if m.submit() != nil || m.input.Value() == "" {
		t.Fatal("recovery availability bypassed ordinary prompt readiness")
	}
}

func TestNativePendingStaleForgetCannotClearAnotherTerminalRequest(t *testing.T) {
	m, _ := nativeUIFixture(t)
	directory := t.TempDir()
	m.recovery = nativeJournal(t, directory, m.connection.Identity())
	other := nativeJournal(t, directory, m.connection.Identity())
	first, second := nativeRecoveryCommand(t, m, "first-intent"), nativeRecoveryCommand(t, m, "second-intent")
	if err := m.recovery.save(first); err != nil {
		t.Fatal(err)
	}
	item, _ := nativePendingIdentity(first)
	command := m.command(nativeForgetCommand(item))
	if err := other.clear(first); err != nil {
		t.Fatal(err)
	}
	if err := other.save(second); err != nil {
		t.Fatal(err)
	}
	m.uncertain = second
	result := command().(nativeControlResult)
	m.Update(result)
	if result.err == nil || m.uncertain != second || m.retryControl != nil {
		t.Fatal("stale forget cleared/replayed changed intent", result, m.uncertain)
	}
	retained, err := m.recovery.restore(m.connection, m.owner.ID)
	if err != nil || retained == nil {
		t.Fatal(err)
	}
	current, _ := nativePendingIdentity(retained)
	if current.identity.RequestID != "second-intent" {
		t.Fatal(current)
	}
	if err := other.locked(func() error {
		result := nativeUIControl(t, m, nativeForgetCommand(current))
		if result.err == nil {
			t.Fatal("concurrent forget acquired another terminal's lock")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestNativePendingInventoryBoundsAndForeignRuntimeIsolation(t *testing.T) {
	m, _ := nativeUIFixture(t)
	r := nativeJournal(t, t.TempDir(), m.connection.Identity())
	command := nativeRecoveryCommand(t, m, "inventory")
	if err := r.save(command); err != nil {
		t.Fatal(err)
	}
	foreign := command.Record()
	foreign.RuntimeID = "other-runtime"
	raw, err := json.Marshal(foreign)
	if err != nil {
		t.Fatal(err)
	}
	name := (&nativeRecovery{runtime: foreign.RuntimeID}).name(m.owner.ID)
	if err := r.root.WriteFile(name, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	items, other, err := r.entries(m.connection)
	if err != nil || len(items) != 1 || other != 1 || items[0].identity.RequestID != "inventory" {
		t.Fatal(items, other, err)
	}
	for index := range nativeRecoveryCount {
		if err := r.root.WriteFile(fmt.Sprintf("%064x.json", index), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := r.entries(m.connection); err == nil {
		t.Fatal("unbounded inventory accepted")
	}
	if retained, err := r.read(m.owner.ID); err != nil || len(retained) == 0 {
		t.Fatal("refused inventory changed original", err)
	}
}

func TestNativePendingForgetAfterUnlinkSyncFailureRequiresOriginalIdentity(t *testing.T) {
	m, _ := nativeUIFixture(t)
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	command := nativeRecoveryCommand(t, m, "original-memory-only")
	m.uncertain = command
	item, _ := nativePendingIdentity(command)
	wrong := item
	wrong.digest = strings.Repeat("a", 64)
	if result := nativeUIControl(t, m, nativeForgetCommand(wrong)); result.err == nil || m.uncertain == nil {
		t.Fatal("wrong identity cleared in-memory pending input")
	}
	if result := nativeUIControl(t, m, nativeForgetCommand(item)); result.err != nil || m.uncertain != nil {
		t.Fatal("exact explicit local forget could not complete idempotent cleanup", result.err)
	}
	record := command.Record()
	if record.Accepted {
		t.Fatal("local forget mutated host admission knowledge")
	}
}

func TestNativePendingForgetDoesNotCancelAcceptedRunningInput(t *testing.T) {
	m, provider := nativeUIFixture(t)
	m.recovery = nativeJournal(t, t.TempDir(), m.connection.Identity())
	command, err := m.handle.Submission(protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "terminal", RequestID: "running-forget"}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "hold"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.recovery.save(command); err != nil {
		t.Fatal(err)
	}
	if _, err := command.Send(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("input did not run")
	}
	m.uncertain = command
	item, _ := nativePendingIdentity(command)
	if result := nativeUIControl(t, m, nativeForgetCommand(item)); result.err != nil || m.uncertain != nil {
		t.Fatal(result.err)
	}
	value, found, err := command.Check(t.Context())
	if err != nil || !found || value.Turn == nil || value.Turn.State != "running" {
		t.Fatal("local forget cancelled host work", value, err)
	}
	close(provider.release)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := command.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}
