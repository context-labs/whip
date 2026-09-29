package tui

import (
	"bytes"
	"context"
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

func nativeJournal(t *testing.T, directory string, runtime protocol.ID) *nativeRecovery {
	t.Helper()
	value, err := openNativeRecovery(directory, runtime)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = value.root.Close() })
	return value
}

func nativeRecoveryCommand(t *testing.T, m *nativeModel, id string) *client.InputCommand {
	t.Helper()
	value, err := m.handle.Submission(protocol.SubmitParams{Identity: protocol.RequestIdentity{ClientID: "terminal", RequestID: protocol.ID(id)}, Source: "user", Parts: []protocol.Part{{Type: "text", Text: "original recovery text"}}})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestNativeRecoveryPersistsBeforeSendAndRestartChecksWithoutReplay(t *testing.T) {
	m, _ := nativeUIFixture(t)
	directory := t.TempDir()
	m.recovery = nativeJournal(t, directory, m.connection.Identity())
	m.input.SetValue("original recovery text")
	unsent := m.submit()
	if unsent == nil {
		t.Fatal(m.status)
	}
	original, err := m.recovery.read(m.owner.ID)
	if err != nil {
		t.Fatal("no durable record before a command could execute", err)
	}
	// Simulate detachment before the prepared send executes. Reopening observes
	// that exact record but cannot admit it with Send or a read-only Check.
	m.work.close()
	second, err := newNativeModel(t.Context(), m.connection, m.owner)
	if err != nil {
		t.Fatal(err)
	}
	second.recovery = nativeJournal(t, directory, m.connection.Identity())
	t.Cleanup(second.work.close)
	if err := second.attachSession(m.owner); err != nil || second.uncertain == nil {
		t.Fatal(second.status, err)
	}
	restored := second.uncertain
	if _, err := restored.Send(t.Context()); err == nil {
		t.Fatal("restored record silently sent")
	}
	result := second.sendInput(restored, "check")().(nativeSubmission)
	second.Update(result)
	if !result.uncertain || result.admission.Input != nil {
		t.Fatal(result)
	}
	before, err := second.handle.History(t.Context(), protocol.HistoryPageParams{Direction: "forward", Limit: 100})
	if err != nil || len(before.Messages) != 0 {
		t.Fatal("read-only recovery admitted work", before, err)
	}
	result = second.command("/retry")().(nativeSubmission)
	second.Update(result)
	if result.err != nil || result.recoveryError != nil || second.uncertain != nil || result.admission.Input == nil {
		t.Fatal(result, second.status)
	}
	if _, err := second.recovery.read(second.owner.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("confirmed acceptance left pending journal", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := restored.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := second.handle.History(ctx, protocol.HistoryPageParams{Direction: "forward", Limit: 100})
	if err != nil || len(page.Messages) != 2 || page.Messages[0].Parts[0].Text != "original recovery text" {
		t.Fatal(page, err)
	}
	// Simulate a process lost after host acceptance but before local cleanup.
	// Restoring the original immutable intent only checks the accepted receipt.
	if err := second.recovery.root.WriteFile(second.recovery.name(m.owner.ID), original, 0o600); err != nil {
		t.Fatal(err)
	}
	accepted, err := second.recovery.restore(m.connection, m.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	checked := second.sendInput(accepted, "check")().(nativeSubmission)
	second.Update(checked)
	if checked.err != nil || checked.recoveryError != nil || checked.admission.Input == nil || checked.admission.Input.ID != result.admission.Input.ID {
		t.Fatal("receipt recovery changed original admission", checked)
	}
	if _, err := second.recovery.read(second.owner.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("known receipt did not clear original journal", err)
	}
	// The same record after lost cleanup checks deletion, never recreates a root.
	nativeMenuRPC[protocol.DeleteResult](t, m.connection, "sessions.delete", protocol.SessionParams{SessionID: m.owner.ID})
	if err := second.recovery.root.WriteFile(second.recovery.name(m.owner.ID), original, 0o600); err != nil {
		t.Fatal(err)
	}
	deleted, err := second.recovery.restore(m.connection, m.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	result = second.sendInput(deleted, "check")().(nativeSubmission)
	second.Update(result)
	if result.err != nil || result.admission.Input != nil || second.uncertain != nil || !strings.Contains(second.status, "deleted") {
		t.Fatal(result, second.status)
	}
}

func TestNativeRecoveryCannotOverwriteAnotherTerminalOrClearItsRequest(t *testing.T) {
	m, _ := nativeUIFixture(t)
	directory := t.TempDir()
	first := nativeJournal(t, directory, m.connection.Identity())
	second := nativeJournal(t, directory, m.connection.Identity())
	a, b := nativeRecoveryCommand(t, m, "first"), nativeRecoveryCommand(t, m, "second")
	if err := first.save(a); err != nil {
		t.Fatal(err)
	}
	original, err := first.read(m.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := second.save(b); err == nil {
		t.Fatal("overwrote unresolved request")
	}
	if err := second.clear(b); err == nil {
		t.Fatal("cleared another request")
	}
	if err := first.locked(func() error {
		if err := second.save(a); err == nil {
			t.Fatal("concurrent writer did not fail visibly")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	after, err := first.read(m.owner.ID)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatal("failed actions changed record", err)
	}
	m.recovery = second
	m.input.SetValue("a different unsent draft")
	if command := m.submit(); command != nil || m.input.Value() != "a different unsent draft" || m.uncertain == nil {
		t.Fatal("cross-terminal collision lost draft or dispatched", m.status)
	}
	var params protocol.SubmitParams
	if err := json.Unmarshal(m.uncertain.Record().Params, &params); err != nil || params.Identity.RequestID != "first" {
		t.Fatal(params, err)
	}
	if err := first.clear(a); err != nil {
		t.Fatal(err)
	}
	if err := second.save(b); err != nil {
		t.Fatal("known cleared record did not free slot", err)
	}
}

func TestNativeRecoveryRejectsUnsafeScopeReplacementAndCapacity(t *testing.T) {
	m, _ := nativeUIFixture(t)
	command := nativeRecoveryCommand(t, m, "unsafe")
	t.Run("publication failure keeps draft and never sends", func(t *testing.T) {
		r := nativeJournal(t, t.TempDir(), m.connection.Identity())
		if err := r.root.Close(); err != nil {
			t.Fatal(err)
		}
		m.recovery = r
		m.input.SetValue("not published")
		if m.submit() != nil || m.input.Value() != "not published" || !strings.Contains(m.status, "not sent") {
			t.Fatal(m.status)
		}
	})
	t.Run("linked directory", func(t *testing.T) {
		directory, target := t.TempDir(), t.TempDir()
		if err := os.Symlink(target, filepath.Join(directory, "tui-inputs")); err != nil {
			t.Fatal(err)
		}
		if _, err := openNativeRecovery(directory, m.connection.Identity()); err == nil {
			t.Fatal("followed linked namespace")
		}
	})
	t.Run("foreign owner and private file", func(t *testing.T) {
		r := nativeJournal(t, t.TempDir(), m.connection.Identity())
		if err := r.save(command); err != nil {
			t.Fatal(err)
		}
		raw, err := r.read(m.owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.root.WriteFile(r.name("foreign"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := r.restore(m.connection, "foreign"); err == nil {
			t.Fatal("restored foreign owner")
		}
		if err := r.root.Chmod(r.name(m.owner.ID), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := r.restore(m.connection, m.owner.ID); err == nil {
			t.Fatal("read public recovery file")
		}
	})
	t.Run("symbolic file", func(t *testing.T) {
		r := nativeJournal(t, t.TempDir(), m.connection.Identity())
		target := filepath.Join(t.TempDir(), "target")
		if err := os.WriteFile(target, []byte("unchanged"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := r.root.Symlink(target, r.name(m.owner.ID)); err != nil {
			t.Fatal(err)
		}
		if err := r.save(command); err == nil {
			t.Fatal("followed symbolic record")
		}
		if err := r.clear(command); err == nil {
			t.Fatal("cleared symbolic record")
		}
		raw, err := os.ReadFile(target)
		if err != nil || string(raw) != "unchanged" {
			t.Fatal(string(raw), err)
		}
	})
	for _, count := range []int{nativeRecoveryCount, 8} {
		t.Run(fmt.Sprintf("bounded-%d", count), func(t *testing.T) {
			r := nativeJournal(t, t.TempDir(), m.connection.Identity())
			for i := range count {
				file, err := r.root.OpenFile(fmt.Sprintf("retained-%d.json", i), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				if count == 8 {
					err = file.Truncate(client.MaxInputRecordBytes)
				}
				if err = errors.Join(err, file.Close()); err != nil {
					t.Fatal(err)
				}
			}
			if err := r.save(command); err == nil {
				t.Fatal("recovery capacity silently exceeded")
			}
		})
	}
}

func TestNativeRejectionPreservesOriginalAndNewlyTypedDrafts(t *testing.T) {
	m, _ := nativeUIFixture(t)
	command := nativeRecoveryCommand(t, m, "rejected")
	rejection := nativeSubmission{command: command, err: &client.Error{Kind: "CONFLICT", Message: "target turn ended"}}
	m.input.Reset()
	m.Update(rejection)
	if m.input.Value() != "original recovery text" || m.rejected != nil {
		t.Fatal("original rejected input was lost", m.input.Value(), m.status)
	}
	m.input.SetValue("next draft already being typed")
	m.Update(rejection)
	if m.input.Value() != "next draft already being typed" || m.rejected == nil {
		t.Fatal("new draft overwritten", m.input.Value())
	}
	if m.prompt(m.input.Value(), "queue") != nil {
		t.Fatal("another rejection could overwrite retained draft")
	}
	m.command("/rejected restore")
	if m.input.Value() != "original recovery text" || m.rejected != nil {
		t.Fatal("explicit restore lost original")
	}
}
