package hostview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fakePicker(t *testing.T, script string) *Picker {
	t.Helper()
	directory := t.TempDir()
	name := "osascript"
	if runtime.GOOS == "linux" {
		name = "zenity"
	}
	if err := os.WriteFile(filepath.Join(directory, name), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	picker := NewPicker(directory)
	t.Cleanup(func() {
		if err := picker.Close(); err != nil {
			t.Error(err)
		}
	})
	return picker
}

func TestHostPickerCommandAndCancellation(t *testing.T) {
	_, args, cancelled := pickCommand("darwin", `/tmp/a "quote"`)
	if !strings.Contains(args[1], `default location (POSIX file "/tmp/a \"quote\"")`) || !cancelled(1, "User canceled") {
		t.Fatal(args)
	}
	_, empty, _ := pickCommand("darwin", "")
	if strings.Contains(empty[1], "default location") {
		t.Fatal(empty)
	}
	for _, test := range []struct {
		script, path       string
		cancelled, invalid bool
	}{
		{"printf '/tmp/space /\\n'", "/tmp/space ", false, false},
		{"printf '/\\n'", "/", false, false},
		{"printf 'User canceled' >&2; exit 1", "", true, false},
		{"exit 2", "", false, true},
		{"printf 'relative\\n'", "", false, true},
	} {
		t.Run(test.script, func(t *testing.T) {
			picker := fakePicker(t, test.script)
			result, err := picker.Pick(t.Context(), "")
			if test.invalid {
				if err == nil {
					t.Fatal(result)
				}
				return
			}
			if err != nil || result.Cancelled != test.cancelled || test.path != "" && (result.Path == nil || *result.Path != test.path) {
				t.Fatal(result, err)
			}
		})
	}
}

func TestHostPickerOutputBoundAndJoinedClose(t *testing.T) {
	t.Run("output", func(t *testing.T) {
		picker := fakePicker(t, "while :; do printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'; done")
		if _, err := picker.Pick(t.Context(), ""); !errors.Is(err, ErrPickerLimit) {
			t.Fatal(err)
		}
	})
	t.Run("close", func(t *testing.T) {
		marker := filepath.Join(t.TempDir(), "started")
		picker := fakePicker(t, "printf started > '"+marker+"'; /bin/sleep 30 & wait")
		done := make(chan error, 1)
		go func() { _, err := picker.Pick(t.Context(), ""); done <- err }()
		deadline := time.Now().Add(3 * time.Second)
		for {
			if _, err := os.Stat(marker); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("picker did not start")
			}
			time.Sleep(time.Millisecond)
		}
		start := time.Now()
		if err := picker.Close(); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > 3*time.Second {
			t.Fatal("close did not join promptly")
		}
		if err := <-done; err == nil {
			t.Fatal("killed dialog reported success")
		}
		if _, err := picker.Pick(t.Context(), ""); !errors.Is(err, ErrPickerClosed) {
			t.Fatal(err)
		}
	})
	t.Run("observer", func(t *testing.T) {
		picker := fakePicker(t, "/bin/sleep 30")
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
		defer cancel()
		if _, err := picker.Pick(ctx, ""); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	})
}

func TestHostPickerConcurrentDialogBound(t *testing.T) {
	picker := fakePicker(t, "/bin/sleep 30")
	done := make(chan error, 2)
	for range 2 {
		go func() { _, err := picker.Pick(t.Context(), ""); done <- err }()
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		picker.mu.Lock()
		active := picker.active
		picker.mu.Unlock()
		if active == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("dialogs did not enter")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := picker.Pick(t.Context(), ""); !errors.Is(err, ErrPickerLimit) {
		t.Fatal(err)
	}
	if err := picker.Close(); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-done; err == nil {
			t.Fatal("closed dialog succeeded")
		}
	}
}
