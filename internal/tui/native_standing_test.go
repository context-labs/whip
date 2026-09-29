package tui

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/context-labs/whip/internal/protocol"
)

func nativeStandingScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "editor")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func nativeStandingFixture(t *testing.T) (*nativeModel, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "host-only.md")
	if err := os.WriteFile(path, []byte("# source\r\nOriginal rule.\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, _ := nativeUIFixtureStanding(t, path)
	return m, path
}

func nativeEditorForTest(m *nativeModel, executable string) *nativeStandingEditor {
	editor := newNativeStandingEditor(&m.work, m.connection)
	editor.editor = executable
	editor.SetStdin(strings.NewReader(""))
	editor.SetStdout(io.Discard)
	editor.SetStderr(io.Discard)
	return editor
}

func TestNativeStandingEditorPublishesOnlyBoundedLocalCopy(t *testing.T) {
	m, hostPath := nativeStandingFixture(t)
	marker := filepath.Join(t.TempDir(), "editor-path")
	t.Setenv("NATIVE_EDITOR_PATH", marker)
	editor := nativeEditorForTest(m, nativeStandingScript(t, `printf '%s' "$1" > "$NATIVE_EDITOR_PATH"
printf '# raw\r\n café\n' > "$1"
`))
	if err := editor.Run(); err != nil {
		t.Fatal(err)
	}
	if editor.draft == nil || editor.draft.Text != "# raw\r\n café\n" {
		t.Fatal(editor.draft)
	}
	copyPath, err := os.ReadFile(marker)
	if err != nil || string(copyPath) == hostPath || strings.Contains(string(copyPath), "host-only") {
		t.Fatal("host path exposed to editor", string(copyPath), err)
	}
	if _, err := os.Stat(string(copyPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("local copy retained after editing", err)
	}
	if raw, err := os.ReadFile(hostPath); err != nil || string(raw) != editor.draft.Text {
		t.Fatal(string(raw), err)
	}
	m.Update(nativeStandingResult{draft: editor.draft})
	if m.standingDraft != nil {
		t.Fatal("saved draft remained pending")
	}
	var grants protocol.GrantsResult
	if err := m.connection.Call(t.Context(), "grants.list", protocol.GrantsParams{SessionID: m.handle.ID(), Limit: 64}, &grants); err != nil || len(grants.Items) != 0 {
		t.Fatal("human edit created agent authority", grants, err)
	}
}

func TestNativeStandingEditorDoesNotStartForUnpublishedHost(t *testing.T) {
	m, _ := nativeUIFixture(t)
	editor := nativeEditorForTest(m, "/not/a/real/editor")
	if err := editor.Run(); err == nil || !strings.Contains(err.Error(), "no standing-instruction file") || editor.draft != nil {
		t.Fatal(err, editor.draft)
	}
	if result := nativeUIControl(t, m, "/me read"); result.err != nil || !strings.Contains(m.status, "No standing-instruction") {
		t.Fatal(result.err, m.status)
	}
}

func TestNativeStandingDraftRetainsOriginalCASAfterLostAcknowledgement(t *testing.T) {
	m, path := nativeStandingFixture(t)
	var before protocol.HostStandingInstructions
	if err := m.connection.Call(t.Context(), "host.standing.read", protocol.EmptyParams{}, &before); err != nil {
		t.Fatal(err)
	}
	draft := protocol.WriteHostStandingInstructionsParams{ExpectedRevision: *before.Revision, Text: "Applied once.\n"}
	if err := writeNativeStanding(t.Context(), m.connection, draft); err != nil {
		t.Fatal(err)
	}
	m.Update(nativeStandingResult{draft: &draft, err: errors.New("lost acknowledgement")})
	m.input.SetValue("must not assume instructions saved")
	if command := m.submit(); command != nil || m.input.Value() == "" {
		t.Fatal("unresolved publication did not preserve prompt")
	}
	if command := m.command("/me draft"); command != nil || !strings.Contains(m.notice, draft.Text) {
		t.Fatal(m.notice)
	}
	if result := nativeUIControl(t, m, "/me read"); result.err != nil || m.standingDraft == nil || m.standingDraft.ExpectedRevision != *before.Revision {
		t.Fatal("read silently rebased or discarded draft", result.err)
	}
	command := m.command("/me retry")
	if command == nil {
		t.Fatal(m.status)
	}
	result := command().(nativeStandingResult)
	m.Update(result)
	if result.err == nil || m.standingDraft == nil || m.standingDraft.ExpectedRevision != *before.Revision {
		t.Fatal("retry refreshed stale CAS", result)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != draft.Text {
		t.Fatal(string(raw), err)
	}
	m.command("/me discard")
	if m.standingDraft != nil {
		t.Fatal("explicit local discard failed")
	}
}

func TestNativeStandingEditorCancellationJoinsOwnedProcess(t *testing.T) {
	m, _ := nativeStandingFixture(t)
	marker := filepath.Join(t.TempDir(), "started")
	t.Setenv("NATIVE_EDITOR_STARTED", marker)
	editor := nativeEditorForTest(m, nativeStandingScript(t, `printf ready > "$NATIVE_EDITOR_STARTED"
exec /bin/sleep 30
`))
	done := make(chan error, 1)
	go func() { done <- editor.Run() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("editor did not start")
		}
		time.Sleep(time.Millisecond)
	}
	closed := make(chan struct{})
	go func() { m.work.close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("terminal did not join editor")
	}
	if err := <-done; err == nil {
		t.Fatal("cancelled edit was published")
	}
}

func TestNativeStandingDraftRejectsUnsafeOrOversizedEditedFiles(t *testing.T) {
	for _, test := range []struct {
		name string
		make func(string) error
	}{
		{"oversized", func(path string) error {
			return os.WriteFile(path, []byte(strings.Repeat("x", nativeStandingLimit+1)), 0o600)
		}},
		{"invalid_utf8", func(path string) error { return os.WriteFile(path, []byte{0xff}, 0o600) }},
		{"nul", func(path string) error { return os.WriteFile(path, []byte("a\x00b"), 0o600) }},
		{"symlink", func(path string) error { return os.Symlink("/not-a-client-file", path) }},
		{"fifo", func(path string) error { return syscall.Mkfifo(path, 0o600) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := test.make(filepath.Join(dir, "standing.md")); err != nil {
				t.Fatal(err)
			}
			if _, err := readNativeStandingDraft(dir); err == nil {
				t.Fatal("unsafe editor output accepted")
			}
		})
	}
}

func TestNativeStandingEditorKeepsDraftOnConcurrentHostEdit(t *testing.T) {
	m, path := nativeStandingFixture(t)
	t.Setenv("NATIVE_HOST_TEST_PATH", path)
	editor := nativeEditorForTest(m, nativeStandingScript(t, `printf 'Another writer.\n' > "$NATIVE_HOST_TEST_PATH"
printf 'My local draft.\n' > "$1"
`))
	err := editor.Run()
	if err == nil || editor.draft == nil || editor.draft.Text != "My local draft.\n" {
		t.Fatal(err, editor.draft)
	}
	m.Update(nativeStandingResult{draft: editor.draft, err: err})
	if m.standingDraft == nil || m.standingDraft.Text != "My local draft.\n" {
		t.Fatal("stale publication lost edited text", m.status)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "Another writer.\n" {
		t.Fatal("concurrent edit overwritten", string(raw), err)
	}
}
