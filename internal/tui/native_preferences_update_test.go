package tui

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestNativePreferencesLockedEditPreservesOtherClientAndRefusesContention(t *testing.T) {
	directory := t.TempDir()
	if err := saveNativePreferences(directory, nativePreferences{Theme: "dark"}); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(directory, ".client-preferences.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	changed := false
	if _, err := updateNativePreferences(directory, func(*nativePreferences) { changed = true }); err == nil || !strings.Contains(err.Error(), "another terminal") || changed {
		t.Fatal("contended edit was applied", err, changed)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if _, err := updateNativePreferences(directory, func(value *nativePreferences) { value.Mouse = new(false) }); err != nil {
		t.Fatal(err)
	}
	result, err := updateNativePreferences(directory, func(value *nativePreferences) { value.Theme = "light" })
	if err != nil || result.Theme != "light" || result.Mouse == nil || *result.Mouse {
		t.Fatal("independent preference overwritten", result, err)
	}
}

func TestNativePreferencesLockCannotFollowSymlink(t *testing.T) {
	directory := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(directory, ".client-preferences.lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := updateNativePreferences(directory, func(value *nativePreferences) { value.Theme = "light" }); err == nil {
		t.Fatal("followed linked lock")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "unchanged" {
		t.Fatal("changed unrelated lock target", err)
	}
}
