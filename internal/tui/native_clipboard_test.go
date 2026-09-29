package tui

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestNativeClipboardUsesBoundedLocalHelperWithoutAmbientSecrets(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory)
	t.Setenv("INFERENCE_API_KEY", "fixture-secret-never-in-child")
	t.Setenv("DISPLAY", "fixture-display")
	body := nativeImageFixture(t)
	if err := os.WriteFile(filepath.Join(directory, "body.png"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\n[ \"$1\" = - ] || exit 5\n[ -z \"$INFERENCE_API_KEY\" ] || exit 6\n[ \"$DISPLAY\" = fixture-display ] || exit 7\nexec /bin/cat body.png\n"
	if err := os.WriteFile(filepath.Join(directory, "pngpaste"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := nativeClipboardImage(t.Context(), directory)
	if err != nil || !bytes.Equal(data, body) {
		t.Fatal("clipboard helper did not preserve scoped bytes", err)
	}
	// A program returning an unbounded stream is stopped at the buffer limit.
	start := time.Now()
	_, err = nativeClipboardCommand(t.Context(), directory, "/bin/sh", []string{"-c", "while :; do printf '0123456789012345678901234567890123456789'; done"}, 1024)
	if err == nil || !strings.Contains(err.Error(), "output limit") || time.Since(start) > 3*time.Second {
		t.Fatal("output bound did not stop helper", err, time.Since(start))
	}
}

func TestNativeClipboardDetachJoinsHelperDescendants(t *testing.T) {
	m, _ := nativeUIFixture(t)
	directory := t.TempDir()
	m.clientDirectory = directory
	t.Setenv("PATH", directory)
	script := "#!/bin/sh\n/bin/sleep 30 &\nchild=$!\nprintf '%s' \"$child\" > child.pid\nwait\n"
	if err := os.WriteFile(filepath.Join(directory, "pngpaste"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	_, command := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if command == nil {
		t.Fatal(m.status)
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- command() }()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var pid int
	for pid == 0 {
		body, err := os.ReadFile(filepath.Join(directory, "child.pid"))
		if err == nil {
			pid, _ = strconv.Atoi(string(body))
		}
		select {
		case <-ctx.Done():
			t.Fatal("clipboard fixture never spawned", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	start := time.Now()
	m.work.close()
	if time.Since(start) > 3*time.Second {
		t.Fatal("detach did not join helper promptly")
	}
	select {
	case message := <-result:
		if message.(nativeImageLoaded).err == nil {
			t.Fatal("cancelled clipboard looked successful")
		}
	case <-ctx.Done():
		t.Fatal("clipboard read outlived detach")
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatal("clipboard descendant survived joined detach", pid, err)
	}
}
