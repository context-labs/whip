package tui

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/context-labs/whip/internal/protocol"
)

func writeNativeCopyHelper(t *testing.T, directory, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
}

func nativeCopyWaitFile(t *testing.T, path, want string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		body, err := os.ReadFile(path)
		if err == nil && (want == "" && len(body) > 0 || want != "" && string(body) == want) {
			return string(body)
		}
		select {
		case <-ctx.Done():
			t.Fatalf("clipboard helper file unavailable: %s: %q, %v", path, body, err)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestNativeCopyForegroundOfferIsReplacedAndJoinedOnDetach(t *testing.T) {
	for _, helper := range []string{"wl-copy", "xclip"} {
		t.Run(helper, func(t *testing.T) {
			directory := t.TempDir()
			t.Setenv("PATH", directory)
			t.Setenv("INFERENCE_API_KEY", "never-in-clipboard")
			t.Setenv("DISPLAY", "fixture-display")
			arguments := "--foreground --type text/plain"
			if helper == "xclip" {
				arguments = "-selection clipboard -quiet"
			}
			writeNativeCopyHelper(t, directory, helper, "[ \"$*\" = '"+arguments+"' ] || exit 7\n[ -z \"$INFERENCE_API_KEY\" ] || exit 8\n[ \"$DISPLAY\" = fixture-display ] || exit 9\n/bin/cat > offered\n/bin/sleep 30 &\nprintf '%s' \"$!\" > child.pid.tmp\n/bin/mv child.pid.tmp child.pid\nwait\n")
			owner := newNativeClipboardOwner(t.Context())
			t.Cleanup(owner.close)
			ctx, cancel := context.WithCancel(t.Context())
			name, err := owner.offer(ctx, directory, "first\nexact\ttext ")
			cancel()
			if err != nil || name != helper {
				t.Fatal(name, err)
			}
			nativeCopyWaitFile(t, filepath.Join(directory, "offered"), "first\nexact\ttext ")
			pid, err := strconv.Atoi(nativeCopyWaitFile(t, filepath.Join(directory, "child.pid"), ""))
			if err != nil || syscall.Kill(pid, 0) != nil {
				t.Fatal("the foreground clipboard owner did not survive observation cancellation", pid, err)
			}
			if err := os.Remove(filepath.Join(directory, "child.pid")); err != nil {
				t.Fatal(err)
			}
			if _, err := owner.offer(t.Context(), directory, "replacement"); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatal("replaced clipboard descendant survived", pid, err)
			}
			nativeCopyWaitFile(t, filepath.Join(directory, "offered"), "replacement")
			pid, err = strconv.Atoi(nativeCopyWaitFile(t, filepath.Join(directory, "child.pid"), ""))
			if err != nil {
				t.Fatal(err)
			}
			owner.close()
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatal("clipboard descendant survived joined detach", pid, err)
			}
			if _, err := owner.offer(t.Context(), directory, "after close"); err == nil {
				t.Fatal("copy accepted after detach")
			}
		})
	}
}

func TestNativeCopyBoundsFailureAndConcurrentDetach(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory)
	writeNativeCopyHelper(t, directory, "pbcopy", "/bin/cat > offered\nexit 17\n")
	owner := newNativeClipboardOwner(t.Context())
	t.Cleanup(owner.close)
	if _, err := owner.offer(t.Context(), directory, "text"); err == nil {
		t.Fatal("confirmed helper failure looked successful")
	}
	if _, err := owner.offer(t.Context(), directory, strings.Repeat("x", nativeCopyLimit+1)); err == nil {
		t.Fatal("unbounded copy accepted")
	}
	writeNativeCopyHelper(t, directory, "pbcopy", "printf started > started\n/bin/sleep 30 &\nprintf '%s' \"$!\" > child.pid.tmp\n/bin/mv child.pid.tmp child.pid\nwait\n")
	done := make(chan error, 1)
	go func() {
		_, err := owner.offer(t.Context(), directory, strings.Repeat("x", nativeCopyLimit))
		done <- err
	}()
	nativeCopyWaitFile(t, filepath.Join(directory, "started"), "started")
	pid, err := strconv.Atoi(nativeCopyWaitFile(t, filepath.Join(directory, "child.pid"), ""))
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	owner.close()
	if time.Since(started) > 3*time.Second {
		t.Fatal("detach waited for helper lifetime timeout")
	}
	if err := <-done; err == nil {
		t.Fatal("interrupted offer looked successful")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("unread clipboard descendant survived", pid, err)
	}
}

func TestNativeCopyOutputBoundAndObservationDeadline(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory)
	writeNativeCopyHelper(t, directory, "pbcopy", "while :; do printf '0123456789012345678901234567890123456789'; done\n")
	owner := newNativeClipboardOwner(t.Context())
	t.Cleanup(owner.close)
	if _, err := owner.offer(t.Context(), directory, "text"); err == nil || !strings.Contains(err.Error(), "output limit") {
		t.Fatal("copy helper output was not bounded", err)
	}
	writeNativeCopyHelper(t, directory, "pbcopy", "/bin/sleep 30\n")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := owner.offer(ctx, directory, strings.Repeat("x", nativeCopyLimit)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked clipboard did not respect observation deadline", err)
	}
}

func TestNativeCopyCapturesCanonicalOwnerTextWithoutRetargetOrAdmission(t *testing.T) {
	m, _ := nativeUIFixture(t)
	t.Cleanup(m.close)
	t.Setenv("PATH", t.TempDir()) // OSC 52 only; no real clipboard helper.
	t.Setenv("TMUX", "fixture")
	m.history.messages = []protocol.Message{{ID: "answer", SessionID: m.owner.ID, Role: "assistant", Parts: []protocol.Part{{Type: "text", Text: "original\n文\t "}, {Type: "private", Text: "never-copy-private-continuation"}}}}
	m.input.SetValue("preserved draft")
	m.shortcut(tea.KeyPressMsg{Code: 'x', Mod: tea.ModCtrl})
	command, handled := m.shortcut(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if !handled || command == nil || m.input.Value() != "preserved draft" {
		t.Fatal("copy shortcut changed the draft", m.status)
	}
	batch := command().(tea.BatchMsg)
	m.history.messages[0].Parts[0].Text = "later replacement"
	m.generation++
	m.status = "new owner view"
	for _, command := range batch {
		switch message := command().(type) {
		case tea.RawMsg:
			want := "\x1bPtmux;\x1b\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("original\n文\t ")) + "\x07\x1b\\"
			if message.Msg != want {
				t.Fatal("copy did not preserve the selected original", message.Msg)
			}
		case nativeCopyResult:
			m.Update(message)
		default:
			t.Fatalf("unexpected clipboard message %T", message)
		}
	}
	if m.status != "new owner view" || m.copyBusy || m.sending {
		t.Fatal("stale clipboard observation changed owner state", m.status)
	}
	activity, err := m.handle.Activity(t.Context())
	if err != nil || activity.ActiveTurn != nil || activity.QueuedInputCount != 0 {
		t.Fatal("copy admitted host work", activity, err)
	}
	m.browse = &nativeBrowse{transcript: nativeTranscript{messages: []protocol.Message{{Role: "user", Parts: []protocol.Part{{Type: "text", Text: "user-only window"}}}}}}
	if m.copyCommand("") != nil || !strings.Contains(m.status, "loaded history window") {
		t.Fatal("copy silently crossed the displayed history window", m.status)
	}
}
