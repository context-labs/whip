package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func TestNativeForkUsesCapturedPrefixAndRetryReceiptWithoutFakeExecution(t *testing.T) {
	m, _ := nativeUIFixture(t)
	source := m.owner
	for _, text := range []string{"first", "second"} {
		input := nativeUISubmit(t, m, text)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, err := input.command.Wait(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	nativeUIRead(t, m)
	command := m.command("/fork-at 2 Selected prefix")
	if command == nil {
		t.Fatal(m.status)
	}
	result := command().(nativeControlResult)
	if result.err != nil || result.attach == nil || result.retry == nil {
		t.Fatal(result.err, result)
	}
	_, read := m.Update(result)
	m.Update(read())
	if m.owner.ID == source.ID || m.owner.ParentID != nil || m.owner.WorkingDirectory != source.WorkingDirectory || len(m.history.messages) != 2 {
		t.Fatal("fork selection or native owner was lost", m.owner, m.history.messages)
	}
	for _, message := range m.history.messages {
		if message.Source == nil || message.Source.SessionID != source.ID || message.InputID != nil || message.TurnID != nil {
			t.Fatal("imported message acquired fake local execution", message)
		}
	}
	fork := m.owner
	var stopped protocol.Session
	if err := m.connection.Call(t.Context(), "sessions.lifecycle", protocol.LifecycleParams{SessionID: source.ID, Lifecycle: "stopped"}, &stopped); err != nil {
		t.Fatal(err)
	}
	var deleted protocol.DeleteResult
	if err := m.connection.Call(t.Context(), "sessions.delete", protocol.SessionParams{SessionID: source.ID}, &deleted); err != nil || !deleted.Deleted {
		t.Fatal(err, deleted)
	}
	retried := result.retry().(nativeControlResult)
	if retried.err != nil || retried.attach == nil || retried.attach.ID != fork.ID {
		t.Fatal("exact retry depended on deleted source", retried.err, retried)
	}
	if err := m.connection.Call(t.Context(), "sessions.lifecycle", protocol.LifecycleParams{SessionID: fork.ID, Lifecycle: "stopped"}, &stopped); err != nil {
		t.Fatal(err)
	}
	if err := m.connection.Call(t.Context(), "sessions.delete", protocol.SessionParams{SessionID: fork.ID}, &deleted); err != nil {
		t.Fatal(err)
	}
	retried = result.retry().(nativeControlResult)
	if retried.err != nil || retried.attach != nil || !strings.Contains(retried.label, "deleted") {
		t.Fatal("fork retry recreated deleted destination", retried.err, retried)
	}
}

func TestNativeRewindKeepsExactGroupPrefixAndRejectsStaleOrSplitHistory(t *testing.T) {
	m, _ := nativeUIFixture(t)
	for _, text := range []string{"one", "two"} {
		input := nativeUISubmit(t, m, text)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, err := input.command.Wait(ctx)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	nativeUIRead(t, m)
	if cmd := m.command("/rewind 2"); cmd != nil || !strings.Contains(m.status, "requires a stopped") {
		t.Fatal("rewind silently changed active lifecycle")
	}
	if result := nativeUIControl(t, m, "/stop"); result.err != nil {
		t.Fatal(result.err)
	}
	if result := nativeUIControl(t, m, "/rewind 1"); result.err == nil || m.retryControl != nil {
		t.Fatal("split group accepted or definite refusal became uncertain")
	}
	stale := m.command("/rewind 0")
	if stale == nil {
		t.Fatal(m.status)
	}
	// Another terminal can mutate history after this terminal captured a CAS.
	var edit protocol.HistoryEdit
	err := m.connection.Call(t.Context(), "sessions.rewind", protocol.RewindParams{EditID: "other-terminal", SessionID: m.owner.ID, ExpectedRevision: m.history.snapshot.Revision, ObservedThrough: m.history.snapshot.ThroughSequence, KeepThrough: 2}, &edit)
	if err != nil {
		t.Fatal(err)
	}
	result := stale().(nativeControlResult)
	m.Update(result)
	if result.err == nil || m.retryControl != nil {
		t.Fatal("stale rewind rebased automatically", result)
	}
	nativeUIRead(t, m)
	if len(m.history.messages) != 2 {
		t.Fatal("external exact prefix not observed")
	}
	result = nativeUIControl(t, m, "/rewind 0")
	if result.err != nil || m.ready {
		t.Fatal(result.err, m.status)
	}
	nativeUIRead(t, m)
	if len(m.history.messages) != 0 || m.owner.Lifecycle != "stopped" {
		t.Fatal("rewind did not preserve stopped owner")
	}
	if repeated := result.retry().(nativeControlResult); repeated.err != nil {
		t.Fatal("exact rewind receipt retry failed", repeated.err)
	}
}
